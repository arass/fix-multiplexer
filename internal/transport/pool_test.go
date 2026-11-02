package transport

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
)

func TestBoundsAndDuplicateConnections(t *testing.T) {
	var failed atomic.Int64
	p := New(2, 4, func() { failed.Add(1) })
	a, b := net.Pipe()
	defer b.Close()
	c, err := p.Add(a)
	if err != nil {
		t.Fatal(err)
	}
	defer p.CloseAll()
	if err = p.Bind("A", c); err != nil {
		t.Fatal(err)
	}
	x, y := net.Pipe()
	defer y.Close()
	other, err := p.Add(x)
	if err != nil {
		t.Fatal(err)
	}
	if p.Bind("A", other) == nil || p.Bind("invalid", y) == nil {
		t.Fatal("duplicate/foreign connection accepted")
	}
	x, y = net.Pipe()
	defer y.Close()
	if _, err = p.Add(x); !errors.Is(err, ErrLimit) {
		t.Fatal("connection cap", err)
	}
	done := make(chan error, 1)
	go func() { _, err := b.Write([]byte("12345")); done <- err }()
	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if n != 4 || err != nil {
		t.Fatal(n, err)
	}
	if _, err = c.Read(buf); !errors.Is(err, ErrLimit) {
		t.Fatal("byte cap", err)
	}
	<-done
	if failed.Load() != 2 || p.Rejected.Load() != 2 {
		t.Fatal("limit failures not counted")
	}
	c.Close()
	c.Close()
}

func TestConsumptionAllowsFurtherReads(t *testing.T) {
	p := New(1, 4, nil)
	a, b := net.Pipe()
	defer b.Close()
	c, err := p.Add(a)
	if err != nil {
		t.Fatal(err)
	}
	defer p.CloseAll()
	if err = p.Bind("A", c); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := b.Write([]byte("12345678")); done <- err }()
	buf := make([]byte, 4)
	if n, err := c.Read(buf); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	p.Consume("A", 4)
	if n, err := c.Read(buf); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	p.Consume("A", 100)
	p.Consume("unknown", 100)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	go func() { _, err := b.Read(buf); done <- err }()
	if _, err = c.Write([]byte("ack")); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestListenerWrapper(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := New(1, 1024, nil)
	wrapped := p.Wrap(l)
	defer wrapped.Close()
	defer p.CloseAll()
	connected := make(chan net.Conn, 1)
	go func() { c, _ := net.Dial("tcp", l.Addr().String()); connected <- c }()
	c, err := wrapped.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	peer := <-connected
	peer.Close()
	wrapped.Close()
	if _, err = wrapped.Accept(); err == nil {
		t.Fatal("closed listener accepted")
	}
}
