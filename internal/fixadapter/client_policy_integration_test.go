//go:build integration

package fixadapter

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
)

func TestMixedClientPolicySessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	venueListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer venueListener.Close()
	muxListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer muxListener.Close()
	c := runtimeConfig(t)
	_, port, _ := net.SplitHostPort(venueListener.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(port)
	c.Downstream.Listen = muxListener.Addr().String()
	c.Downstream.Clients = []config.Client{{Sender: "TRADING"}, {Sender: "RISK", ReadOnly: true, ReceiveMessageTypes: []string{"8"}}, {Sender: "QUIET", ReadOnly: true, ReceiveMessageTypes: []string{}}}
	venue := testfix.NewPeer()
	vs, venueID, err := testfix.Settings(c.Upstream.Target, c.Upstream.Sender, venueListener.Addr().String(), c.Upstream.Dictionary, false)
	if err != nil {
		t.Fatal(err)
	}
	va, err := testfix.Listen(venue, venueListener, vs)
	if err != nil {
		t.Fatal(err)
	}
	defer va.Stop()
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return muxListener, nil })
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	wait(t, ctx, venue.Logons)
	clients := []*testfix.Peer{testfix.NewPeer(), testfix.NewPeer(), testfix.NewPeer()}
	ids := make([]quickfix.SessionID, len(clients))
	for i, name := range []string{"TRADING", "RISK", "QUIET"} {
		settings, id, err := testfix.Settings(name, c.Downstream.Target, c.Downstream.Listen, c.Downstream.Dictionary, true)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
		initiator, err := testfix.Connect(clients[i], settings)
		if err != nil {
			t.Fatal(err)
		}
		defer initiator.Stop()
		wait(t, ctx, clients[i].Logons)
	}
	waitLogged(t, ctx, r, 4)
	if err := quickfix.SendToTarget(testfix.Order("read-only"), ids[1]); err != nil {
		t.Fatal(err)
	}
	rejection := wait(t, ctx, clients[1].Messages)
	typ, _ := rejection.MsgType()
	text, _ := rejection.Body.GetString(58)
	if typ != "j" || !strings.Contains(text, "read-only") {
		t.Fatalf("unexpected rejection: %s %s", typ, text)
	}
	if err := quickfix.SendToTarget(testfix.Order("allowed"), ids[0]); err != nil {
		t.Fatal(err)
	}
	forwarded := wait(t, ctx, venue.Messages)
	id, _ := forwarded.Body.GetString(11)
	if id != "allowed" {
		t.Fatalf("unexpected upstream order %q", id)
	}
	quietID := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: c.Downstream.Target, TargetCompID: "QUIET"}
	before, err := quickfix.GetExpectedSenderNum(quietID)
	if err != nil {
		t.Fatal(err)
	}
	if err := quickfix.SendToTarget(testfix.Report("report"), venueID); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 1} {
		msg := wait(t, ctx, clients[i].Messages)
		id, _ := msg.Body.GetString(11)
		if id != "report" {
			t.Fatal("report payload changed")
		}
	}
	after, err := quickfix.GetExpectedSenderNum(quietID)
	if err != nil || after != before {
		t.Fatalf("filtered client sequence changed: %d -> %d (%v)", before, after, err)
	}
	riskID := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: c.Downstream.Target, TargetCompID: "RISK"}
	riskBefore, err := quickfix.GetExpectedSenderNum(riskID)
	if err != nil {
		t.Fatal(err)
	}
	cancelReject := quickfix.NewMessage()
	cancelReject.Header.SetString(35, "9")
	for tag, value := range map[quickfix.Tag]string{37: "mock-order", 11: "cancel", 41: "original", 39: "0", 434: "1"} {
		cancelReject.Body.SetString(tag, value)
	}
	if err := quickfix.SendToTarget(cancelReject, venueID); err != nil {
		t.Fatal(err)
	}
	msg := wait(t, ctx, clients[0].Messages)
	if typ, _ := msg.MsgType(); typ != "9" {
		t.Fatalf("unexpected message %s", typ)
	}
	riskAfter, err := quickfix.GetExpectedSenderNum(riskID)
	if err != nil || riskAfter != riskBefore {
		t.Fatalf("filtered risk sequence changed: %d -> %d (%v)", riskBefore, riskAfter, err)
	}
	if r.Router.Counters.Blocked.Load() != 1 {
		t.Fatal("blocked counter")
	}
}
