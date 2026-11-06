package fixadapter

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/fileoutput"
	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/sessions"
	"github.com/quickfixgo/quickfix"
)

type capturePublisher struct{ messages []fileoutput.Message }

type filePublishFunc func(fileoutput.Message)

func (f filePublishFunc) Publish(m fileoutput.Message) { f(m) }

func (p *capturePublisher) Publish(m fileoutput.Message) {
	m.FIX = append([]byte(nil), m.FIX...)
	p.messages = append(p.messages, m)
}

func TestFileCapturePreservesUpstreamReplayMetadataOnly(t *testing.T) {
	u := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "UP", TargetCompID: "VENUE"}
	client := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "DOWN", TargetCompID: "CLIENT"}
	registry := sessions.New(map[string]bool{u.String(): true, client.String(): true}, time.Now)
	router := gateway.New(gateway.Options{Upstream: u.String(), Clients: []string{client.String()}, UpstreamTypes: []string{"D"}, DownstreamTypes: []string{"8"}, UpstreamQueue: 8, DownstreamQueue: 8, ByteBudget: 1 << 20}, registry, gateway.SendFunc(func(gateway.Message, string) error { return nil }))
	defer router.Close()
	publisher := &capturePublisher{}
	a := &Application{Registry: registry, Router: router, Upstream: u, UpstreamDictionary: dictionary(t), DownstreamDictionary: dictionary(t), MaxMessageBytes: 4096, FileOutputs: publisher}
	for _, dup := range []string{"", "N", "Y"} {
		m := framed("8")
		if dup != "" {
			m.Header.SetString(43, dup)
		}
		m.Header.SetString(122, "20260101-10:00:00.000")
		m.Header.SetInt(34, 42)
		if a.FromApp(m, u) != nil {
			t.Fatal("upstream application rejected")
		}
		r := publisher.messages[len(publisher.messages)-1]
		if r.PossDupFlag != dup || r.OrigSendingTime != "20260101-10:00:00.000" || r.UpstreamSeqNum != 42 || !bytes.Equal(r.FIX, m.Bytes()) {
			t.Fatal("upstream replay metadata lost")
		}
		cloned, err := Snapshot(m, dictionary(t))
		if err != nil {
			t.Fatal(err)
		}
		network, err := cloned.Clone()
		if err != nil {
			t.Fatal(err)
		}
		out := network.(*Message).out
		if out.Header.Has(43) || out.Header.Has(122) || out.Header.Has(34) {
			t.Fatal("upstream session flags leaked into rebuilt network framing")
		}
		if err = a.ToApp(m, u); err != nil {
			t.Fatal(err)
		}
		_ = a.FromAdmin(framed("0"), u)
		_ = a.FromApp(framed("D"), client)
	}
	if len(publisher.messages) != 3 {
		t.Fatal("admin, downstream input or outbound replay captured")
	}
}

func readCaptureRecords(t *testing.T, dir string) []fileoutput.Record {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []fileoutput.Record
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var r fileoutput.Record
			err = decoder.Decode(&r)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
	}
	return out
}

func TestRuntimeFileOutputLifecycleAndFailure(t *testing.T) {
	c := runtimeConfig(t)
	c.Outputs = []config.Output{{Name: "capture", Type: "file", Directory: filepath.Join(t.TempDir(), "capture"), MaxFileBytes: 4096}}
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshot().FileOutputs) != 1 {
		t.Fatal("output metrics missing")
	}
	_ = r.Registry.SetLoggedOn(r.Application.Upstream.String(), true)
	if !r.Snapshot().Ready {
		t.Fatal("healthy output affected readiness")
	}
	r.FileOutputs.Publish(fileoutput.Message{MsgType: "8", FIX: bytes.Repeat([]byte("x"), 5000)})
	if r.Snapshot().Ready || r.Snapshot().FileOutputs[0].Dropped != 1 {
		t.Fatal("capture failure not visible")
	}
	if r.Stop() == nil {
		t.Fatal("output error hidden at shutdown")
	}
	// Capture failure does not retire or prune the independent FIX session store.
	r, err = New(c)
	if err != nil {
		t.Fatal("cleanly released output lease prevented restart", err)
	}
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsOutputFilesystemProblems(t *testing.T) {
	for _, kind := range []string{"file-root", "foreign-entry", "stale-lease"} {
		t.Run(kind, func(t *testing.T) {
			c := runtimeConfig(t)
			dir := filepath.Join(t.TempDir(), "capture")
			c.Outputs = []config.Output{{Name: "capture", Type: "file", Directory: dir}}
			if kind == "file-root" {
				if err := os.WriteFile(dir, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "foreign-entry" {
					_ = os.WriteFile(filepath.Join(dir, "foreign"), []byte("keep"), 0600)
				} else {
					_ = os.Mkdir(filepath.Join(dir, ".file-output.lock"), 0700)
				}
			}
			if _, err := New(c); err == nil {
				t.Fatal("invalid output filesystem accepted")
			}
			// Construction failure releases the FIX roots, independent of output state.
			c.Outputs = nil
			r, err := New(c)
			if err != nil {
				t.Fatal("FIX lease leaked", err)
			}
			_ = r.Stop()
		})
	}
}

func TestRuntimeRejectsResolvedOutputStoreOverlap(t *testing.T) {
	c := runtimeConfig(t)
	if err := os.MkdirAll(c.Upstream.StoreDir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(c.Upstream.StoreDir, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	c.Outputs = []config.Output{{Name: "capture", Type: "file", Directory: filepath.Join(link, "exports")}}
	if _, err := New(c); err == nil {
		t.Fatal("symlink ancestor hid output/store overlap")
	}
}
