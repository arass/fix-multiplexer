// Package transport bounds downstream connections and unread bytes without
// interpreting FIX framing, fields, sequence numbers or resend semantics.
package transport

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var ErrLimit = errors.New("gateway: transport resource limit")

type Pool struct {
	mu             sync.Mutex
	connections    map[*Conn]bool
	bindings       map[string]*Conn
	maxConnections int
	maxBytes       int64
	onError        func()
	closed         bool
	Rejected       atomic.Uint64
}
type Conn struct {
	net.Conn
	pool    *Pool
	pending atomic.Int64
	bound   atomic.Bool
	id      string
	once    sync.Once
}

func New(maxConnections, maxBytes int, onError func()) *Pool {
	return &Pool{connections: make(map[*Conn]bool), bindings: make(map[string]*Conn), maxConnections: maxConnections, maxBytes: int64(maxBytes), onError: onError}
}
func (p *Pool) Add(c net.Conn) (*Conn, error) {
	p.mu.Lock()
	if p.closed || len(p.connections) >= p.maxConnections {
		p.mu.Unlock()
		c.Close()
		p.reject()
		return nil, ErrLimit
	}
	conn := &Conn{Conn: c, pool: p}
	p.connections[conn] = true
	p.mu.Unlock()
	return conn, nil
}
func (p *Pool) Bind(id string, c net.Conn) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	conn, ok := c.(*Conn)
	if !ok || conn.pool != p {
		return ErrLimit
	}
	if current := p.bindings[id]; current != nil && current != conn {
		return errors.New("gateway: duplicate connection rejected")
	}
	conn.id = id
	conn.bound.Store(true)
	p.bindings[id] = conn
	return nil
}
func (p *Pool) Consume(id string, n int) {
	p.mu.Lock()
	conn := p.bindings[id]
	p.mu.Unlock()
	if conn == nil {
		return
	}
	for {
		old := conn.pending.Load()
		next := old - int64(n)
		if next < 0 {
			next = 0
		}
		if conn.pending.CompareAndSwap(old, next) {
			return
		}
	}
}
func (p *Pool) CloseAll() {
	p.mu.Lock()
	p.closed = true
	conns := make([]*Conn, 0, len(p.connections))
	for c := range p.connections {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}
func (p *Pool) reject() {
	p.Rejected.Add(1)
	if p.onError != nil {
		p.onError()
	}
}
func (c *Conn) Read(b []byte) (int, error) {
	remaining := c.pool.maxBytes - c.pending.Load()
	if remaining <= 0 {
		c.pool.reject()
		c.Close()
		return 0, ErrLimit
	}
	if int64(len(b)) > remaining {
		b = b[:int(remaining)]
	}
	timeout := 5 * time.Second
	if c.bound.Load() {
		timeout = 65 * time.Second
	}
	if err := c.Conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(b)
	c.pending.Add(int64(n))
	return n, err
}
func (c *Conn) Write(b []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}
func (c *Conn) Close() error {
	var result error
	c.once.Do(func() {
		result = c.Conn.Close()
		c.pool.mu.Lock()
		delete(c.pool.connections, c)
		if c.pool.bindings[c.id] == c {
			delete(c.pool.bindings, c.id)
		}
		c.pool.mu.Unlock()
	})
	return result
}

type listener struct {
	net.Listener
	pool *Pool
}

func (p *Pool) Wrap(l net.Listener) net.Listener { return &listener{l, p} }
func (l *listener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		wrapped, err := l.pool.Add(c)
		if err == nil {
			return wrapped, nil
		}
	}
}
