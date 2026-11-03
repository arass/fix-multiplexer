// Package gateway routes application messages without depending on a FIX engine.
package gateway

import (
	"errors"
	"sync"
	"sync/atomic"

	"fix-multiplexer/internal/sessions"
)

var (
	ErrStopped     = errors.New("gateway: routing stopped")
	ErrUnavailable = errors.New("gateway: upstream unavailable")
	ErrPolicy      = errors.New("gateway: unsupported application message type")
	ErrReadOnly    = errors.New("gateway: client is read-only")
	ErrFull        = errors.New("gateway: routing queue or byte budget exhausted")
	ErrSend        = errors.New("gateway: forwarding failed; delivery may be uncertain")
)

// Message is immutable until copied for an individual outbound send.
type Message interface {
	Type() string
	Size() int
	Clone() (Message, error)
}
type Sender interface{ Send(Message, string) error }
type SendFunc func(Message, string) error

func (f SendFunc) Send(m Message, id string) error { return f(m, id) }

type Options struct {
	Upstream                                  string
	Clients                                   []string
	UpstreamTypes, DownstreamTypes, DenyTypes []string
	UpstreamQueue, DownstreamQueue            int
	ByteBudget                                int64
	ClientPolicies                            map[string]ClientPolicy
}

// A nil receive list inherits the global policy; an empty list receives none.
type ClientPolicy struct {
	ReadOnly     bool
	ReceiveTypes []string
}

type Counters struct{ Forwarded, Failed, Dropped, Recipients, Blocked atomic.Uint64 }
type job struct {
	source, target string
	generation     uint64
	msg            Message
	reply          chan error
	bytes          int64
}
type Router struct {
	options  Options
	registry *sessions.Registry
	sender   Sender
	upstream chan job
	clients  map[string]chan job
	done     chan struct{}
	mu       sync.RWMutex
	stopped  bool
	wg       sync.WaitGroup
	used     atomic.Int64
	Counters Counters
}

func New(o Options, r *sessions.Registry, s Sender) *Router {
	g := &Router{options: o, registry: r, sender: s, upstream: make(chan job, o.UpstreamQueue), clients: make(map[string]chan job), done: make(chan struct{})}
	g.wg.Add(1)
	go g.worker(g.upstream)
	for _, id := range o.Clients {
		q := make(chan job, o.DownstreamQueue)
		g.clients[id] = q
		g.wg.Add(1)
		go g.worker(q)
	}
	return g
}

func allowed(typ string, allow, deny []string) bool {
	switch typ {
	case "0", "1", "2", "3", "4", "5", "A":
		return false
	}
	for _, v := range deny {
		if typ == v {
			return false
		}
	}
	for _, v := range allow {
		if typ == v {
			return true
		}
	}
	return false
}

// Submit serializes upstream sends. A nil result is local engine acceptance only.
func (g *Router) Submit(source string, m Message) error {
	fail := func(err error) error { g.Counters.Failed.Add(1); return err }
	s, ok := g.registry.Get(source)
	if !ok || !s.Enabled || !s.LoggedOn {
		return fail(sessions.ErrUnavailable)
	}
	if _, ok := g.clients[source]; !ok {
		return fail(sessions.ErrUnavailable)
	}
	if g.options.ClientPolicies[source].ReadOnly {
		g.Counters.Blocked.Add(1)
		return fail(ErrReadOnly)
	}
	if !allowed(m.Type(), g.options.UpstreamTypes, g.options.DenyTypes) {
		g.Counters.Blocked.Add(1)
		return fail(ErrPolicy)
	}
	u, ok := g.registry.Get(g.options.Upstream)
	if !ok || !u.Enabled || !u.LoggedOn {
		return fail(ErrUnavailable)
	}
	j := job{source: source, target: u.ID, generation: u.Generation, msg: m, reply: make(chan error, 1)}
	if err := g.enqueue(g.upstream, j); err != nil {
		g.Counters.Failed.Add(1)
		return err
	}
	return <-j.reply
}

// Broadcast enqueues only for recipients live at receipt; generation checks prevent
// stale work being delivered on a later connection. Slow recipients have separate workers.
func (g *Router) Broadcast(m Message) error {
	if !allowed(m.Type(), g.options.DownstreamTypes, g.options.DenyTypes) {
		g.Counters.Dropped.Add(1)
		return ErrPolicy
	}
	return g.broadcast(m, true)
}

// BroadcastNotice sends a gateway-generated operational notice regardless of
// application fanout policy. Callers must never pass an upstream admin message.
func (g *Router) BroadcastNotice(m Message) error {
	return g.broadcast(m, false)
}

func (g *Router) broadcast(m Message, filtered bool) error {
	for _, s := range g.registry.Snapshot() {
		if s.Enabled && s.LoggedOn {
			if q, ok := g.clients[s.ID]; ok {
				policy := g.options.ClientPolicies[s.ID]
				if filtered && policy.ReceiveTypes != nil && !allowed(m.Type(), policy.ReceiveTypes, nil) {
					continue
				}
				if err := g.enqueue(q, job{target: s.ID, generation: s.Generation, msg: m}); err != nil {
					g.Counters.Failed.Add(1)
					g.Counters.Dropped.Add(1)
				} else {
					g.Counters.Recipients.Add(1)
				}
			}
		}
	}
	return nil
}

func (g *Router) enqueue(q chan job, j job) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.stopped {
		return ErrStopped
	}
	j.bytes = int64(j.msg.Size())
	if j.bytes < 0 {
		return ErrFull
	}
	for {
		old := g.used.Load()
		if j.bytes > g.options.ByteBudget-old {
			return ErrFull
		}
		if g.used.CompareAndSwap(old, old+j.bytes) {
			break
		}
	}
	select {
	case q <- j:
		return nil
	default:
		g.used.Add(-j.bytes)
		return ErrFull
	}
}

func (g *Router) worker(q chan job) {
	defer g.wg.Done()
	finish := func(j job, err error) {
		g.used.Add(-j.bytes)
		if j.reply != nil {
			j.reply <- err
		}
	}
	for {
		select {
		case <-g.done:
			for {
				select {
				case j := <-q:
					finish(j, ErrStopped)
				default:
					return
				}
			}
		case j := <-q:
			if !g.registry.Live(j.target, j.generation) {
				g.Counters.Failed.Add(1)
				finish(j, ErrUnavailable)
				continue
			}
			m, err := j.msg.Clone()
			if err == nil {
				err = g.sender.Send(m, j.target)
			}
			if err != nil {
				g.Counters.Failed.Add(1)
				finish(j, ErrSend)
			} else {
				g.Counters.Forwarded.Add(1)
				finish(j, nil)
			}
		}
	}
}

func (g *Router) Ready() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	u, ok := g.registry.Get(g.options.Upstream)
	return !g.stopped && ok && u.Enabled && u.LoggedOn
}
func (g *Router) QueueBytes() int64 { return g.used.Load() }
func (g *Router) QueueDepth() int64 {
	n := len(g.upstream)
	for _, q := range g.clients {
		n += len(q)
	}
	return int64(n)
}
func (g *Router) Close() {
	g.mu.Lock()
	if !g.stopped {
		g.stopped = true
		close(g.done)
	}
	g.mu.Unlock()
	g.wg.Wait()
}
