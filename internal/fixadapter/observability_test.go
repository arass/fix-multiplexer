package fixadapter

import (
	"bytes"
	"strings"
	"testing"

	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
)

func TestEngineLogsSuppressInterpolatedSecrets(t *testing.T) {
	var out bytes.Buffer
	recorder := observability.New(&out)
	factory := logFactory{recorder: recorder}
	global, err := factory.Create()
	if err != nil {
		t.Fatal(err)
	}
	session, err := factory.CreateSessionLog(quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "SYNTHETIC", TargetCompID: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	global.OnIncoming([]byte("553=synthetic-secret"))
	global.OnOutgoing([]byte("554=synthetic-secret"))
	global.OnEventf("Invalid Message: %s", "553=synthetic-secret")
	session.OnEvent("Msg Parse Error: synthetic-secret")
	session.OnEventf("Connecting: %s", "synthetic-secret")
	if strings.Contains(out.String(), "synthetic-secret") || recorder.EngineErrors.Load() != 2 {
		t.Fatal("raw engine content leaked")
	}
}

func TestReadinessIncludesStoreHealth(t *testing.T) {
	c := runtimeConfig(t)
	var out bytes.Buffer
	r, err := New(c, observability.New(&out))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if r.Snapshot().Ready {
		t.Fatal("ready without upstream")
	}
	if err = r.Registry.SetLoggedOn(r.Application.Upstream.String(), true); err != nil {
		t.Fatal(err)
	}
	if !r.Snapshot().Ready {
		t.Fatal("offline downstream made gateway unready")
	}
	r.Observer.Replay.Add(1)
	r.Observer.EngineError("synthetic")
	s := r.Snapshot()
	if s.Replays != 1 || s.EngineErrors != 1 || len(s.Sessions) != 2 {
		t.Fatal("metrics mismatch")
	}
	r.Stores.Fail()
	if r.Snapshot().Ready {
		t.Fatal("unhealthy durability still ready")
	}
}

func TestQueueCreditsUseActualEngineFrames(t *testing.T) {
	c := runtimeConfig(t)
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	m := framed("D")
	m.Body.SetString(11, "synthetic")
	if err = r.Application.ToApp(m, r.Application.Upstream); err != nil {
		t.Fatal(err)
	}
	if r.Pending.Usage() == 0 {
		t.Fatal("send queue not bounded")
	}
	l, err := (logFactory{recorder: r.Observer, pending: r.Pending}).CreateSessionLog(r.Application.Upstream)
	if err != nil {
		t.Fatal(err)
	}
	l.OnOutgoing(m.Bytes())
	if r.Pending.Usage() != 0 {
		t.Fatal("frame credit not released")
	}
}

func TestUnhealthyDurabilityDeniesFurtherBusinessAndLogon(t *testing.T) {
	c := runtimeConfig(t)
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	r.Stores.Fail()
	a := r.Application
	if a.FromApp(framed("D"), a.Upstream) == nil || a.FromAdmin(framed("A"), a.Upstream) == nil {
		t.Fatal("unhealthy store accepted traffic")
	}
	a.OnLogon(a.Upstream)
	state, _ := r.Registry.Get(a.Upstream.String())
	if state.Enabled || state.LoggedOn {
		t.Fatal("failed store admitted logon")
	}
	a.OnLogout(a.Upstream)
	a.Pending = transport.NewPending(1, 1)
	m := framed("8")
	if a.ToApp(m, a.Upstream) == nil {
		t.Fatal("engine queue limit ignored")
	}
	m.Header.SetBool(43, true)
	if a.ToApp(m, a.Upstream) != nil {
		t.Fatal("PossDup replay rejected for queue credit")
	}
}
