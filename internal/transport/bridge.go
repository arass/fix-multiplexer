package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"
	"time"
)

// Bridge adds transport bounds to an engine initiator with no custom dial hook.
// It copies opaque bytes for the SAME upstream FIX session; it never interprets
// or forwards messages between independent FIX sessions.
type Bridge struct {
	listener net.Listener
	pool     *Pool
	id       string
	dial     func(context.Context) (net.Conn, error)
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   net.Conn
	wg       sync.WaitGroup
	once     sync.Once
}

func NewBridge(l net.Listener, p *Pool, id string, dial func(context.Context) (net.Conn, error)) *Bridge {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bridge{listener: l, pool: p, id: id, dial: dial, ctx: ctx, cancel: cancel}
}
func (b *Bridge) Address() string { return b.listener.Addr().String() }
func (b *Bridge) Start() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			local, err := b.listener.Accept()
			if err != nil {
				return
			}
			b.mu.Lock()
			b.active = local
			b.mu.Unlock()
			// A TLS listener performs its handshake lazily. Authenticate before
			// dialing the real upstream, with a bounded unauthenticated wait.
			if secure, ok := local.(*tls.Conn); ok {
				ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
				err := secure.HandshakeContext(ctx)
				cancel()
				if err != nil {
					b.pool.reject()
					local.Close()
					continue
				}
			}
			remote, err := b.dial(b.ctx)
			if err != nil {
				b.pool.reject()
				local.Close()
				continue
			}
			bounded, err := b.pool.Add(remote)
			if err != nil {
				local.Close()
				continue
			}
			if err = b.pool.Bind(b.id, bounded); err != nil {
				bounded.Close()
				local.Close()
				continue
			}
			var pair sync.WaitGroup
			pair.Add(2)
			copyBytes := func(dst io.Writer, src io.Reader) {
				defer pair.Done()
				_, _ = io.Copy(dst, src)
				local.Close()
				bounded.Close()
			}
			go copyBytes(deadlineConn{local}, bounded)
			go copyBytes(bounded, deadlineConn{local})
			pair.Wait()
			b.mu.Lock()
			b.active = nil
			b.mu.Unlock()
		}
	}()
}
func (b *Bridge) Close() {
	b.once.Do(func() {
		b.cancel()
		b.listener.Close()
		b.mu.Lock()
		if b.active != nil {
			b.active.Close()
		}
		b.mu.Unlock()
		b.pool.CloseAll()
		b.wg.Wait()
	})
}

type deadlineConn struct{ net.Conn }

func (c deadlineConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
func (c deadlineConn) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(65 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}
