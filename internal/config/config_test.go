package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func sample(t *testing.T) Config {
	t.Helper()
	f, err := os.Open("../../config/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSample(t *testing.T) {
	c := sample(t)
	if c.Retention.DownstreamWindow != 8*time.Hour || c.Downstream.DuplicatePolicy != "reject-new" || c.Service.ShutdownTimeout != 10*time.Second {
		t.Fatal("incorrect defaults")
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"mode", func(c *Config) { c.Service.Mode = "production" }},
		{"name", func(c *Config) { c.Service.Name = "bad\nname" }},
		{"bind", func(c *Config) { c.Service.Bind = "0.0.0.0" }},
		{"health", func(c *Config) { c.Service.HealthBind = ":8080" }},
		{"client-limit", func(c *Config) { c.Service.MaxClients = 0 }},
		{"message-limit", func(c *Config) { c.Service.MaxMessageBytes = -1 }},
		{"queue", func(c *Config) { c.Service.UpstreamQueue = 0 }},
		{"downstream-queue", func(c *Config) { c.Service.DownstreamQueue = 5000 }},
		{"timeout", func(c *Config) { c.Service.ShutdownTimeout = 0 }},
		{"version", func(c *Config) { c.Upstream.Version = "FIXT.1.1" }},
		{"cross-version", func(c *Config) { c.Downstream.Version = "FIX.4.2" }},
		{"endpoint", func(c *Config) { c.Upstream.Host = "" }},
		{"port", func(c *Config) { c.Upstream.Port = 65536 }},
		{"compid", func(c *Config) { c.Upstream.Sender = c.Upstream.Target }},
		{"listener", func(c *Config) { c.Downstream.Listen = "localhost:9879" }},
		{"same-listener", func(c *Config) { c.Downstream.Listen = c.Service.HealthBind }},
		{"target", func(c *Config) { c.Downstream.Target = "" }},
		{"empty-allowlist", func(c *Config) { c.Downstream.Clients = nil }},
		{"too-many", func(c *Config) { c.Service.MaxClients = 1 }},
		{"same-client", func(c *Config) { c.Downstream.Clients[1].Sender = c.Downstream.Clients[0].Sender }},
		{"self-client", func(c *Config) { c.Downstream.Clients[0].Sender = c.Downstream.Target }},
		{"client-secret", func(c *Config) { c.Downstream.Clients[0].Password = Secret{Env: "PASSWORD", File: "password.txt"} }},
		{"overlap", func(c *Config) {
			c.Downstream.Target = c.Upstream.Sender
			c.Downstream.Clients[0].Sender = c.Upstream.Target
		}},
		{"duplicate-policy", func(c *Config) { c.Downstream.DuplicatePolicy = "replace" }},
		{"store", func(c *Config) { c.Upstream.StoreDir = c.Downstream.StoreDir }},
		{"fanout", func(c *Config) { c.Fanout.Mode = "offline" }},
		{"admin", func(c *Config) { c.Fanout.AppTypes = []string{"A"} }},
		{"invalid-type", func(c *Config) { c.Upstream.AppTypes = []string{"D\n"} }},
		{"duplicate-type", func(c *Config) { c.Upstream.AppTypes = []string{"D", "D"} }},
		{"empty-policy", func(c *Config) { c.Fanout.AppTypes = nil }},
		{"retention", func(c *Config) { c.Retention.DownstreamWindow = 0 }},
		{"ttl", func(c *Config) { c.Retention.IdleTTL = -time.Hour }},
		{"prune", func(c *Config) { c.Retention.PruneActive = true }},
		{"budget", func(c *Config) { c.Retention.DiskLimitBytes = 1 }},
		{"log", func(c *Config) { c.Logging.Mode = "raw" }},
		{"redact", func(c *Config) { c.Logging.RedactTags = []string{"-1"} }},
		{"secret", func(c *Config) { c.Upstream.Password = Secret{Env: "PASSWORD", File: "password.txt"} }},
		{"tls-pair", func(c *Config) { c.Upstream.TLS.CertFile = "cert.pem" }},
		{"tls", func(c *Config) { c.Downstream.TLSEnabled = true }},
		{"mtls", func(c *Config) { c.Downstream.TLS.RequireClientCert = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := sample(t)
			tt.mutate(&c)
			if c.Validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestLoadErrorsHideValues(t *testing.T) {
	for _, input := range []string{"", "service: [", "upstream:\n  password: secret-that-must-not-leak", "unknown: secret-that-must-not-leak", "{}\n---\n{}", "service:\n  max_clients: secret-that-must-not-leak"} {
		_, err := Load(strings.NewReader(input))
		if err == nil || strings.Contains(err.Error(), "secret-that-must-not-leak") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}

func TestOptionalHealthAndAddresses(t *testing.T) {
	c := sample(t)
	c.Service.HealthBind = ""
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"broken", "127.0.0.1:bad", "127.0.0.1:0", "[::1]:70000", "8.8.8.8:100"} {
		if loopbackAddress(a) {
			t.Fatalf("accepted %s", a)
		}
	}
	if !loopbackAddress("[::1]:8080") {
		t.Fatal("IPv6 loopback rejected")
	}
	for _, typ := range []string{"0", "1", "2", "3", "4", "5", "A"} {
		if !IsAdmin(typ) {
			t.Fatal(typ)
		}
	}
}
