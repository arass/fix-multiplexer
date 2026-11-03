//go:build integration

package fixadapter

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
)

func TestReadOnlySubscriberRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	c.Downstream.Clients = []config.Client{{Sender: "TRADING"}, {Sender: "RISK", ReadOnly: true, ReceiveMessageTypes: []string{"8"}}}
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
	defer func() { r.Stop() }()
	wait(t, ctx, venue.Logons)
	peers := []*testfix.Peer{testfix.NewPeer(), testfix.NewPeer()}
	settings := make([]*quickfix.Settings, 2)
	ids := make([]quickfix.SessionID, 2)
	clients := make([]*testfix.Initiator, 2)
	for i, name := range []string{"TRADING", "RISK"} {
		settings[i], ids[i], err = testfix.Settings(name, c.Downstream.Target, c.Downstream.Listen, c.Downstream.Dictionary, true)
		if err != nil {
			t.Fatal(err)
		}
		settings[i].GlobalSettings().Set("FileStorePath", filepath.Join(t.TempDir(), name))
		clients[i], err = testfix.ConnectDurable(peers[i], settings[i])
		if err != nil {
			t.Fatal(err)
		}
		wait(t, ctx, peers[i].Logons)
	}
	defer func() {
		for _, client := range clients {
			client.Stop()
		}
	}()
	waitLogged(t, ctx, r, 3)
	if err = quickfix.SendToTarget(testfix.Report("before-disconnect"), venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[0].Messages)
	first := wait(t, ctx, peers[1].Messages)
	seq, err := first.Header.GetInt(34)
	if err != nil {
		t.Fatal(err)
	}
	riskGatewayID := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: c.Downstream.Target, TargetCompID: "RISK"}
	beforeFilter, err := quickfix.GetExpectedSenderNum(riskGatewayID)
	if err != nil {
		t.Fatal(err)
	}
	filtered := quickfix.NewMessage()
	filtered.Header.SetString(35, "9")
	for tag, value := range map[quickfix.Tag]string{37: "mock-order", 11: "filtered-cancel", 41: "original", 39: "0", 434: "1"} {
		filtered.Body.SetString(tag, value)
	}
	if err = quickfix.SendToTarget(filtered, venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[0].Messages)
	afterFilter, err := quickfix.GetExpectedSenderNum(riskGatewayID)
	if err != nil || afterFilter != beforeFilter {
		t.Fatal("filtered message consumed a sequence number")
	}
	store, err := quickfix.GetMessageStore(riskGatewayID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetMessages(1, afterFilter-1)
	if err != nil {
		t.Fatal(err)
	}
	seenReport := false
	for _, raw := range stored {
		if strings.Contains(string(raw), "filtered-cancel") {
			t.Fatal("filtered message entered the resend store")
		}
		if strings.Contains(string(raw), "before-disconnect") {
			seenReport = true
		}
	}
	if !seenReport {
		t.Fatal("sent report was not present in resend store")
	}
	clients[1].Stop()
	waitLogged(t, ctx, r, 2)
	if err = quickfix.SendToTarget(testfix.Report("while-offline"), venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[0].Messages)
	peers[1] = testfix.NewPeer()
	clients[1], err = testfix.ConnectDurable(peers[1], settings[1])
	if err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[1].Logons)
	waitLogged(t, ctx, r, 3)
	if err = testfix.WaitTarget(ctx, ids[1], seq+1); err != nil {
		t.Fatal(err)
	}
	if err = quickfix.SetNextTargetMsgSeqNum(ids[1], seq); err != nil {
		t.Fatal(err)
	}
	request := quickfix.NewMessage()
	request.Header.SetString(35, "2")
	request.Body.SetInt(7, seq)
	request.Body.SetInt(16, seq)
	if err = quickfix.SendToTarget(request, ids[1]); err != nil {
		t.Fatal(err)
	}
	replayed := wait(t, ctx, peers[1].Messages)
	id, _ := replayed.Body.GetString(11)
	dup, _ := replayed.Header.GetBool(43)
	if id != "before-disconnect" || !dup {
		t.Fatal("stored report was not correctly replayed")
	}
	if err = quickfix.SendToTarget(testfix.Report("after-reconnect"), venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[0].Messages)
	msg := wait(t, ctx, peers[1].Messages)
	id, _ = msg.Body.GetString(11)
	if id != "after-reconnect" {
		t.Fatalf("unexpected offline backlog: %q", id)
	}
	assertReadOnly := func() {
		t.Helper()
		if err := quickfix.SendToTarget(testfix.Order("blocked"), ids[1]); err != nil {
			t.Fatal(err)
		}
		reject := wait(t, ctx, peers[1].Messages)
		typ, _ := reject.MsgType()
		text, _ := reject.Body.GetString(58)
		if typ != "j" || !strings.Contains(text, "read-only") {
			t.Fatalf("unexpected policy response %s %s", typ, text)
		}
		if err := quickfix.SendToTarget(testfix.Order("allowed"), ids[0]); err != nil {
			t.Fatal(err)
		}
		forwarded := wait(t, ctx, venue.Messages)
		id, _ := forwarded.Body.GetString(11)
		if id != "allowed" {
			t.Fatalf("unexpected upstream submission %s", id)
		}
	}
	assertReadOnly()
	r.Stop()
	wait(t, ctx, peers[0].Logouts)
	wait(t, ctx, peers[1].Logouts)
	wait(t, ctx, venue.Logouts)
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, venue.Logons)
	wait(t, ctx, peers[0].Logons)
	wait(t, ctx, peers[1].Logons)
	waitLogged(t, ctx, r, 3)
	assertReadOnly()
	beforeFilter, err = quickfix.GetExpectedSenderNum(riskGatewayID)
	if err != nil {
		t.Fatal(err)
	}
	if err = quickfix.SendToTarget(filtered, venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[0].Messages)
	afterFilter, err = quickfix.GetExpectedSenderNum(riskGatewayID)
	if err != nil || afterFilter != beforeFilter {
		t.Fatal("receive filter did not survive restart")
	}
	if err = quickfix.SendToTarget(testfix.Report("after-restart"), venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, peers[0].Messages)
	last := wait(t, ctx, peers[1].Messages)
	id, _ = last.Body.GetString(11)
	if id != "after-restart" {
		t.Fatalf("unexpected report after restart: %s", id)
	}
}
