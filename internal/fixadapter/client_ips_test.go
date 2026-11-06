package fixadapter

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
)

type sourceConn struct {
	net.Conn
	source net.Addr
}

func (c sourceConn) RemoteAddr() net.Addr { return c.source }

func pooledSource(t *testing.T, pool *transport.Pool, source net.Addr) *transport.Conn {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { b.Close() })
	c, err := pool.Add(sourceConn{Conn: a, source: source})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestClientIPValidation(t *testing.T) {
	id := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "GATE", TargetCompID: "CLIENT_A"}
	for _, tc := range []struct {
		name    string
		ips     []string
		source  net.Addr
		allowed bool
	}{
		{"unrestricted", nil, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}, true},
		{"unrestricted-no-ip", nil, nil, true},
		{"deny-all", []string{}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}, false},
		{"exact-ipv4", []string{"192.0.2.1"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 12345}, true},
		{"wrong-ipv4", []string{"192.0.2.1"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.2")}, false},
		{"ipv4-range-first", []string{"192.0.2.0/24"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.0")}, true},
		{"ipv4-range-last", []string{"192.0.2.0/24"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.255")}, true},
		{"outside-range", []string{"192.0.2.0/24"}, &net.TCPAddr{IP: net.ParseIP("192.0.3.0")}, false},
		{"ipv6", []string{"2001:db8::1"}, &net.TCPAddr{IP: net.ParseIP("2001:db8::1")}, true},
		{"ipv6-range", []string{"2001:db8::/32"}, &net.TCPAddr{IP: net.ParseIP("2001:db8:abcd::1")}, true},
		{"ipv6-outside-range", []string{"2001:db8::/32"}, &net.TCPAddr{IP: net.ParseIP("2001:db9::1")}, false},
		{"ipv6-zone-not-identity", []string{"fe80::1"}, &net.TCPAddr{IP: net.ParseIP("fe80::1"), Zone: "test"}, true},
		{"mapped-source", []string{"192.0.2.0/24"}, &net.TCPAddr{IP: net.ParseIP("::ffff:192.0.2.1")}, true},
		{"mapped-policy", []string{"::ffff:192.0.2.0/120"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}, true},
		{"second-range", []string{"198.51.100.1", "192.0.2.0/24"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}, true},
		{"ipv4-is-not-ipv6", []string{"::/0"}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}, false},
		{"missing-source", []string{"0.0.0.0/0", "::/0"}, nil, false},
		{"nil-tcp-source", []string{"0.0.0.0/0"}, (*net.TCPAddr)(nil), false},
		{"invalid-tcp-source", []string{"0.0.0.0/0"}, &net.TCPAddr{}, false},
		{"non-tcp-source", []string{"0.0.0.0/0"}, &net.UnixAddr{Name: "synthetic-non-ip"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefixes, err := (config.Client{AllowedIPs: tc.ips}).IPPrefixes()
			if err != nil {
				t.Fatal(err)
			}
			pool := transport.New(4, 4096, nil)
			t.Cleanup(pool.CloseAll)
			var out bytes.Buffer
			observer := observability.New(&out)
			v := connectionValidator{pool: pool, allowedIPs: map[string][]netip.Prefix{id.String(): prefixes}, observer: observer}
			conn := pooledSource(t, pool, tc.source)
			if err = v.Validate(conn, id); (err == nil) != tc.allowed {
				t.Fatalf("validation: %v", err)
			}
			if tc.allowed {
				if observer.IPDenials.Load() != 0 || out.Len() != 0 {
					t.Fatal("allowed connection logged as denied")
				}
			} else {
				var event map[string]any
				if observer.IPDenials.Load() != 1 || json.Unmarshal(out.Bytes(), &event) != nil || event["msg"] != "connection_ip_denied" || event["session"] != observability.Label(id.String()) || event["source_ip"] == nil {
					t.Fatal("missing IP denial diagnostic")
				}
				if strings.Contains(out.String(), id.TargetCompID) {
					t.Fatal("raw session identity leaked")
				}
				// A rejected socket never occupies the session's binding.
				if err = pool.Bind(id.String(), pooledSource(t, pool, tc.source)); err != nil {
					t.Fatalf("denied connection held the session: %v", err)
				}
			}
		})
	}
}

func TestClientIPIsolationAndReconnect(t *testing.T) {
	id := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "GATE", TargetCompID: "CLIENT_A"}
	other := id
	other.TargetCompID = "CLIENT_B"
	unknown := id
	unknown.TargetCompID = "UNKNOWN_OR_DISABLED"
	pool := transport.New(20, 4096, nil)
	defer pool.CloseAll()
	v := connectionValidator{pool: pool, allowedIPs: map[string][]netip.Prefix{
		id.String():    {netip.MustParsePrefix("192.0.2.1/32")},
		other.String(): {netip.MustParsePrefix("198.51.100.1/32")},
	}}
	source := &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}
	conn := pooledSource(t, pool, source)
	if v.Validate(conn, other) == nil || v.Validate(conn, unknown) == nil {
		t.Fatal("source permitted for a different or unknown client")
	}
	if err := v.Validate(conn, id); err != nil {
		t.Fatal(err)
	}
	if v.Validate(pooledSource(t, pool, source), id) == nil {
		t.Fatal("duplicate live session accepted")
	}
	conn.Close()
	if err := v.Validate(pooledSource(t, pool, source), id); err != nil {
		t.Fatalf("permitted reconnect failed: %v", err)
	}
}

func TestClientIPConcurrentDenials(t *testing.T) {
	id := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "GATE", TargetCompID: "CLIENT_A"}
	observer := observability.New(io.Discard)
	v := connectionValidator{allowedIPs: map[string][]netip.Prefix{id.String(): {}}, observer: observer}
	conn := sourceConn{source: &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v.Validate(conn, id) == nil {
				t.Error("concurrent denied connection accepted")
			}
		}()
	}
	wg.Wait()
	if observer.IPDenials.Load() != 100 {
		t.Fatal("lost concurrent denials")
	}
}

func TestRuntimeCopiesIPPolicyAndExcludesDisabledClients(t *testing.T) {
	c := runtimeConfig(t)
	c.Downstream.Clients = []config.Client{
		{Sender: "CLIENT", AllowedIPs: []string{"192.0.2.1"}},
		{Sender: "DENY_ALL", AllowedIPs: []string{}},
		{Sender: "UNRESTRICTED"},
		{Sender: "DISABLED", Disabled: true, AllowedIPs: []string{}},
	}
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	c.Downstream.Clients[0].AllowedIPs[0] = "198.51.100.1"
	if len(r.clientIPs) != 3 {
		t.Fatal("disabled client provisioned with an IP policy")
	}
	for _, tc := range []struct {
		client string
		nilIPs bool
		count  int
	}{{"CLIENT", false, 1}, {"DENY_ALL", false, 0}, {"UNRESTRICTED", true, 0}} {
		id := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: c.Downstream.Target, TargetCompID: tc.client}
		prefixes, ok := r.clientIPs[id.String()]
		if !ok || (prefixes == nil) != tc.nilIPs || len(prefixes) != tc.count {
			t.Fatal("runtime lost omitted/empty IP-policy distinction")
		}
		if len(prefixes) == 1 && prefixes[0] != netip.MustParsePrefix("192.0.2.1/32") {
			t.Fatal("runtime policy changed when input config was mutated")
		}
	}
}
