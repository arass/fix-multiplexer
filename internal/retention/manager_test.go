package retention

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOfflineRetirementPersistsAndReset(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	m, err := Open([]string{root}, 100000, 10000, clock)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := m.Register("client", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Register("other", filepath.Join(root, "not-owned")); err == nil {
		t.Fatal("unowned root")
	}
	if _, err := Open([]string{root}, 100000, 10000, clock); err == nil {
		t.Fatal("concurrent store owner")
	}
	if err = m.Connected("client", true); err != nil {
		t.Fatal(err)
	}
	if !m.CanUse("client") {
		t.Fatal("not usable")
	}
	if err = m.Write("client", 100, func() error { return os.WriteFile(filepath.Join(dir, "body"), []byte("synthetic"), 0600) }); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Prune("client", time.Hour, false); err == nil {
		t.Fatal("runtime manager may not prune")
	}
	if err = m.Connected("client", false); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if m.CanUse("client") {
		t.Fatal("closed manager usable")
	}
	if _, err = m.Register("bad", root); err == nil {
		t.Fatal("closed register")
	}
	maintenance, err := OpenMaintenance([]string{root}, 100000, 10000, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.Close()
	if exists, err := maintenance.LoadExisting("missing", root); err != nil || exists {
		t.Fatal("missing client")
	}
	if exists, err := maintenance.LoadExisting("client", root); err != nil || !exists {
		t.Fatal(err)
	}
	if eligible, err := maintenance.Prune("client", time.Hour, true); err != nil || eligible {
		t.Fatal("pruned young session")
	}
	now = now.Add(time.Hour)
	if eligible, err := maintenance.Prune("client", time.Hour, true); err != nil || !eligible {
		t.Fatal("dry run", err)
	}
	if _, err = os.Stat(dir); err != nil {
		t.Fatal("dry-run removed state")
	}
	s, _ := maintenance.State("client")
	if s.Retired {
		t.Fatal("dry-run retired client")
	}
	if eligible, err := maintenance.Prune("client", time.Hour, false); err != nil || !eligible {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("store remains")
	}
	if _, err = maintenance.Prune("client", time.Hour, false); err != nil {
		t.Fatal("idempotent cleanup", err)
	}
	maintenance.Close()
	m, err = Open([]string{root}, 100000, 10000, clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Register("client", root); !errors.Is(err, ErrRetired) {
		t.Fatal("retirement forgotten", err)
	}
	m.Close()
	maintenance, err = OpenMaintenance([]string{root}, 100000, 10000, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.Close()
	if _, err = maintenance.LoadExisting("client", root); err != nil {
		t.Fatal(err)
	}
	if err = maintenance.Reset("client"); err != nil {
		t.Fatal(err)
	}
	maintenance.Close()
	m, err = Open([]string{root}, 100000, 10000, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Register("client", root); err != nil {
		t.Fatal("explicit reprovision failed", err)
	}
}

func TestActiveCleanupAndExpiry(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	m, err := OpenMaintenance([]string{root}, 100000, 10000, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Register("a", root); err != nil {
		t.Fatal(err)
	}
	_ = m.Connected("a", true)
	now = now.Add(24 * time.Hour)
	if eligible, err := m.Prune("a", time.Hour, false); err != nil || eligible {
		t.Fatal("active session pruned")
	}
	if err = m.Expire("a"); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	if m.CanUse("a") {
		t.Fatal("expired session usable")
	}
	if m.Connected("a", true) == nil {
		t.Fatal("retired reconnect")
	}
	if err = m.Connected("a", false); err != nil {
		t.Fatal("retired session could not record disconnect", err)
	}
	state, _ := m.State("a")
	if state.Active || !state.Retired {
		t.Fatal("retired disconnect state lost")
	}
	if _, err = m.Prune("missing", time.Hour, false); err == nil {
		t.Fatal("unknown cleanup")
	}
	if m.Expire("missing") == nil || m.Reset("missing") == nil {
		t.Fatal("unknown expiry/reset")
	}
}

func TestBudgetAndStoreFailures(t *testing.T) {
	for _, kind := range []string{"session-budget", "global-budget", "callback", "negative", "inventory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			m, err := Open([]string{root}, 10000, 1000, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			dir, err := m.Register("a", root)
			if err != nil {
				t.Fatal(err)
			}
			usage, e := m.Usage()
			if e != nil || m.CachedUsage() < usage {
				t.Fatal("store usage metric mismatch", e)
			}
			if _, err = m.Usage(); err != nil {
				t.Fatal(err)
			}
			extra := int64(2000)
			callback := func() error { return nil }
			switch kind {
			case "global-budget":
				extra = 20000
			case "callback":
				extra = 1
				callback = func() error { return errors.New("synthetic failure") }
			case "negative":
				extra = -1
			case "inventory":
				extra = 1
				if err = os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(dir, make([]byte, 10001), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err = m.Usage(); err == nil {
					t.Fatal("external growth not detected")
				}
			}
			if m.Write("a", extra, callback) == nil || m.Healthy() {
				t.Fatal("failure not latched")
			}
			if m.Write("a", 0, func() error { return nil }) == nil {
				t.Fatal("failed store reused")
			}
		})
	}
}

func TestMetadataAndLeaseErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "not-directory"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open([]string{filepath.Join(root, "not-directory", "nested")}, 10000, 1000, time.Now); err == nil {
		t.Fatal("bad root accepted")
	}
	if _, err := Open([]string{root, root}, 10000, 1000, time.Now); err == nil {
		t.Fatal("duplicate roots")
	}
	m, err := Open([]string{root}, 10000, 1000, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = os.WriteFile(filepath.Join(root, key("bad")+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Register("bad", root); err == nil {
		t.Fatal("corrupt metadata")
	}
	if _, ok := m.State("missing"); ok {
		t.Fatal("unknown state")
	}
	if m.Connected("missing", true) == nil || m.Write("missing", 1, func() error { return nil }) == nil {
		t.Fatal("unknown write")
	}
	if _, err = m.Register("a", root); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(root, key("a")+".json.tmp")
	if err = os.Mkdir(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	if m.Connected("a", true) == nil || m.Healthy() {
		t.Fatal("metadata failure hidden")
	}
}

func TestReservationsSurviveReconciliationAndDetectExternalGrowth(t *testing.T) {
	for _, kind := range []string{"reserved-budget", "external-session-growth", "global-headroom"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			now := time.Now()
			limit := int64(10000)
			if kind == "global-headroom" {
				limit = 500
			}
			m, err := Open([]string{root}, limit, 1000, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			dir, err := m.Register("client", root)
			if kind == "global-headroom" {
				if err == nil {
					t.Fatal("metadata headroom exceeded global budget")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = m.Write("client", 600, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			reserved := m.CachedUsage()
			now = now.Add(time.Second)
			if kind == "external-session-growth" {
				if err = os.WriteFile(filepath.Join(dir, "external"), make([]byte, 1001), 0600); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			err = m.Write("client", 401, func() error { called = true; return nil })
			if err == nil || called || m.Healthy() || m.CachedUsage() < reserved {
				t.Fatal("reservation or external-growth failure was lost")
			}
		})
	}
}
