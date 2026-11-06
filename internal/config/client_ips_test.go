package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClientIPPrefixes(t *testing.T) {
	for _, tc := range []struct {
		name string
		ips  []string
		want []string
		bad  bool
	}{
		{name: "omitted"},
		{name: "deny-all", ips: []string{}, want: []string{}},
		{name: "addresses", ips: []string{"192.0.2.1", "2001:db8::1"}, want: []string{"192.0.2.1/32", "2001:db8::1/128"}},
		{name: "ranges", ips: []string{"192.0.2.45/24", "2001:db8:1::2/48"}, want: []string{"192.0.2.0/24", "2001:db8:1::/48"}},
		{name: "mapped", ips: []string{"::ffff:192.0.2.1", "::ffff:198.51.100.22/120"}, want: []string{"192.0.2.1/32", "198.51.100.0/24"}},
		{name: "all-addresses", ips: []string{"0.0.0.0/0", "::/0", "::ffff:0.0.0.0/96"}, bad: true}, // duplicate normalized IPv4 range
		{name: "duplicate-address", ips: []string{"192.0.2.1", "192.0.2.1/32"}, bad: true},
		{name: "duplicate-range", ips: []string{"192.0.2.0/24", "192.0.2.1/24"}, bad: true},
		{name: "duplicate-mapped", ips: []string{"192.0.2.1", "::ffff:192.0.2.1"}, bad: true},
		{name: "hostname", ips: []string{"client.example.com"}, bad: true},
		{name: "wildcard", ips: []string{"192.0.2.*"}, bad: true},
		{name: "port", ips: []string{"192.0.2.1:9879"}, bad: true},
		{name: "zone", ips: []string{"fe80::1%eth0"}, bad: true},
		{name: "zone-range", ips: []string{"fe80::1%eth0/64"}, bad: true},
		{name: "empty-entry", ips: []string{""}, bad: true},
		{name: "whitespace", ips: []string{" 192.0.2.1"}, bad: true},
		{name: "prefix-too-long", ips: []string{"192.0.2.1/33"}, bad: true},
		{name: "mapped-too-wide", ips: []string{"::ffff:192.0.2.1/95"}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := Client{AllowedIPs: tc.ips}
			got, err := client.IPPrefixes()
			if (err != nil) != tc.bad {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.bad {
				if (got == nil) != (tc.want == nil) || len(got) != len(tc.want) {
					t.Fatalf("prefixes %v, want %v", got, tc.want)
				}
				for i, value := range tc.want {
					if got[i] != netip.MustParsePrefix(value) {
						t.Fatalf("prefixes %v, want %v", got, tc.want)
					}
				}
			}
			c := sample(t)
			c.Downstream.Clients[0] = Client{Sender: "DEV_A", AllowedIPs: tc.ips}
			if (c.Validate() != nil) != tc.bad {
				t.Fatal("configuration did not validate the IP policy")
			}
		})
	}
	client := Client{AllowedIPs: make([]string, 128)}
	for i := range client.AllowedIPs {
		client.AllowedIPs[i] = fmt.Sprintf("192.0.2.%d", i)
	}
	if _, err := client.IPPrefixes(); err != nil {
		t.Fatal(err)
	}
	client.AllowedIPs = append(client.AllowedIPs, "198.51.100.1")
	if _, err := client.IPPrefixes(); err == nil {
		t.Fatal("unbounded IP list accepted")
	}
}

func TestClientAllowedIPsYAMLRoundTrip(t *testing.T) {
	for _, ips := range [][]string{nil, {}, {"192.0.2.1", "2001:db8::/32"}} {
		c := sample(t)
		c.Downstream.Clients[0].AllowedIPs = ips
		data, err := yaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Load(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		got := decoded.Downstream.Clients[0].AllowedIPs
		if (got == nil) != (ips == nil) || strings.Join(got, ",") != strings.Join(ips, ",") {
			t.Fatalf("round trip changed IP policy: %v -> %v", ips, got)
		}
	}
	for _, field := range []string{"allowed_ips: [synthetic-invalid-sensitive-value]", "allowed_ips: invalid", "allowed_ips: [192.0.2.1/33]"} {
		data, err := os.ReadFile("../../config/example.yaml")
		if err != nil {
			t.Fatal(err)
		}
		input := strings.Replace(strings.ReplaceAll(string(data), "\r\n", "\n"), "    - sender_comp_id: DEV_A\n", "    - sender_comp_id: DEV_A\n      "+field+"\n", 1)
		_, err = Load(strings.NewReader(input))
		if err == nil || strings.Contains(err.Error(), "synthetic-invalid-sensitive-value") {
			t.Fatalf("unsafe validation error: %v", err)
		}
	}
}
