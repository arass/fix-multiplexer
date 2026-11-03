package fixadapter

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/retention"
	"fix-multiplexer/internal/sessions"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/datadictionary"
	"github.com/quickfixgo/quickfix/store/file"
)

type Runtime struct {
	Registry          *sessions.Registry
	Router            *gateway.Router
	Application       *Application
	acceptor          *quickfix.Acceptor
	initiator         *quickfix.Initiator
	mu                sync.Mutex
	started, stopped  bool
	stopErr           error
	stores            []quickfix.MessageStore
	Stores            *retention.Manager
	Observer          *observability.Recorder
	storeWarn         int64
	Transport         *transport.Pool
	UpstreamTransport *transport.Pool
	Pending           *transport.Pending
	bridge            *transport.Bridge
	listenerFactory   quickfix.NewListenerCallback
}

func New(c config.Config, observer ...*observability.Recorder) (*Runtime, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	u, d, err := Settings(c)
	if err != nil {
		return nil, errors.New("invalid engine settings")
	}
	load := func(path string) (*datadictionary.DataDictionary, error) {
		if path == "" {
			path = "spec/FIX44.xml"
		}
		dict, err := datadictionary.Parse(path)
		if err != nil || dict.Major != 4 || dict.Minor != 4 {
			return nil, errors.New("cannot load FIX 4.4 dictionary")
		}
		return dict, nil
	}
	ud, err := load(c.Upstream.Dictionary)
	if err != nil {
		return nil, err
	}
	dd, err := load(c.Downstream.Dictionary)
	if err != nil {
		return nil, err
	}
	// Identical dictionaries are required until differing extension schemas have
	// an explicit compatibility test. Version translation is never attempted.
	if c.Upstream.Dictionary != c.Downstream.Dictionary {
		return nil, errors.New("upstream and downstream must use the same dictionary")
	}
	a := &Application{UpstreamDictionary: ud, DownstreamDictionary: dd, MaxMessageBytes: c.Service.MaxMessageBytes, ClientPasswords: make(map[string]string)}
	if a.Username, err = ResolveSecret(c.Upstream.Username); err != nil {
		return nil, err
	}
	if a.Password, err = ResolveSecret(c.Upstream.Password); err != nil {
		return nil, err
	}
	ids := make(map[string]quickfix.SessionID)
	enabled := make(map[string]bool)
	policies := make(map[string]gateway.ClientPolicy)
	var clients []string
	for id := range u.SessionSettings() {
		a.Upstream = id
		ids[id.String()] = id
		enabled[id.String()] = true
	}
	for id := range d.SessionSettings() {
		for _, cl := range c.Downstream.Clients {
			if id.TargetCompID == cl.Sender {
				policies[id.String()] = gateway.ClientPolicy{ReadOnly: cl.ReadOnly, ReceiveTypes: cl.ReceiveMessageTypes}
				enabled[id.String()] = !cl.Disabled
				password, e := ResolveSecret(cl.Password)
				if e != nil {
					return nil, e
				}
				a.ClientPasswords[id.String()] = password
			}
		}
		ids[id.String()] = id
		clients = append(clients, id.String())
	}
	r := &Runtime{Observer: observability.New(io.Discard), storeWarn: c.Retention.DiskWarnBytes}
	if len(observer) > 0 && observer[0] != nil {
		r.Observer = observer[0]
	}
	a.Observer = r.Observer
	r.Pending = transport.NewPending(64<<20, 2<<20)
	a.Pending = r.Pending
	r.Transport = transport.New(max(1, len(clients))*2, c.Service.MaxMessageBytes, func() { r.Observer.EngineError("downstream_transport") })
	r.Stores, err = retention.Open([]string{c.Upstream.StoreDir, c.Downstream.StoreDir}, c.Retention.DiskLimitBytes, c.Retention.SessionLimitBytes, time.Now)
	if err != nil {
		return nil, err
	}
	paths := make(map[quickfix.SessionID]string)
	for id := range ids {
		sid := ids[id]
		root := c.Downstream.StoreDir
		if sid == a.Upstream {
			root = c.Upstream.StoreDir
		}
		path, e := r.Stores.Register(id, root)
		if e != nil {
			r.Stores.Close()
			return nil, e
		}
		paths[sid] = path
	}
	u, d, err = settingsWithPaths(c, paths)
	if err != nil {
		r.Stores.Close()
		return nil, errors.New("cannot bind isolated store settings")
	}
	a.Stores = r.Stores
	r.Registry = sessions.New(enabled, time.Now)
	a.Registry = r.Registry
	r.Router = gateway.New(gateway.Options{Upstream: a.Upstream.String(), Clients: clients, ClientPolicies: policies, UpstreamTypes: c.Upstream.AppTypes, DownstreamTypes: c.Fanout.AppTypes, DenyTypes: c.Fanout.DenyTypes, UpstreamQueue: c.Service.UpstreamQueue, DownstreamQueue: c.Service.DownstreamQueue, ByteBudget: 64 << 20}, r.Registry, EngineSender{IDs: ids})
	a.Router = r.Router
	r.Application = a
	// Raw engine logs can contain payloads, parser errors and credentials. Disable
	// them completely; metadata is emitted by our observability layer instead.
	constructed := false
	defer func() {
		if !constructed {
			r.cleanup(false)
		}
	}()
	if err = r.prepareUpstreamTransport(c, u); err != nil {
		return nil, err
	}
	r.acceptor, err = quickfix.NewAcceptor(a, trackingFactory{base: file.NewStoreFactory(d), stores: &r.stores, manager: r.Stores, window: c.Retention.DownstreamWindow}, d, logFactory{recorder: r.Observer, pool: r.Transport, pending: r.Pending})
	if err != nil {
		return nil, errors.New("cannot initialize downstream FIX sessions")
	}
	r.initiator, err = quickfix.NewInitiator(a, trackingFactory{base: file.NewStoreFactory(u), stores: &r.stores, manager: r.Stores, window: c.Retention.UpstreamWindow}, u, logFactory{recorder: r.Observer, pool: r.UpstreamTransport, pending: r.Pending})
	if err != nil {
		return nil, errors.New("cannot initialize upstream FIX session")
	}
	constructed = true
	return r, nil
}
func (r *Runtime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return errors.New("runtime already started or stopped")
	}
	r.bridge.Start()
	factory := r.listenerFactory
	if factory == nil {
		factory = func(address string, c *tls.Config) (net.Listener, error) {
			if c != nil {
				return tls.Listen("tcp", address, c)
			}
			return net.Listen("tcp", address)
		}
	}
	r.acceptor.SetNewListenerCallback(func(address string, c *tls.Config) (net.Listener, error) {
		l, err := factory(address, c)
		if err != nil {
			return nil, err
		}
		return r.Transport.Wrap(l), nil
	})
	r.acceptor.SetConnectionValidator(connectionValidator{r.Transport})
	if err := r.acceptor.Start(); err != nil {
		r.cleanup(false)
		r.stopped = true
		return errors.New("cannot start downstream FIX listener")
	}
	if err := r.initiator.Start(); err != nil {
		r.acceptor.Stop()
		r.cleanup(false)
		r.stopped = true
		return errors.New("cannot start upstream FIX initiator")
	}
	r.started = true
	return nil
}

func (r *Runtime) Snapshot() observability.Snapshot {
	g := r.Router
	s := observability.Snapshot{Ready: g.Ready() && r.Stores.Healthy(), Forwarded: g.Counters.Forwarded.Load(), Failed: g.Counters.Failed.Load(), Dropped: g.Counters.Dropped.Load(), Recipients: g.Counters.Recipients.Load(), Replays: r.Observer.Replay.Load(), EngineErrors: r.Observer.EngineErrors.Load(), AuthFailures: r.Observer.AuthFailures.Load(), Cleanups: r.Stores.CleanupCount.Load(), QueueBytes: g.QueueBytes(), QueueDepth: g.QueueDepth(), StoreBytes: r.Stores.CachedUsage()}
	s.Blocked = g.Counters.Blocked.Load()
	s.StoreWarning = s.StoreBytes >= r.storeWarn
	s.EngineQueueBytes = r.Pending.Usage()
	for _, state := range r.Registry.Snapshot() {
		s.Sessions = append(s.Sessions, observability.Session{ID: state.ID, LoggedOn: state.LoggedOn, Enabled: state.Enabled})
	}
	return s
}

// SetListenerFactory injects a reserved or bounded listener before Start.
func (r *Runtime) SetListenerFactory(factory quickfix.NewListenerCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listenerFactory = factory
}
func (r *Runtime) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return r.stopErr
	}
	r.stopErr = r.cleanup(r.started)
	r.stopped = true
	return r.stopErr
}

func (r *Runtime) cleanup(started bool) error {
	var failed bool
	r.Router.Close()
	if started {
		r.initiator.Stop()
		r.acceptor.Stop()
	}
	r.Transport.CloseAll()
	if r.bridge != nil {
		r.bridge.Close()
	}
	for _, id := range r.Application.created {
		_ = quickfix.UnregisterSession(id)
	}
	for _, s := range r.stores {
		if s.Close() != nil {
			failed = true
		}
	}
	if r.Stores.Close() != nil {
		failed = true
	}
	if failed {
		r.Observer.EngineError("durable_shutdown")
		return errors.New("gateway: durable shutdown failed; inspect state before restart")
	}
	return nil
}

type connectionValidator struct{ pool *transport.Pool }

func (v connectionValidator) Validate(c net.Conn, id quickfix.SessionID) error {
	return v.pool.Bind(id.String(), c)
}

type trackingFactory struct {
	base    quickfix.MessageStoreFactory
	stores  *[]quickfix.MessageStore
	manager *retention.Manager
	window  time.Duration
}

func (f trackingFactory) Create(id quickfix.SessionID) (quickfix.MessageStore, error) {
	var s quickfix.MessageStore
	err := f.manager.Write(id.String(), 256, func() error {
		var err error
		s, err = f.base.Create(id)
		return err
	})
	if err == nil {
		s = &guardedStore{MessageStore: s, manager: f.manager, id: id.String(), window: f.window, now: time.Now}
		*f.stores = append(*f.stores, s)
	}
	return s, err
}
