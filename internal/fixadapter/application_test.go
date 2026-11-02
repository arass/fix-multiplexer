package fixadapter

import (
	"testing"
	"time"

	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/sessions"
	"github.com/quickfixgo/quickfix"
)

func TestApplicationRoutingPossDupAndReplay(t *testing.T) {
	u := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "UP", TargetCompID: "VENUE"}
	client := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: "DOWN", TargetCompID: "CLIENT"}
	r := sessions.New(map[string]bool{u.String(): true, client.String(): true}, time.Now)
	sent := make(chan string, 8)
	router := gateway.New(gateway.Options{Upstream: u.String(), Clients: []string{client.String()}, UpstreamTypes: []string{"D"}, DownstreamTypes: []string{"8"}, UpstreamQueue: 2, DownstreamQueue: 2, ByteBudget: 100000}, r, gateway.SendFunc(func(m gateway.Message, id string) error { sent <- id; return nil }))
	defer router.Close()
	d := dictionary(t)
	a := &Application{Registry: r, Router: router, Upstream: u, UpstreamDictionary: d, DownstreamDictionary: d, MaxMessageBytes: 4096, Username: "synthetic-user", Password: "synthetic-password", ClientPasswords: map[string]string{client.String(): "synthetic-client"}}
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
	if rej := a.FromAdmin(logon, client); rej != nil {
		t.Fatal(rej)
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
