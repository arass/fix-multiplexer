package fixadapter

import (
	"io"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/sessions"
	"github.com/quickfixgo/quickfix"
)

func TestUpstreamRejectNoticeFieldsAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		populate func(*quickfix.Message)
		contains string
	}{
		{"minimal", func(m *quickfix.Message) { m.Body.SetInt(45, 42) }, "upstream RefSeqNum=42"},
		{"long text", func(m *quickfix.Message) { m.Body.SetString(58, strings.Repeat("x", 600)+"secret-after-limit") }, "exchange Text=" + strings.Repeat("x", 512)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := framed("3")
			tc.populate(m)
			notice := newUpstreamRejectNotice(m)
			cloned, err := notice.Clone()
			if err != nil {
				t.Fatal(err)
			}
			out := cloned.(*Message).out
			text, _ := out.Body.GetString(58)
			if notice.Type() != "j" || notice.Size() < len(text) || !strings.Contains(text, tc.contains) || strings.Contains(text, "secret-after-limit") || out.Body.Has(45) {
				t.Fatal("incorrect or unbounded notification")
			}
		})
	}
}

func TestApplicationRoutingPossDupAndReplay(t *testing.T) {
	u := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "UP", TargetCompID: "VENUE"}
	client := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "DOWN", TargetCompID: "CLIENT"}
	r := sessions.New(map[string]bool{u.String(): true, client.String(): true}, time.Now)
	sent := make(chan string, 8)
	router := gateway.New(gateway.Options{Upstream: u.String(), Clients: []string{client.String()}, UpstreamTypes: []string{"D"}, DownstreamTypes: []string{"8"}, UpstreamQueue: 2, DownstreamQueue: 2, ByteBudget: 100000}, r, gateway.SendFunc(func(m gateway.Message, id string) error { sent <- id; return nil }))
	defer router.Close()
	d := dictionary(t)
	a := &Application{Registry: r, Router: router, Upstream: u, UpstreamDictionary: d, DownstreamDictionary: d, MaxMessageBytes: 4096, Username: "synthetic-user", Password: "synthetic-password", ClientPasswords: map[string]string{client.String(): "synthetic-client"}}
	a.Observer = observability.New(io.Discard)
	a.OnCreate(client)
	a.OnLogon(client)
	a.OnLogon(u)
	m := framed("D")
	m.Body.SetString(11, "unchanged")
	m.Header.SetBool(43, true)
	m.Header.SetString(122, "20260101-11:00:00.000")
	if rej := a.FromApp(m, client); rej != nil {
		t.Fatal(rej)
	}
	if id := <-sent; id != u.String() {
		t.Fatal(id)
	}
	if err := a.ToApp(m, u); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
		t.Fatal("outbound replay routed")
	default:
	}
	if rej := a.FromApp(framed("8"), u); rej != nil {
		t.Fatal(rej)
	}
	select {
	case id := <-sent:
		if id != client.String() {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("fanout missing")
	}
	adminReject := framed("3")
	adminReject.Body.SetInt(45, 123)
	if a.FromAdmin(adminReject, client) != nil {
		t.Fatal("downstream reject failed")
	}
	select {
	case <-sent:
		t.Fatal("downstream session reject was broadcast")
	default:
	}
	if a.FromAdmin(adminReject, u) != nil {
		t.Fatal("upstream reject failed")
	}
	select {
	case id := <-sent:
		if id != client.String() {
			t.Fatal("reject notice sent to wrong session")
		}
	case <-time.After(time.Second):
		t.Fatal("upstream reject notice missing")
	}
	a.OnLogout(u)
	if rej := a.FromApp(m, client); rej == nil || !rej.IsBusinessReject() || rej.BusinessRejectRefID() != "unchanged" {
		t.Fatal("outage reject", rej)
	}
	if rej := a.FromApp(framed("ZZ"), client); rej == nil {
		t.Fatal("unsupported accepted")
	}
	a.MaxMessageBytes = 1
	if a.FromApp(m, client) == nil || a.FromAdmin(m, client) == nil {
		t.Fatal("oversize accepted")
	}
	a.MaxMessageBytes = 4096
	logon := framed("A")
	a.ToAdmin(logon, u)
	password, _ := logon.Body.GetString(554)
	if password != "synthetic-password" {
		t.Fatal("credential injection")
	}
	if a.FromAdmin(logon, u) != nil {
		t.Fatal("upstream admin")
	}
	if a.FromAdmin(framed("0"), client) != nil {
		t.Fatal("heartbeat")
	}
	if a.FromAdmin(framed("A"), client) == nil {
		t.Fatal("missing credential accepted")
	}
	logon.Body.SetString(554, "wrong")
	if a.FromAdmin(logon, client) == nil {
		t.Fatal("bad password")
	}
	logon.Body.SetString(554, "synthetic-client")
	if a.Observer.AuthFailures.Load() != 2 {
		t.Fatal("failed logons not counted")
	}
	if rej := a.FromAdmin(logon, client); rej != nil {
		t.Fatal(rej)
	}
	if a.Observer.AuthFailures.Load() != 2 {
		t.Fatal("successful logon counted as failure")
	}
	logon.Body.SetBool(141, true)
	if a.FromAdmin(logon, client) == nil {
		t.Fatal("uncoordinated reset")
	}
	logon.Body.SetBool(141, false)
	_ = r.Disable(client.String())
	if a.FromAdmin(logon, client) == nil {
		t.Fatal("disabled logon")
	}
	if a.FromAdmin(logon, quickfix.SessionID{}) == nil {
		t.Fatal("unknown logon")
	}
}
