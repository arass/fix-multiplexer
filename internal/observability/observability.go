// Package observability exposes metadata only. FIX payloads and engine error
// strings are never passed to the logger, even when malformed or rejected.
package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

type Recorder struct {
	log                                *slog.Logger
	Replay, EngineErrors, AuthFailures atomic.Uint64
	IPDenials                          atomic.Uint64
	Errors                             chan struct{}
	errorMu                            sync.Mutex
	lastErrorLog                       time.Time
	suppressedErrors                   uint64
	now                                func() time.Time
	ipMu                               sync.Mutex
	lastIPLog                          time.Time
	suppressedIPDenials                uint64
}

func New(w io.Writer) *Recorder {
	return &Recorder{log: slog.New(slog.NewJSONHandler(w, nil)), Errors: make(chan struct{}, 1), now: time.Now}
}
func Label(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "s_" + hex.EncodeToString(sum[:8])
}
func (r *Recorder) Application(direction, id, typ string, sequence int, outcome string) {
	r.log.Info("application", "direction", direction, "session", Label(id), "msg_type", typ, "sequence", sequence, "outcome", outcome)
}
func (r *Recorder) Session(id string, loggedOn bool) {
	r.log.Info("session", "session", Label(id), "logged_on", loggedOn)
}
func (r *Recorder) EngineError(id string) {
	r.EngineErrors.Add(1)
	select {
	case r.Errors <- struct{}{}:
	default:
	}
	r.errorEvent("engine_error", id, "suppressed to protect FIX content")
}
func (r *Recorder) AuthenticationFailure(id string) {
	r.AuthFailures.Add(1)
	r.errorEvent("authentication_failed", id, "invalid credentials")
}

// IPDenied logs only an OS-observed IP and hashed session identity. Its separate
// global limit keeps engine errors from hiding the first IP-policy diagnostic.
func (r *Recorder) IPDenied(id string, source netip.Addr) {
	r.IPDenials.Add(1)
	r.ipMu.Lock()
	defer r.ipMu.Unlock()
	now := r.now()
	if !r.lastIPLog.IsZero() && !now.Before(r.lastIPLog) && now.Sub(r.lastIPLog) < time.Second {
		r.suppressedIPDenials++
		return
	}
	ip := "unknown"
	if source.IsValid() {
		ip = source.Unmap().WithZone("").String()
	}
	r.log.Warn("connection_ip_denied", "session", Label(id), "source_ip", ip,
		"reason", "source IP not in client allowed_ips", "suppressed_denials", r.suppressedIPDenials)
	r.suppressedIPDenials = 0
	r.lastIPLog = now
}
func (r *Recorder) errorEvent(event, id, detail string) {
	// Bound unauthenticated error-log floods while keeping every event in its
	// metric. Summarize suppressed events on the next emitted error.
	r.errorMu.Lock()
	defer r.errorMu.Unlock()
	now := r.now()
	if !r.lastErrorLog.IsZero() && !now.Before(r.lastErrorLog) && now.Sub(r.lastErrorLog) < time.Second {
		r.suppressedErrors++
		return
	}
	suppressed := r.suppressedErrors
	r.suppressedErrors = 0
	r.lastErrorLog = now
	r.log.Error(event, "session", Label(id), "detail", detail, "suppressed_errors", suppressed)
}
func (r *Recorder) Operational(outcome string) { r.log.Info("gateway", "outcome", outcome) }

type Session struct {
	ID                string
	LoggedOn, Enabled bool
}
type Snapshot struct {
	Blocked                                                                               uint64
	Ready                                                                                 bool
	Forwarded, Failed, Dropped, Recipients, Replays, EngineErrors, AuthFailures, Cleanups uint64
	IPDenials                                                                             uint64
	QueueBytes, QueueDepth, StoreBytes                                                    int64
	EngineQueueBytes                                                                      int64
	StoreWarning                                                                          bool
	Sessions                                                                              []Session
}
type Provider interface{ Snapshot() Snapshot }
type ProviderFunc func() Snapshot

func (f ProviderFunc) Snapshot() Snapshot { return f() }

func Handler(p Provider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "live")
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if !p.Snapshot().Ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "not ready")
			return
		}
		fmt.Fprintln(w, "ready")
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s := p.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.Header().Set("Cache-Control", "no-store")
		for _, v := range []struct {
			name  string
			value uint64
		}{{"forwarded_total", s.Forwarded}, {"failed_total", s.Failed}, {"blocked_total", s.Blocked}, {"dropped_total", s.Dropped}, {"fanout_recipients_total", s.Recipients}, {"replays_total", s.Replays}, {"engine_errors_total", s.EngineErrors}, {"auth_failures_total", s.AuthFailures}, {"ip_denials_total", s.IPDenials}, {"cleanup_total", s.Cleanups}} {
			fmt.Fprintf(w, "fix_mux_%s %d\n", v.name, v.value)
		}
		fmt.Fprintf(w, "fix_mux_queue_bytes %d\nfix_mux_queue_depth %d\nfix_mux_store_bytes %d\nfix_mux_ready %d\nfix_mux_store_warning %d\n", s.QueueBytes, s.QueueDepth, s.StoreBytes, boolean(s.Ready), boolean(s.StoreWarning))
		fmt.Fprintf(w, "fix_mux_engine_queue_bytes %d\n", s.EngineQueueBytes)
		for _, session := range s.Sessions {
			fmt.Fprintf(w, "fix_mux_session_logged_on{session=%q} %d\nfix_mux_session_enabled{session=%q} %d\n", Label(session.ID), boolean(session.LoggedOn), Label(session.ID), boolean(session.Enabled))
		}
	})
	return mux
}
func boolean(v bool) int {
	if v {
		return 1
	}
	return 0
}
