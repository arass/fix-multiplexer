package observability

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetadataNeverLogsPayloads(t *testing.T) {
	var out bytes.Buffer
	r := New(&out)
	id := "synthetic-client-secret"
	r.Application("downstream", id, "D", 2, "queued_upstream")
	r.Session(id, true)
	r.EngineError(id)
	r.Operational("live-only")
	if strings.Contains(out.String(), id) || strings.Contains(out.String(), "553=") || r.EngineErrors.Load() != 1 {
		t.Fatal("sensitive metadata leaked")
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["time"] == nil {
			t.Fatal("timestamp missing")
		}
	}
}

func TestHealthAndMetrics(t *testing.T) {
	s := Snapshot{Ready: false, Forwarded: 2, Failed: 3, Dropped: 4, Recipients: 5, Replays: 6, EngineErrors: 7, Cleanups: 8, QueueBytes: 9, QueueDepth: 10, StoreBytes: 11, StoreWarning: true, Sessions: []Session{{ID: "secret-id", LoggedOn: true, Enabled: true}}}
	h := Handler(ProviderFunc(func() Snapshot { return s }))
	for _, tt := range []struct {
		path, method string
		status       int
		contains     string
	}{{"/live", "GET", 200, "live"}, {"/ready", "GET", 503, "not ready"}, {"/metrics", "GET", 200, "fix_mux_forwarded_total 2"}, {"/live", "POST", 405, ""}, {"/ready", "POST", 405, ""}, {"/metrics", "POST", 405, ""}, {"/missing", "GET", 404, ""}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.contains) {
			t.Fatal(tt, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret-id") {
			t.Fatal("identity label leak")
		}
	}
	s.Ready = true
	s.StoreWarning = false
	s.Sessions[0].Enabled = false
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != 200 {
		t.Fatal("not ready")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), "fix_mux_ready 1") || !strings.Contains(w.Body.String(), "fix_mux_store_warning 0") {
		t.Fatal(w.Body.String())
	}
}
