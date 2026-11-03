//go:build integration

package fixadapter

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/testfix"
)

// FIX engine stores the outbound Logon as well as application messages. This
// synthetic regression documents why the durable store needs secret protection.
func TestUpstreamLogonCredentialIsPersisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Setenv("FIX_TEST_SYNTHETIC_PASSWORD", "synthetic-review-credential")
	venueListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer venueListener.Close()
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer clientListener.Close()
	c := runtimeConfig(t)
	c.Downstream.Listen = clientListener.Addr().String()
	_, port, _ := net.SplitHostPort(venueListener.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(port)
	c.Upstream.Password = config.Secret{Env: "FIX_TEST_SYNTHETIC_PASSWORD"}
	venue := testfix.NewPeer()
	vs, _, err := testfix.Settings(c.Upstream.Target, c.Upstream.Sender, venueListener.Addr().String(), c.Upstream.Dictionary, false)
	if err != nil {
		t.Fatal(err)
	}
	acceptor, err := testfix.Listen(venue, venueListener, vs)
	if err != nil {
		t.Fatal(err)
	}
	defer acceptor.Stop()
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return clientListener, nil })
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	wait(t, ctx, venue.Logons)
	files, err := filepath.Glob(filepath.Join(c.Upstream.StoreDir, "*", "*.body"))
	if err != nil || len(files) != 1 {
		t.Fatal("upstream store not found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal("cannot read synthetic store")
	}
	if !bytes.Contains(data, []byte("synthetic-review-credential")) {
		t.Fatal("expected synthetic logon credential in store")
	}
}

func TestUpstreamTLSVerifiesIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("local TLS sockets")
	}
	for _, valid := range []bool{true, false} {
		t.Run(strconv.FormatBool(valid), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cert, key := syntheticCertificate(t)
			vl, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer vl.Close()
			ml, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ml.Close()
			c := runtimeConfig(t)
			c.Downstream.Listen = ml.Addr().String()
			_, port, _ := net.SplitHostPort(vl.Addr().String())
			c.Upstream.Port, _ = strconv.Atoi(port)
			c.Upstream.TLSEnabled = true
			c.Upstream.TLS = config.TLS{CAFile: cert, ServerName: "mock.test"}
			if !valid {
				c.Upstream.TLS.ServerName = "wrong.test"
			}
			venue := testfix.NewPeer()
			vs, _, err := testfix.Settings(c.Upstream.Target, c.Upstream.Sender, vl.Addr().String(), c.Upstream.Dictionary, false)
			if err != nil {
				t.Fatal(err)
			}
			vs.GlobalSettings().Set("SocketUseSSL", "Y")
			vs.GlobalSettings().Set("SocketCertificateFile", cert)
			vs.GlobalSettings().Set("SocketPrivateKeyFile", key)
			va, err := testfix.Listen(venue, vl, vs)
			if err != nil {
				t.Fatal(err)
			}
			defer va.Stop()
			r, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return ml, nil })
			if err = r.Start(); err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			if valid {
				wait(t, ctx, venue.Logons)
				waitLogged(t, ctx, r, 1)
				if !r.Snapshot().Ready {
					t.Fatal("verified upstream not ready")
				}
			} else {
				wait(t, ctx, r.Observer.Errors)
				if r.Snapshot().Ready {
					t.Fatal("invalid server identity accepted")
				}
			}
		})
	}
}

func TestDownstreamMutualTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("local TLS sockets")
	}
	for _, identity := range []bool{true, false} {
		t.Run(strconv.FormatBool(identity), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cert, key := syntheticCertificate(t)
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			c := runtimeConfig(t)
			c.Downstream.Listen = l.Addr().String()
			c.Downstream.TLSEnabled = true
			c.Downstream.TLS = config.TLS{CAFile: cert, CertFile: cert, KeyFile: key, RequireClientCert: true}
			r, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			r.SetListenerFactory(func(_ string, secure *tls.Config) (net.Listener, error) { return tls.NewListener(l, secure), nil })
			if err = r.Start(); err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			if !identity {
				data, e := os.ReadFile(cert)
				if e != nil {
					t.Fatal(e)
				}
				roots := x509.NewCertPool()
				roots.AppendCertsFromPEM(data)
				conn, e := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", c.Downstream.Listen, &tls.Config{RootCAs: roots, ServerName: "mock.test", MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
				if e == nil {
					conn.Close()
					t.Fatal("missing client certificate accepted")
				}
				return
			}
			client := testfix.NewPeer()
			cs, _, err := testfix.Settings("CLIENT", c.Downstream.Target, c.Downstream.Listen, c.Downstream.Dictionary, true)
			if err != nil {
				t.Fatal(err)
			}
			cs.GlobalSettings().Set("SocketUseSSL", "Y")
			cs.GlobalSettings().Set("SocketCAFile", cert)
			cs.GlobalSettings().Set("SocketServerName", "mock.test")
			if identity {
				cs.GlobalSettings().Set("SocketCertificateFile", cert)
				cs.GlobalSettings().Set("SocketPrivateKeyFile", key)
			}
			ci, err := testfix.Connect(client, cs)
			if err != nil {
				t.Fatal(err)
			}
			defer ci.Stop()
			wait(t, ctx, client.Logons)
		})
	}
}

func TestOversizeAndUnknownDownstreamFailClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("raw local transport sockets")
	}
	c := runtimeConfig(t)
	c.Service.MaxMessageBytes = 256
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c.Downstream.Listen = l.Addr().String()
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return l, nil })
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	conn, err := net.Dial("tcp", c.Downstream.Listen)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write([]byte("8=FIX.4.4\x019=999999\x01" + strings.Repeat("x", 512)))
	buf := make([]byte, 1)
	if _, err = conn.Read(buf); err == nil {
		t.Fatal("oversize connection remained open")
	}
	conn.Close()
	if r.Transport.Rejected.Load() == 0 {
		t.Fatal("oversize not counted")
	}
	conn, err = net.Dial("tcp", c.Downstream.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	m := framed("A")
	m.Header.SetInt(34, 1)
	m.Header.SetString(49, "UNKNOWN")
	m.Header.SetString(56, c.Downstream.Target)
	m.Header.SetString(52, time.Now().UTC().Format("20060102-15:04:05.000"))
	m.Body.SetInt(98, 0)
	m.Body.SetInt(108, 30)
	if _, err = conn.Write(m.Bytes()); err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(conn)
	if err != nil {
		if n, ok := err.(net.Error); ok && n.Timeout() {
			t.Fatal("unknown connection not closed")
		}
	}
	if len(r.Registry.Snapshot()) != 2 {
		t.Fatal("unknown client provisioned")
	}
}
