package fixadapter

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/maintenance"
	"fix-multiplexer/internal/retention"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
)

func TestTransportLimitUsesEnabledClients(t *testing.T) {
	c := runtimeConfig(t)
	c.Downstream.Clients = append(c.Downstream.Clients, config.Client{Sender: "DISABLED", Disabled: true})
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	for i := 0; i < 3; i++ {
		local, remote := net.Pipe()
		defer remote.Close()
		conn, err := r.Transport.Add(local)
		if i < 2 && err != nil {
			t.Fatal(err)
		}
		if i == 2 && !errors.Is(err, transport.ErrLimit) {
			t.Fatal("expected two-connection limit", err)
		}
		if conn != nil {
			defer conn.Close()
		}
	}
}

func testStore(t *testing.T, window time.Duration) (*guardedStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	m, err := retention.Open([]string{root}, 100000, 10000, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	if _, err = m.Register("test", root); err != nil {
		t.Fatal(err)
	}
	base, err := quickfix.NewMemoryStoreFactory().Create(quickfix.SessionID{})
	if err != nil {
		t.Fatal(err)
	}
	return &guardedStore{MessageStore: base, manager: m, id: "test", window: window, now: func() time.Time { return now }}, &now
}

func TestGuardedStoreRecoveryAndExpiry(t *testing.T) {
	s, now := testStore(t, 8*time.Hour)
	msg := framed("8")
	msg.Header.SetString(52, now.Format("20060102-15:04:05.000"))
	b := msg.Bytes()
	if err := s.SaveMessageAndIncrNextSenderMsgSeqNum(1, b); err != nil {
		t.Fatal(err)
	}
	if s.NextSenderMsgSeqNum() != 2 {
		t.Fatal("sender sequence")
	}
	if err := s.IncrNextTargetMsgSeqNum(); err != nil {
		t.Fatal(err)
	}
	if s.NextTargetMsgSeqNum() != 2 {
		t.Fatal("target sequence not advanced")
	}
	if err := s.IncrNextSenderMsgSeqNum(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNextSenderMsgSeqNum(3); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNextTargetMsgSeqNum(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMessage(2, b); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMessages(1, 2)
	if err != nil || len(got) != 2 {
		t.Fatal("replay failed", err, len(got))
	}
	*now = now.Add(8 * time.Hour)
	if _, err = s.GetMessages(1, 1); err != nil {
		t.Fatal("boundary should be retained", err)
	}
	*now = now.Add(time.Nanosecond)
	if _, err = s.GetMessages(1, 1); !errors.Is(err, retention.ErrExpired) {
		t.Fatal("expiry hidden", err)
	}
	if s.manager.CanUse("test") {
		t.Fatal("expired store still usable")
	}
	if s.Refresh() == nil || s.SaveMessage(3, b) == nil || s.Reset() == nil {
		t.Fatal("retired store reused")
	}
	if _, err = s.GetMessages(1, 1); err == nil {
		t.Fatal("retired replay")
	}
}

func TestGuardedStoreUpstreamAndCorruption(t *testing.T) {
	s, now := testStore(t, 0)
	if err := s.SaveMessage(1, framed("8").Bytes()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(48 * time.Hour)
	if _, err := s.GetMessages(1, 1); err != nil {
		t.Fatal("downstream age applied upstream")
	}
	s, now = testStore(t, 8*time.Hour)
	if err := s.SaveMessage(1, framed("0").Bytes()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(48 * time.Hour)
	if _, err := s.GetMessages(1, 1); err != nil {
		t.Fatal("admin gap history unnecessarily expired")
	}
	if err := s.SaveMessage(2, []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMessages(2, 2); !errors.Is(err, retention.ErrStorage) || s.manager.Healthy() {
		t.Fatal("corruption hidden", err)
	}
}

type refreshFailure struct{ quickfix.MessageStore }

func (refreshFailure) Refresh() error { return errors.New("synthetic refresh error") }

type closeFailure struct{ quickfix.MessageStore }

func (closeFailure) Close() error { return errors.New("synthetic close failure") }

func TestDurableShutdownFailureIsVisibleAndIdempotent(t *testing.T) {
	r, err := New(runtimeConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	r.stores = append(r.stores, closeFailure{})
	if r.Stop() == nil || r.Stop() == nil || r.Observer.EngineErrors.Load() == 0 {
		t.Fatal("durable close failure hidden")
	}
}

func TestGuardedStoreReadFailures(t *testing.T) {
	s, _ := testStore(t, time.Hour)
	s.MessageStore = refreshFailure{s.MessageStore}
	if s.Refresh() == nil || s.manager.Healthy() {
		t.Fatal("refresh failure hidden")
	}
	s, _ = testStore(t, time.Hour)
	m := framed("8")
	m.Header.Remove(52)
	if err := s.SaveMessage(1, m.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMessages(1, 1); err == nil || s.manager.Healthy() {
		t.Fatal("missing replay time accepted")
	}
	s, _ = testStore(t, 0)
	if err := s.SaveMessage(1, framed("8").Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := s.IterateMessages(1, 1, func([]byte) error { return errors.New("synthetic replay failure") }); err == nil || s.manager.Healthy() {
		t.Fatal("replay failure hidden")
	}
}

func TestSettingsTLSAndDisabledClients(t *testing.T) {
	c := runtimeConfig(t)
	c.Upstream.TLSEnabled = true
	c.Upstream.TLS = config.TLS{CAFile: "synthetic-ca", CertFile: "synthetic-cert", KeyFile: "synthetic-key", ServerName: "synthetic-host"}
	c.Downstream.Clients = append(c.Downstream.Clients, config.Client{Sender: "DISABLED", Disabled: true})
	_, d, err := Settings(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.SessionSettings()) != 1 {
		t.Fatal("disabled session provisioned")
	}
	g := quickfix.NewSessionSettings()
	setTLS(g, true, c.Upstream.TLS)
	c.Upstream.TLS.RequireClientCert = true
	setTLS(g, true, c.Upstream.TLS)
	flag, err := g.Setting("SocketUseSSL")
	if err != nil || flag != "N" {
		t.Fatal("mTLS client identity not required")
	}
	for _, key := range []string{"SocketUseSSL", "SocketCAFile", "SocketCertificateFile", "SocketPrivateKeyFile", "SocketServerName"} {
		if !g.HasSetting(key) {
			t.Fatal(key)
		}
	}
	if _, err := ResolveSecret(config.Secret{File: t.TempDir()}); err == nil {
		t.Fatal("directory used as credential file")
	}
}

func TestRuntimeReleasesUnstartedResources(t *testing.T) {
	c := runtimeConfig(t)
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.Stop()
	r.Stop()
	if r.Start() == nil {
		t.Fatal("restarted stopped runtime")
	}
	r, err = New(c)
	if err != nil {
		t.Fatal("store lease leaked", err)
	}
	defer r.Stop()
	if _, err := New(c); err == nil {
		t.Fatal("concurrent process store access accepted")
	}
}

func TestEngineStoresAreActuallyIsolatedAndPruned(t *testing.T) {
	c := runtimeConfig(t)
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{c.Upstream.StoreDir, c.Downstream.StoreDir} {
		files, err := filepath.Glob(filepath.Join(root, "*", "*.body"))
		if err != nil || len(files) != 1 {
			t.Fatal("engine did not use isolated session directory", files, err)
		}
		direct, _ := filepath.Glob(filepath.Join(root, "*.body"))
		if len(direct) != 0 {
			t.Fatal("legacy root-level store bypassed retention")
		}
	}
	r.Stop()
	c.Retention.IdleTTL = time.Nanosecond
	if err = maintenance.Run(c, "prune", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(c.Downstream.StoreDir, "*", "*.body"))
	if len(files) != 0 {
		t.Fatal("prune did not remove actual engine history")
	}
	up, _ := filepath.Glob(filepath.Join(c.Upstream.StoreDir, "*", "*.body"))
	if len(up) != 1 {
		t.Fatal("prune touched upstream")
	}
	if next, err := New(c); err == nil {
		next.Stop()
		t.Fatal("retired engine session resurrected")
	}
	if err = maintenance.Run(c, "reset", "CLIENT", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
}

func runtimeConfig(t *testing.T) config.Config {
	t.Helper()
	c := config.Defaults()
	c.Upstream.Host = "127.0.0.1"
	c.Upstream.Port = 9880
	c.Upstream.Sender = "RUNTIME_UP"
	c.Upstream.Target = "VENUE"
	c.Downstream.Listen = "127.0.0.1:9879"
	c.Downstream.Target = "RUNTIME_DOWN"
	c.Downstream.Clients = []config.Client{{Sender: "CLIENT"}}
	root := t.TempDir()
	c.Upstream.StoreDir = filepath.Join(root, "up")
	c.Downstream.StoreDir = filepath.Join(root, "down")
	c.Upstream.Dictionary = "../../spec/FIX44.xml"
	c.Downstream.Dictionary = c.Upstream.Dictionary
	return c
}

func TestRuntimeStartupFailuresCleanUp(t *testing.T) {
	for _, kind := range []string{"config", "dictionary", "mismatch", "secret", "client-secret", "bad-store", "listener"} {
		t.Run(kind, func(t *testing.T) {
			c := runtimeConfig(t)
			switch kind {
			case "config":
				c.Upstream.Version = "FIX.4.2"
			case "dictionary":
				c.Upstream.Dictionary = "missing"
			case "mismatch":
				c.Downstream.Dictionary = ""
			case "secret":
				c.Upstream.Password = config.Secret{Env: "MUX_TEST_MISSING_ENV"}
			case "client-secret":
				c.Downstream.Clients[0].Password = config.Secret{File: "missing"}
			case "bad-store":
				if err := os.WriteFile(filepath.Dir(c.Upstream.StoreDir)+"/bad", nil, 0600); err != nil {
					t.Fatal(err)
				}
				c.Upstream.StoreDir = filepath.Dir(c.Upstream.StoreDir) + "/bad/sub"
			}
			r, err := New(c)
			if kind != "listener" {
				if err == nil {
					r.Stop()
					t.Fatal("invalid runtime started")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return nil, errors.New("synthetic bind failure") })
			if r.Start() == nil {
				t.Fatal("bind error hidden")
			}
			r.Stop()
			r, err = New(c)
			if err != nil {
				t.Fatal("startup failed to release state", err)
			}
			r.Stop()
		})
	}
}

func TestUpstreamTLSPreparationAndSafeFailures(t *testing.T) {
	cert, key := syntheticCertificate(t)
	for _, kind := range []string{"valid", "missing-ca", "invalid-ca", "missing-key"} {
		t.Run(kind, func(t *testing.T) {
			c := runtimeConfig(t)
			c.Upstream.TLSEnabled = true
			c.Upstream.TLS = config.TLS{CAFile: cert, CertFile: cert, KeyFile: key}
			switch kind {
			case "missing-ca":
				c.Upstream.TLS.CAFile = "synthetic-missing-ca"
			case "invalid-ca":
				path := filepath.Join(t.TempDir(), "ca")
				if err := os.WriteFile(path, []byte("invalid synthetic PEM"), 0600); err != nil {
					t.Fatal(err)
				}
				c.Upstream.TLS.CAFile = path
			case "missing-key":
				c.Upstream.TLS.KeyFile = "synthetic-missing-key"
			}
			r, err := New(c)
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				r.Stop()
			} else {
				if err == nil {
					r.Stop()
					t.Fatal("invalid TLS accepted")
				}
				if strings.Contains(err.Error(), "synthetic-missing") {
					t.Fatal("credential location leaked")
				}
			}
		})
	}
}
