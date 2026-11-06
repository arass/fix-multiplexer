package fileoutput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
)

type memoryFile struct {
	bytes.Buffer
	writeErr, syncErr, closeErr error
	short                       bool
	entered, release            chan struct{}
	once                        sync.Once
}

func (f *memoryFile) Write(p []byte) (int, error) {
	if f.entered != nil {
		f.once.Do(func() { close(f.entered) })
		<-f.release
	}
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.short {
		return len(p) - 1, nil
	}
	return f.Buffer.Write(p)
}
func (f *memoryFile) Sync() error  { return f.syncErr }
func (f *memoryFile) Close() error { return f.closeErr }

type memoryStore struct {
	files                                   []*memoryFile
	entries                                 []Entry
	removed                                 []string
	listErr, createErr, removeErr, closeErr error
	next                                    *memoryFile
	closed                                  bool
}

func (s *memoryStore) List() ([]Entry, error) { return append([]Entry(nil), s.entries...), s.listErr }
func (s *memoryStore) Create(time.Time) (File, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	f := s.next
	if f == nil {
		f = &memoryFile{}
	}
	s.next = nil
	s.files = append(s.files, f)
	return f, nil
}
func (s *memoryStore) Remove(name string) error {
	s.removed = append(s.removed, name)
	if s.removeErr != nil {
		return s.removeErr
	}
	for i, e := range s.entries {
		if e.Name == name {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			break
		}
	}
	return nil
}
func (s *memoryStore) Close() error { s.closed = true; return s.closeErr }
func outputConfig() config.Output {
	return config.Output{Name: "capture", Type: "file", Directory: "capture", QueueMessages: 32, QueueBytes: 1 << 20, MaxFileBytes: 8192, MaxFiles: 2}
}
func openMemory(t *testing.T, c config.Output, store *memoryStore, failure func(string, string)) *Set {
	t.Helper()
	s, err := New([]config.Output{c}, []string{"8", "9"}, nil, Options{Now: func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }, OpenStore: func(string) (Store, error) { return store, nil }, OnFailure: failure})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func decodeRecords(t *testing.T, files []*memoryFile) []Record {
	t.Helper()
	var out []Record
	for _, f := range files {
		d := json.NewDecoder(bytes.NewReader(f.Bytes()))
		for {
			var r Record
			err := d.Decode(&r)
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

func TestCaptureFidelityFilteringAndShutdown(t *testing.T) {
	stores := map[string]*memoryStore{"inherit": {}, "selected": {}, "none": {}}
	var configs []config.Output
	for _, name := range []string{"inherit", "selected", "none"} {
		c := outputConfig()
		c.Name = name
		c.Directory = name
		if name == "selected" {
			c.ReceiveMessageTypes = []string{"8"}
		}
		if name == "none" {
			c.ReceiveMessageTypes = []string{}
		}
		configs = append(configs, c)
	}
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	s, err := New(configs, []string{"8", "9", "j", "A"}, []string{"j"}, Options{Now: func() time.Time { return at }, OpenStore: func(name string) (Store, error) { return stores[name], nil }})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{'8', '=', 0xff, 1, '4', '3', '=', 'Y', 1, 0, 10}
	for i, dup := range []string{"", "N", "Y"} {
		s.Publish(Message{MsgType: "8", UpstreamSeqNum: i + 1, PossDupFlag: dup, OrigSendingTime: "20260101-10:00:00.000", FIX: raw})
	}
	s.Publish(Message{MsgType: "9", FIX: []byte("cancel")})
	s.Publish(Message{MsgType: "j", FIX: []byte("denied")})
	s.Publish(Message{MsgType: "A", FIX: []byte("password")})
	s.Publish(Message{MsgType: "UNKNOWN"})
	raw[0] = 'X'
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inherit", "selected", "none"} {
		records := decodeRecords(t, stores[name].files)
		want := 3
		if name == "inherit" {
			want = 4
		}
		if name == "none" {
			want = 0
		}
		if len(records) != want {
			t.Fatal(name, len(records))
		}
		for i := 0; i < min(3, len(records)); i++ {
			r := records[i]
			if r.ReceivedAt != at.UTC() || r.UpstreamSeqNum != i+1 || r.PossDupFlag != []string{"", "N", "Y"}[i] || r.OrigSendingTime != "20260101-10:00:00.000" || r.FIX[0] != '8' || r.FIX[2] != 0xff {
				t.Fatal("capture metadata or binary snapshot changed")
			}
		}
	}
	for _, state := range s.Snapshots() {
		if !state.Healthy || state.Dropped != 0 || state.QueueBytes != 0 || state.QueueDepth != 0 {
			t.Fatal(state)
		}
	}
	s.Publish(Message{MsgType: "8"})
	if s.Snapshots()[0].Dropped != 1 {
		t.Fatal("post-close capture not counted")
	}
	var none *Set
	none.Publish(Message{})
	if !none.Healthy() || none.Close() != nil {
		t.Fatal("nil outputs changed default behavior")
	}
}

func TestOutputQueueAndRecordLimits(t *testing.T) {
	for _, tc := range []struct {
		name       string
		queue      int
		budget     int64
		size       int
		wantReason string
	}{
		{"messages", 1, 1 << 20, 10, "queue_full"}, {"bytes", 8, 4096, 2000, "queue_full"}, {"record", 8, 1 << 20, 10000, "record_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := outputConfig()
			c.QueueMessages = tc.queue
			c.QueueBytes = tc.budget
			f := &memoryFile{entered: make(chan struct{}), release: make(chan struct{})}
			release := sync.OnceFunc(func() { close(f.release) })
			defer release()
			store := &memoryStore{next: f}
			failures := make(chan string, 4)
			s := openMemory(t, c, store, func(_, reason string) { failures <- reason })
			m := Message{MsgType: "8", FIX: bytes.Repeat([]byte("x"), tc.size)}
			s.Publish(m)
			if tc.name != "record" {
				select {
				case <-f.entered:
				case <-time.After(2 * time.Second):
					t.Fatal("writer did not start")
				}
				s.Publish(m)
				if tc.name == "messages" {
					s.Publish(m)
				}
			}
			select {
			case reason := <-failures:
				if reason != tc.wantReason {
					t.Fatal(reason)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no failure event")
			}
			if s.Healthy() {
				t.Fatal("capture gap did not latch unhealthy")
			}
			s.Publish(m)
			release()
			if s.Close() == nil {
				t.Fatal("capture failure not reported at shutdown")
			}
			state := s.Snapshots()[0]
			if state.QueueBytes != 0 || state.QueueDepth != 0 || state.Dropped < 2 {
				t.Fatal(state)
			}
			select {
			case <-failures:
				t.Fatal("failed output flooded diagnostics")
			default:
			}
		})
	}
}

func TestOutputStorageFailures(t *testing.T) {
	fault := errors.New("synthetic-sensitive-write-error")
	for _, kind := range []string{"create", "write", "short", "sync", "close", "lease-close", "list-on-rotate", "remove-on-rotate"} {
		t.Run(kind, func(t *testing.T) {
			f := &memoryFile{}
			store := &memoryStore{next: f}
			c := outputConfig()
			switch kind {
			case "create":
				store.createErr = fault
			case "write":
				f.writeErr = fault
			case "short":
				f.short = true
			case "sync":
				f.syncErr = fault
			case "close":
				f.closeErr = fault
			case "lease-close":
				store.closeErr = fault
			}
			failures := make(chan string, 2)
			s := openMemory(t, c, store, func(_, reason string) { failures <- reason })
			if kind == "list-on-rotate" {
				store.listErr = fault
			}
			if kind == "remove-on-rotate" {
				store.entries = []Entry{{Name: "old1"}, {Name: "old2"}}
				store.removeErr = fault
			}
			s.Publish(Message{MsgType: "8", FIX: []byte("message")})
			if s.Close() == nil {
				t.Fatal("storage failure hidden")
			}
			if s.Healthy() || !store.closed {
				t.Fatal("failed output remained healthy or leased")
			}
			reason := <-failures
			if reason == fault.Error() {
				t.Fatal("raw storage error exposed")
			}
		})
	}
}

func TestOutputRotationAndRetention(t *testing.T) {
	store := &memoryStore{entries: []Entry{{Name: "old1"}, {Name: "old2"}, {Name: "old3"}}}
	c := outputConfig()
	c.MaxFiles = 2
	s := openMemory(t, c, store, nil)
	// Directly exercise the single writer with known record sizes and UTC days.
	o := s.outputs[0]
	a := time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC)
	for _, q := range []queued{{at: a, line: bytes.Repeat([]byte("a"), 5000)}, {at: a, line: bytes.Repeat([]byte("b"), 5000)}, {at: a.Add(2 * time.Minute), line: []byte("day-two")}} {
		if err := o.write(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if len(store.files) != 3 || len(store.removed) != 2 || store.removed[0] != "old1" || store.removed[1] != "old2" {
		t.Fatal("rotation/retention policy incorrect", store.removed)
	}
}

func TestOutputConstructorCleanup(t *testing.T) {
	fault := errors.New("synthetic-fault")
	for _, kind := range []string{"open", "list", "oversized-existing", "remove", "invalid-limits"} {
		t.Run(kind, func(t *testing.T) {
			store := &memoryStore{}
			c := outputConfig()
			if kind == "list" {
				store.listErr = fault
			}
			if kind == "oversized-existing" {
				store.entries = []Entry{{Size: 9000}}
			}
			if kind == "remove" {
				store.entries = []Entry{{Name: "a"}, {Name: "b"}, {Name: "c"}}
				store.removeErr = fault
			}
			if kind == "invalid-limits" {
				c.QueueMessages = -1
			}
			_, err := New([]config.Output{c}, []string{"8"}, nil, Options{OpenStore: func(string) (Store, error) {
				if kind == "open" {
					return nil, fault
				}
				return store, nil
			}})
			if err == nil {
				t.Fatal("bad output accepted")
			}
			if kind != "open" && kind != "invalid-limits" && !store.closed {
				t.Fatal("constructor leaked lease")
			}
		})
	}
	first := &memoryStore{}
	calls := 0
	c := outputConfig()
	other := c
	other.Name = "other"
	other.Directory = "other"
	_, err := New([]config.Output{c, other}, []string{"8"}, nil, Options{OpenStore: func(string) (Store, error) {
		calls++
		if calls == 2 {
			return nil, fault
		}
		return first, nil
	}})
	if err == nil || !first.closed {
		t.Fatal("partial construction leaked first output")
	}
}

func TestOutputEncodingFailure(t *testing.T) {
	s, err := New([]config.Output{outputConfig()}, []string{"8"}, nil, Options{Now: func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, OpenStore: func(string) (Store, error) { return &memoryStore{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	s.Publish(Message{MsgType: "8"})
	if s.Healthy() || s.Close() == nil || s.Snapshots()[0].Dropped != 1 {
		t.Fatal("invalid capture time emitted a corrupt record")
	}
}

func TestOutputConcurrentPublishAndClose(t *testing.T) {
	s := openMemory(t, outputConfig(), &memoryStore{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Publish(Message{MsgType: "8", FIX: []byte(fmt.Sprint(i))})
			_ = s.Close()
			_ = s.Snapshots()
		}(i)
	}
	wg.Wait()
}
