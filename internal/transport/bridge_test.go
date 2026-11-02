package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestBridgeCopiesOpaqueBytesAndCloses(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backend, remote := net.Pipe()
	defer backend.Close()
	p := New(1, 64, nil)
	b := NewBridge(l, p, "up", func(context.Context) (net.Conn, error) { return remote, nil })
	b.Start()
	defer b.Close()
	client, err := net.Dial("tcp", b.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(backend, buf); err != nil {
			done <- err
			return
		}
		if string(buf) != "ping" {
			done <- errors.New("opaque bytes changed")
			return
		}
		_, err := backend.Write([]byte("pong"))
		done <- err
	}()
	if _, err = client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err = io.ReadFull(client, buf); err != nil || string(buf) != "pong" {
		t.Fatal("bridge response", err)
	}
	p.Consume("up", 4)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	b.Close()
	b.Close()
}

func TestBridgeDialFailureAndByteCap(t *testing.T) {
	for _, dialFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "dial", false: "bytes"}[dialFailure], func(t *testing.T) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			backend, remote := net.Pipe()
			defer backend.Close()
			defer remote.Close()
			p := New(1, 4, nil)
			b := NewBridge(l, p, "up", func(context.Context) (net.Conn, error) {
				if dialFailure {
					return nil, errors.New("synthetic dial failure")
				}
				return remote, nil
			})
			b.Start()
			defer b.Close()
			client, err := net.Dial("tcp", b.Address())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			client.SetDeadline(time.Now().Add(3 * time.Second))
			if !dialFailure {
				go func() { _, _ = backend.Write([]byte("12345678")) }()
			}
			_, err = io.ReadAll(client)
			if err != nil {
				if e, ok := err.(net.Error); ok && e.Timeout() {
					t.Fatal("transport did not fail closed")
				}
			}
			if p.Rejected.Load() != 1 {
				t.Fatal("transport error not counted")
			}
		})
	}
}
