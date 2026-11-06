package fileoutput

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
)

func TestDirectoryStoreLeaseAndRotation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	s, err := OpenDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = OpenDirectory(dir); err == nil {
		t.Fatal("duplicate writer lease accepted")
	}
	f, err := s.Create(time.Date(2026, 1, 1, 1, 0, 0, 1, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("record\n")); err != nil {
		t.Fatal(err)
	}
	if f.Sync() != nil || f.Close() != nil {
		t.Fatal("file flush failed")
	}
	entries, err := s.List()
	if err != nil || len(entries) != 1 || entries[0].Size != 7 {
		t.Fatal(entries, err)
	}
	if s.Remove("../outside") == nil {
		t.Fatal("unsafe deletion accepted")
	}
	if err = s.Remove(entries[0].Name); err != nil {
		t.Fatal(err)
	}
	if s.Remove(entries[0].Name) == nil {
		t.Fatal("missing file reported deleted")
	}
	if s.Close() != nil || s.Close() != nil {
		t.Fatal("lease release not idempotent")
	}
	s, err = OpenDirectory(dir)
	if err != nil {
		t.Fatal("lease survived clean close", err)
	}
	_ = s.Close()
}

func TestDiskCaptureBoundsAndRestart(t *testing.T) {
	c := outputConfig()
	c.Directory = filepath.Join(t.TempDir(), "capture")
	c.MaxFileBytes = 4096
	c.MaxFiles = 2
	for run := 0; run < 2; run++ {
		s, err := New([]config.Output{c}, []string{"8"}, nil, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			s.Publish(Message{MsgType: "8", UpstreamSeqNum: i + 1, FIX: bytes.Repeat([]byte("x"), 2000)})
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		files, err := filepath.Glob(filepath.Join(c.Directory, "*.jsonl"))
		if err != nil || len(files) != 2 {
			t.Fatal(files, err)
		}
		for _, path := range files {
			info, err := os.Stat(path)
			if err != nil || info.Size() > 4096 {
				t.Fatal("file budget exceeded")
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.HasSuffix(data, []byte("\n")) {
				t.Fatal("incomplete record after clean shutdown")
			}
		}
	}
}

func TestDirectoryStoreRejectsForeignEntriesAndFailures(t *testing.T) {
	for _, kind := range []string{"foreign", "nonregular", "inventory-limit", "oversized", "root-file", "removed-root", "lease-removed"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "capture")
			if kind == "root-file" {
				if err := os.WriteFile(dir, []byte("unrelated"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := OpenDirectory(dir); err == nil {
					t.Fatal("regular file accepted as directory")
				}
				return
			}
			s, err := OpenDirectory(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			name := "capture-20260101T000000.000000000Z-123.jsonl"
			switch kind {
			case "foreign":
				_ = os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("keep"), 0600)
			case "nonregular":
				_ = os.Mkdir(filepath.Join(dir, name), 0700)
				if s.Remove(name) == nil {
					t.Fatal("directory deletion accepted")
				}
			case "inventory-limit":
				for i := 0; i < 130; i++ {
					f, err := os.CreateTemp(dir, "foreign-")
					if err != nil {
						t.Fatal(err)
					}
					f.Close()
				}
			case "oversized":
				_ = os.WriteFile(filepath.Join(dir, name), bytes.Repeat([]byte("x"), 9000), 0600)
			case "removed-root":
				if err = os.Remove(filepath.Join(dir, ".file-output.lock")); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(dir); err != nil {
					t.Fatal(err)
				}
			case "lease-removed":
				if err = os.Remove(filepath.Join(dir, ".file-output.lock")); err != nil {
					t.Fatal(err)
				}
				if s.Close() == nil {
					t.Fatal("missing lease release hidden")
				}
				return
			}
			if kind == "oversized" {
				o := &output{config: outputConfig(), store: s}
				if o.trim(2) == nil {
					t.Fatal("existing oversized capture accepted")
				}
			} else {
				if _, err = s.List(); err == nil {
					t.Fatal("unsafe directory accepted")
				}
			}
			if kind == "foreign" {
				data, err := os.ReadFile(filepath.Join(dir, "unrelated.txt"))
				if err != nil || string(data) != "keep" {
					t.Fatal("foreign file modified")
				}
			}
		})
	}
}

func TestDirectoryStoreRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	if _, err := OpenDirectory(link); err == nil {
		t.Fatal("symlink output root accepted")
	}
	s, err := OpenDirectory(target)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	outside := filepath.Join(root, "outside")
	if err = os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	name := "capture-20260101T000000.000000000Z-123.jsonl"
	if err = os.Symlink(outside, filepath.Join(target, name)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.List(); err == nil {
		t.Fatal("capture symlink accepted")
	}
	if s.Remove(name) == nil {
		t.Fatal("capture symlink removed")
	}
	data, _ := os.ReadFile(outside)
	if strings.TrimSpace(string(data)) != "keep" {
		t.Fatal("external file changed")
	}
}
