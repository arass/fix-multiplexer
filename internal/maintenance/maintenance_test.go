package maintenance

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/retention"
	"github.com/quickfixgo/quickfix"
)

func TestOfflineMaintenance(t *testing.T) {
	root := t.TempDir()
	c := config.Defaults()
	c.Upstream.StoreDir = filepath.Join(root, "up")
	c.Downstream.StoreDir = filepath.Join(root, "down")
	c.Downstream.Target = "MUX"
	c.Downstream.Clients = []config.Client{{Sender: "A"}, {Sender: "B"}}
	c.Retention.IdleTTL = time.Hour
	var out bytes.Buffer
	if err := Run(c, "list", "", true, &out); err != nil || !strings.Contains(out.String(), "no stored session") {
		t.Fatal(err, out.String())
	}
	m, err := retention.Open([]string{c.Upstream.StoreDir, c.Downstream.StoreDir}, c.Retention.DiskLimitBytes, c.Retention.SessionLimitBytes, func() time.Time { return time.Now().Add(-2 * time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	id := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "MUX", TargetCompID: "A"}.String()
	dir, err := m.Register(id, c.Downstream.StoreDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "history"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if Run(c, "prune", "", false, &out) == nil {
		t.Fatal("online maintenance accepted")
	}
	m.Close()
	for _, command := range []string{"list", "prune", "reset"} {
		if err = Run(c, command, "A", true, &out); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(dir); err != nil {
		t.Fatal("dry-run removed files")
	}
	if err = Run(c, "prune", "", false, &out); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("stale history retained")
	}
	if err = Run(c, "reset", "A", false, &out); err != nil {
		t.Fatal(err)
	}
	if Run(c, "reset", "UNKNOWN", false, &out) == nil || Run(c, "unknown", "", true, &out) == nil {
		t.Fatal("invalid operation accepted")
	}
}
