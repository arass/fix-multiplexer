//go:build integration

package fixadapter

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestUpstreamReconnectIntervalOnSocketFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	venue, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer venue.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	arrivals := make(chan time.Time, 2)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := venue.Accept()
			if err != nil {
				return
			}
			arrivals <- time.Now()
			conn.Close()
		}
	}()
	c := runtimeConfig(t)
	c.Upstream.ReconnectInterval = 2 * time.Second
	_, port, _ := net.SplitHostPort(venue.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(port)
	c.Downstream.Listen = listener.Addr().String()
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return listener, nil })
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	first := wait(t, ctx, arrivals)
	second := wait(t, ctx, arrivals)
	if delta := second.Sub(first); delta < 1900*time.Millisecond {
		t.Fatalf("upstream reconnected before configured interval: %s", delta)
	}
}
