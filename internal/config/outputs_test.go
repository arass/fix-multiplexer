package config

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOutputConfiguration(t *testing.T) {
	valid := Output{Name: "capture", Type: "file", Directory: "./captures"}
	for _, tc := range []struct {
		name   string
		change func(*Config)
		bad    bool
	}{
		{"defaults", func(*Config) {}, false},
		{"empty-filter", func(c *Config) { c.Outputs[0].ReceiveMessageTypes = []string{} }, false},
		{"subset", func(c *Config) { c.Outputs[0].ReceiveMessageTypes = []string{"8", "9"} }, false},
		{"type", func(c *Config) { c.Outputs[0].Type = "network" }, true},
		{"name", func(c *Config) { c.Outputs[0].Name = "bad\nname" }, true},
		{"format", func(c *Config) { c.Outputs[0].Format = "csv" }, true},
		{"missing-dir", func(c *Config) { c.Outputs[0].Directory = "" }, true},
		{"store-dir", func(c *Config) { c.Outputs[0].Directory = c.Upstream.StoreDir }, true},
		{"store-parent", func(c *Config) { c.Outputs[0].Directory = filepath.Dir(c.Downstream.StoreDir) }, true},
		{"store-child", func(c *Config) { c.Outputs[0].Directory = filepath.Join(c.Upstream.StoreDir, "capture") }, true},
		{"duplicate-name", func(c *Config) { x := valid; x.Directory = "./other-captures"; c.Outputs = append(c.Outputs, x) }, true},
		{"duplicate-dir", func(c *Config) { x := valid; x.Name = "other"; c.Outputs = append(c.Outputs, x) }, true},
		{"nested-dir", func(c *Config) {
			x := valid
			x.Name = "other"
			x.Directory = "./captures/sub"
			c.Outputs = append(c.Outputs, x)
		}, true},
		{"admin", func(c *Config) { c.Outputs[0].ReceiveMessageTypes = []string{"A"} }, true},
		{"duplicate-type", func(c *Config) { c.Outputs[0].ReceiveMessageTypes = []string{"8", "8"} }, true},
		{"not-subset", func(c *Config) { c.Outputs[0].ReceiveMessageTypes = []string{"AE"} }, true},
		{"queue-negative", func(c *Config) { c.Outputs[0].QueueMessages = -1 }, true},
		{"queue-large", func(c *Config) { c.Outputs[0].QueueMessages = 4097 }, true},
		{"bytes-small", func(c *Config) { c.Outputs[0].QueueBytes = 4095 }, true},
		{"bytes-large", func(c *Config) { c.Outputs[0].QueueBytes = 65 << 20 }, true},
		{"file-small", func(c *Config) { c.Outputs[0].MaxFileBytes = 4095 }, true},
		{"file-large", func(c *Config) { c.Outputs[0].MaxFileBytes = 2 << 30 }, true},
		{"count-negative", func(c *Config) { c.Outputs[0].MaxFiles = -1 }, true},
		{"count-large", func(c *Config) { c.Outputs[0].MaxFiles = 129 }, true},
		{"disk-budget", func(c *Config) { c.Outputs[0].MaxFileBytes = 1 << 30; c.Outputs[0].MaxFiles = 9 }, true},
		{"memory-budget", func(c *Config) {
			c.Outputs[0].QueueBytes = 64 << 20
			x := valid
			x.Name = "other"
			x.Directory = "./other"
			c.Outputs = append(c.Outputs, x)
		}, true},
		{"too-many", func(c *Config) { c.Outputs = make([]Output, 9) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sample(t)
			c.Outputs = []Output{valid}
			tc.change(&c)
			if (c.Validate() != nil) != tc.bad {
				t.Fatal("unexpected output validation")
			}
		})
	}
	o := valid.WithDefaults()
	if o.Format != "jsonl" || o.QueueMessages != 1000 || o.QueueBytes != 8<<20 || o.MaxFileBytes != 64<<20 || o.MaxFiles != 16 {
		t.Fatal("wrong output defaults")
	}
}

func TestOutputYAMLRoundTrip(t *testing.T) {
	for _, types := range [][]string{nil, {}, {"8"}} {
		c := sample(t)
		c.Outputs = []Output{{Name: "capture", Type: "file", Directory: "./captures", ReceiveMessageTypes: types}}
		data, err := yaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Load(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		got := d.Outputs[0].ReceiveMessageTypes
		if (got == nil) != (types == nil) || strings.Join(got, ",") != strings.Join(types, ",") {
			t.Fatal("output subscription changed during serialization")
		}
	}
	c := sample(t)
	data, _ := yaml.Marshal(c)
	_, err := Load(strings.NewReader(string(data) + "\noutputs:\n  - name: capture\n    type: file\n    directory: ./captures\n    secret_typo: synthetic-value\n"))
	if err == nil || strings.Contains(err.Error(), "synthetic-value") {
		t.Fatal("unknown output field accepted or echoed")
	}
}
