package observability

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetadataNeverLogsPayloads(t *testing.T) {
	var out bytes.Buffer
	r := New(&out)
	id := "synthetic-client-secret"
	r.Application("downstream", id, "D", 2, "queued_upstream")
	r.Session(id, true)
	r.EngineError(id)
	r.AuthenticationFailure(id)
	r.Operational("live-only")
	if strings.Contains(out.String(), id) || strings.Contains(out.String(), "553=") || r.EngineErrors.Load() != 1 || r.AuthFailures.Load() != 1 {
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

func TestEngineErrorLogsAreBoundedButMetricsCountEveryEvent(t *testing.T) {
	var out bytes.Buffer
	r := New(&out)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.EngineError("synthetic-client") }()
	}
	wg.Wait()
	if r.EngineErrors.Load() != 100 || strings.Count(out.String(), "\n") != 1 {
		t.Fatal("error flood was not bounded or counted")
	}
	now = now.Add(time.Second)
	r.EngineError("synthetic-client")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatal("next error was not logged")
	}
	var record map[string]any
	if json.Unmarshal([]byte(lines[1]), &record) != nil || record["suppressed_errors"] != float64(99) {
		t.Fatal("suppressed error count missing")
	}
	if strings.Contains(out.String(), "synthetic-client") || r.EngineErrors.Load() != 101 {
		t.Fatal("identity leaked or counter lost events")
	}
}

func TestAuthenticationFailuresShareErrorLogLimit(t *testing.T) {
	var out bytes.Buffer
	r := New(&out)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	r.AuthenticationFailure("synthetic-client")
	r.AuthenticationFailure("synthetic-client")
	now = now.Add(time.Second)
	r.EngineError("synthetic-client")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || strings.Contains(out.String(), "synthetic-client") {
		t.Fatal("unexpected log count or identity leak")
	}
	var first, second map[string]any
	if json.Unmarshal([]byte(lines[0]), &first) != nil || json.Unmarshal([]byte(lines[1]), &second) != nil || first["msg"] != "authentication_failed" || first["detail"] != "invalid credentials" || second["suppressed_errors"] != float64(1) || r.AuthFailures.Load() != 2 || r.EngineErrors.Load() != 1 {
		t.Fatal("authentication events were not safely logged and counted")
	}
}

func TestHealthAndMetrics(t *testing.T) {
	s := Snapshot{Ready: false, Forwarded: 2, Failed: 3, Dropped: 4, Recipients: 5, Replays: 6, EngineErrors: 7, AuthFailures: 9, IPDenials: 12, Cleanups: 8, QueueBytes: 9, QueueDepth: 10, StoreBytes: 11, StoreWarning: true, Sessions: []Session{{ID: "secret-id", LoggedOn: true, Enabled: true}}}
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
		if tt.path == "/metrics" && tt.method == "GET" && !strings.Contains(w.Body.String(), "fix_mux_auth_failures_total 9") {
			t.Fatal("authentication failure metric missing")
		}
		if tt.path == "/metrics" && tt.method == "GET" && !strings.Contains(w.Body.String(), "fix_mux_ip_denials_total 12") {
			t.Fatal("IP denial metric missing")
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

func TestIPDenialLogsIncludeSourceAndBoundFloods(t *testing.T) {
	var out bytes.Buffer
	r := New(&out)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	r.EngineError("unrelated-session")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.IPDenied("synthetic-sensitive-session", netip.MustParseAddr("::ffff:192.0.2.1"))
		}()
	}
	wg.Wait()
	if r.IPDenials.Load() != 100 || strings.Count(out.String(), "\n") != 2 {
		t.Fatal("denial flood was not bounded independently of engine errors")
	}
	now = now.Add(time.Second)
	r.IPDenied("synthetic-sensitive-session", netip.MustParseAddr("2001:db8::1%test"))
	now = now.Add(-time.Minute)
	r.IPDenied("synthetic-sensitive-session", netip.Addr{})
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || r.IPDenials.Load() != 102 || strings.Contains(out.String(), "synthetic-sensitive-session") || strings.Contains(out.String(), "%test") {
		t.Fatal("IP diagnostics lost events or leaked identity/zone")
	}
	for i, want := range []struct {
		source     string
		suppressed float64
	}{{"192.0.2.1", 0}, {"2001:db8::1", 99}, {"unknown", 0}} {
		var event map[string]any
		if json.Unmarshal([]byte(lines[i+1]), &event) != nil || event["msg"] != "connection_ip_denied" || event["source_ip"] != want.source || event["suppressed_denials"] != want.suppressed || event["reason"] != "source IP not in client allowed_ips" || event["session"] != Label("synthetic-sensitive-session") {
			t.Fatal("incorrect denial diagnostic")
		}
	}
}
