package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
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
	if c.Retention.DownstreamWindow != 8*time.Hour || c.Downstream.DuplicatePolicy != "reject-new" || c.Service.ShutdownTimeout != 10*time.Second || c.Upstream.ReconnectInterval != time.Minute {
		t.Fatal("incorrect defaults")
	}
}

func TestExampleMixedClients(t *testing.T) {
	c := sample(t)
	if len(c.Downstream.Clients) != 4 || c.Downstream.Clients[0].ReadOnly || c.Downstream.Clients[1].ReadOnly || !c.Downstream.Clients[2].ReadOnly || c.Downstream.Clients[3].ReadOnly {
		t.Fatal("example does not contain the expected writable and read-only clients")
	}
	client := c.Downstream.Clients[2]
	if client.Sender != "DROP_COPY" || len(client.ReceiveMessageTypes) != 2 || client.ReceiveMessageTypes[0] != "8" || client.ReceiveMessageTypes[1] != "9" {
		t.Fatal("example subscriber does not have the expected receive policy")
	}
}

func TestClientReceivePolicyYAMLRoundTrip(t *testing.T) {
	for _, types := range [][]string{nil, {}, {"8"}} {
		c := sample(t)
		c.Downstream.Clients[0].ReceiveMessageTypes = types
		data, err := yaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Load(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		got := d.Downstream.Clients[0].ReceiveMessageTypes
		if (got == nil) != (types == nil) || len(got) != len(types) {
			t.Fatalf("round trip %v: %v", types, got)
		}
	}
}

func TestClientPolicies(t *testing.T) {
	data, err := os.ReadFile("../../config/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, tc := range []struct {
		name, yaml string
		valid      bool
		nilList    bool
		readOnly   bool
	}{
		{"omitted", "", true, true, false},
		{"empty", "      read_only: true\n      receive_message_types: []\n", true, false, true},
		{"subset", "      receive_message_types: [\"8\", \"9\"]\n", true, false, false},
		{"not-subset", "      receive_message_types: [\"AE\"]\n", false, false, false},
		{"duplicate", "      receive_message_types: [\"8\", \"8\"]\n", false, false, false},
		{"admin", "      receive_message_types: [\"A\"]\n", false, false, false},
		{"invalid-bool", "      read_only: perhaps\n", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yaml := strings.Replace(base, "    - sender_comp_id: DEV_A\n", "    - sender_comp_id: DEV_A\n"+tc.yaml, 1)
			c, err := Load(strings.NewReader(yaml))
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.valid && ((c.Downstream.Clients[0].ReceiveMessageTypes == nil) != tc.nilList || c.Downstream.Clients[0].ReadOnly != tc.readOnly) {
				t.Fatal("policy decoded incorrectly")
			}
		})
	}
	yaml := strings.Replace(base, `app_message_types: ["D", "F", "G", "H", "V", "x"]`, "app_message_types: []", 1)
	c, err := Load(strings.NewReader(yaml))
	if err != nil || len(c.Upstream.AppTypes) != 0 {
		t.Fatalf("empty upstream list: %v", err)
	}
	c = sample(t)
	c.Fanout.AppTypes = append(c.Fanout.AppTypes, "AE", "x")
	for _, tc := range []struct {
		types []string
		valid bool
	}{
		{[]string{"AE", "x", "X"}, true},
		{[]string{"ae"}, false},
		{[]string{""}, false},
	} {
		c.Downstream.Clients[0].ReceiveMessageTypes = tc.types
		if (c.Validate() == nil) != tc.valid {
			t.Fatalf("types %v", tc.types)
		}
	}
}

func TestUpstreamReconnectInterval(t *testing.T) {
	for _, tc := range []struct {
		interval time.Duration
		valid    bool
	}{{0, false}, {-time.Second, false}, {time.Millisecond * 999, false}, {time.Second, true}, {time.Minute, true}, {time.Hour, true}} {
		c := sample(t)
		c.Upstream.ReconnectInterval = tc.interval
		if (c.Validate() == nil) != tc.valid {
			t.Fatalf("unexpected validation for %s", tc.interval)
		}
	}
	if Defaults().Upstream.ReconnectInterval != time.Minute {
		t.Fatal("unexpected default")
	}
}

func TestClientCountLimit(t *testing.T) {
	c := sample(t)
	c.Downstream.Clients = make([]Client, 128)
	for i := range c.Downstream.Clients {
		c.Downstream.Clients[i].Sender = fmt.Sprintf("CLIENT_%d", i)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Downstream.Clients = append(c.Downstream.Clients, Client{Sender: "CLIENT_128", Disabled: true})
	if c.Validate() == nil {
		t.Fatal("more than 128 configured clients accepted")
	}
}

func TestRemoteListenerRequiresClientPasswords(t *testing.T) {
	for _, tc := range []struct {
		name      string
		address   string
		tls       bool
		passwords bool
		valid     bool
	}{
		{"loopback", "127.0.0.1:9879", false, false, true},
		{"ipv6-loopback", "[::1]:9879", false, false, true},
		{"remote-plaintext", "0.0.0.0:9879", false, true, true},
		{"remote-plaintext-missing-passwords", "0.0.0.0:9879", false, false, false},
		{"remote-missing-passwords", "0.0.0.0:9879", true, false, false},
		{"remote-protected", "0.0.0.0:9879", true, true, true},
		{"ipv6-protected", "[::]:9879", true, true, true},
		{"hostname", "localhost:9879", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sample(t)
			c.Downstream.Listen = tc.address
			c.Downstream.TLSEnabled = tc.tls
			if tc.tls {
				c.Downstream.TLS.CertFile, c.Downstream.TLS.KeyFile = "cert", "key"
			}
			if tc.passwords {
				for i := range c.Downstream.Clients {
					c.Downstream.Clients[i].Password.Env = fmt.Sprintf("CLIENT_%d_PASSWORD", i)
				}
			}
			if (c.Validate() == nil) != tc.valid {
				t.Fatal("unexpected validation outcome")
			}
		})
	}
	c := sample(t)
	c.Downstream.Listen, c.Downstream.TLSEnabled = "0.0.0.0:9879", true
	c.Downstream.TLS.CertFile, c.Downstream.TLS.KeyFile = "cert", "key"
	for i := range c.Downstream.Clients {
		c.Downstream.Clients[i].Disabled = true
	}
	c.Downstream.Clients[0].Disabled = false
	c.Downstream.Clients[0].Password.Env = "CLIENT_PASSWORD"
	if err := c.Validate(); err != nil {
		t.Fatal("disabled clients should not require passwords", err)
	}
	c.Downstream.Clients[1].Disabled = false
	if c.Validate() == nil {
		t.Fatal("enabled client without password accepted")
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
		{"message-limit", func(c *Config) { c.Service.MaxMessageBytes = -1 }},
		{"queue", func(c *Config) { c.Service.UpstreamQueue = 0 }},
		{"downstream-queue", func(c *Config) { c.Service.DownstreamQueue = 5000 }},
		{"timeout", func(c *Config) { c.Service.ShutdownTimeout = 0 }},
		{"startup-timeout", func(c *Config) { c.Service.StartupTimeout = 0 }},
		{"version", func(c *Config) { c.Upstream.Version = "FIXT.1.1" }},
		{"cross-version", func(c *Config) { c.Downstream.Version = "FIX.4.2" }},
		{"endpoint", func(c *Config) { c.Upstream.Host = "" }},
		{"port", func(c *Config) { c.Upstream.Port = 65536 }},
		{"compid", func(c *Config) { c.Upstream.Sender = c.Upstream.Target }},
		{"listener", func(c *Config) { c.Downstream.Listen = "localhost:9879" }},
		{"same-listener", func(c *Config) { c.Downstream.Listen = c.Service.HealthBind }},
		{"target", func(c *Config) { c.Downstream.Target = "" }},
		{"empty-allowlist", func(c *Config) { c.Downstream.Clients = nil }},
		{"too-many", func(c *Config) {
			c.Downstream.Clients = make([]Client, 129)
			for i := range c.Downstream.Clients {
				c.Downstream.Clients[i].Sender = fmt.Sprintf("CLIENT_%d", i)
			}
		}},
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
	if _, err := Load(strings.NewReader(strings.Repeat(" ", 1<<20) + "x")); err == nil {
		t.Fatal("oversize configuration accepted")
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
