package fixadapter

import (
	"errors"
	"sync"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/sessions"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/datadictionary"
	"github.com/quickfixgo/quickfix/store/file"
)

type Runtime struct {
	Registry         *sessions.Registry
	Router           *gateway.Router
	Application      *Application
	acceptor         *quickfix.Acceptor
	initiator        *quickfix.Initiator
	mu               sync.Mutex
	started, stopped bool
	stores           []quickfix.MessageStore
}

func New(c config.Config) (*Runtime, error) {
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
	var clients []string
	for id := range u.SessionSettings() {
		a.Upstream = id
		ids[id.String()] = id
		enabled[id.String()] = true
	}
	for id := range d.SessionSettings() {
		for _, cl := range c.Downstream.Clients {
			if id.TargetCompID == cl.Sender {
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
	r := &Runtime{}
	r.Registry = sessions.New(enabled, time.Now)
	a.Registry = r.Registry
	r.Router = gateway.New(gateway.Options{Upstream: a.Upstream.String(), Clients: clients, UpstreamTypes: c.Upstream.AppTypes, DownstreamTypes: c.Fanout.AppTypes, DenyTypes: c.Fanout.DenyTypes, UpstreamQueue: c.Service.UpstreamQueue, DownstreamQueue: c.Service.DownstreamQueue, ByteBudget: 64 << 20}, r.Registry, EngineSender{IDs: ids})
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
	r.acceptor, err = quickfix.NewAcceptor(a, trackingFactory{file.NewStoreFactory(d), &r.stores}, d, quickfix.NewNullLogFactory())
	if err != nil {
		return nil, errors.New("cannot initialize downstream FIX sessions")
	}
	r.initiator, err = quickfix.NewInitiator(a, trackingFactory{file.NewStoreFactory(u), &r.stores}, u, quickfix.NewNullLogFactory())
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

// SetListenerFactory injects a reserved or bounded listener before Start.
func (r *Runtime) SetListenerFactory(factory quickfix.NewListenerCallback) {
	r.acceptor.SetNewListenerCallback(factory)
}
func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.cleanup(r.started)
	r.stopped = true
}

func (r *Runtime) cleanup(started bool) {
	r.Router.Close()
	if started {
		r.initiator.Stop()
		r.acceptor.Stop()
	}
	for _, id := range r.Application.created {
		_ = quickfix.UnregisterSession(id)
	}
	for _, s := range r.stores {
		_ = s.Close()
	}
}

type trackingFactory struct {
	base   quickfix.MessageStoreFactory
	stores *[]quickfix.MessageStore
}

func (f trackingFactory) Create(id quickfix.SessionID) (quickfix.MessageStore, error) {
	s, err := f.base.Create(id)
	if err == nil {
		*f.stores = append(*f.stores, s)
	}
	return s, err
}
