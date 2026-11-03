package retention

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetirementMarkerFailureNeverDeletesHistory(t *testing.T) {
	for _, op := range []string{"prune", "reset", "expire"} {
		t.Run(op, func(t *testing.T) {
			root := t.TempDir()
			now := time.Now()
			m, err := OpenMaintenance([]string{root}, 100000, 10000, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			dir, err := m.Register("a", root)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "history"), []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Hour)
			if err = os.Mkdir(filepath.Join(root, key("a")+".json.tmp"), 0700); err != nil {
				t.Fatal(err)
			}
			switch op {
			case "prune":
				_, err = m.Prune("a", time.Hour, false)
			case "reset":
				err = m.Reset("a")
			case "expire":
				err = m.Expire("a")
			}
			if err == nil {
				t.Fatal("marker failure hidden")
			}
			if _, err = os.Stat(filepath.Join(dir, "history")); err != nil {
				t.Fatal("history deleted before durable retirement")
			}
		})
	}
}

func TestInventoryRejectsSymlinksAndExcessBudget(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "excess"), make([]byte, 1001), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open([]string{root}, 1000, 500, time.Now); err == nil {
		t.Fatal("over-budget state accepted")
	}
	// Offline maintenance must remain possible when a full store blocks startup.
	maintenance, err := OpenMaintenance([]string{root}, 1000, 500, time.Now)
	if err != nil {
		t.Fatal("full disk budget blocked maintenance", err)
	}
	maintenance.Close()
	if _, err := os.Stat(filepath.Join(root, ".gateway-lock")); !os.IsNotExist(err) {
		t.Fatal("failed startup leaked lease")
	}
	other := t.TempDir()
	link := filepath.Join(other, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	if _, err := Open([]string{link}, 10000, 1000, time.Now); err == nil {
		t.Fatal("symlink store root accepted")
	}
	if _, err := Open([]string{other}, 10000, 1000, time.Now); err == nil {
		t.Fatal("symlink inventory accepted")
	}
}

func TestOfflineInventoryIsReadOnlyAndCorruptionVisible(t *testing.T) {
	root := t.TempDir()
	m, err := OpenMaintenance([]string{root}, 100000, 10000, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.LoadExisting("a", filepath.Join(root, "other")); err == nil {
		t.Fatal("unowned inventory")
	}
	if err = os.WriteFile(filepath.Join(root, key("a")+".json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.LoadExisting("a", root); err == nil {
		t.Fatal("corrupt inventory")
	}
	if err = os.Mkdir(filepath.Join(root, key("b")+".json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Register("b", root); err == nil {
		t.Fatal("metadata directory accepted")
	}
	blocked := filepath.Join(root, key("blocked"))
	if err = os.WriteFile(blocked, []byte("unexpected synthetic file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Register("blocked", root); err == nil {
		t.Fatal("store creation replaced an unexpected file")
	}
	if _, ok := m.State("blocked"); ok {
		t.Fatal("failed store creation registered a session")
	}
	dir, err := m.Register("c", root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Register("c", root)
	if err != nil || dir != again {
		t.Fatal("registration not idempotent")
	}
	if err = os.Remove(filepath.Join(root, ".gateway-lock")); err != nil {
		t.Fatal(err)
	}
	if m.Close() == nil {
		t.Fatal("lease tampering hidden")
	}
	if _, err = m.LoadExisting("c", root); err == nil {
		t.Fatal("closed inventory")
	}
}

func TestOversizeMetadataRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker.json")
	if err := os.WriteFile(path, make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readMetadata(path)
	if err != nil || len(data) != 4097 {
		t.Fatal("metadata read was not bounded", err)
	}
	for _, maintenance := range []bool{false, true} {
		t.Run(fmt.Sprint(maintenance), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, key("client")+".json"), make([]byte, 5000), 0600); err != nil {
				t.Fatal(err)
			}
			var m *Manager
			var err error
			if maintenance {
				m, err = OpenMaintenance([]string{root}, 100000, 10000, time.Now)
			} else {
				m, err = Open([]string{root}, 100000, 10000, time.Now)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if maintenance {
				_, err = m.LoadExisting("client", root)
			} else {
				_, err = m.Register("client", root)
			}
			if err == nil {
				t.Fatal("oversized metadata accepted")
			}
		})
	}
}

func TestLegacyStateIsNeverSilentlyReinitialized(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "FIX.4.4-MUX-CLIENT.body")
	if err := os.WriteFile(path, []byte("synthetic legacy history"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open([]string{root}, 10000, 1000, time.Now); !errors.Is(err, ErrLegacy) {
		t.Fatal("legacy state accepted for sequence reset", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("legacy history removed")
	}
}

func TestUnexpectedStoreFileIsNotDeleted(t *testing.T) {
	for _, operation := range []string{"prune", "reset"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			now := time.Now()
			m, err := OpenMaintenance([]string{root}, 10000, 1000, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			path, err := m.Register("client", root)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte("unexpected synthetic file"), 0600); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Hour)
			if operation == "prune" {
				_, err = m.Prune("client", time.Hour, false)
			} else {
				err = m.Reset("client")
			}
			if err == nil {
				t.Fatal("unexpected file accepted as a store directory")
			}
			if _, err = os.Stat(path); err != nil {
				t.Fatal("unexpected file deleted")
			}
		})
	}
}
