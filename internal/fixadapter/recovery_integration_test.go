//go:build integration

package fixadapter

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"testing"
	"time"

	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
)

func wait[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	v, err := testfix.Receive(ctx, ch)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPersistentRestartAndIndependentResend(t *testing.T) {
	if testing.Short() {
		t.Skip("real FIX sockets")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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
	client := testfix.NewPeer()
	cs, clientID, err := testfix.Settings("CLIENT", c.Downstream.Target, c.Downstream.Listen, c.Downstream.Dictionary, true)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := testfix.Connect(client, cs)
	if err != nil {
		t.Fatal(err)
	}
	defer ci.Stop()
	wait(t, ctx, client.Logons)
	waitLogged(t, ctx, r, 2)
	if err = quickfix.SendToTarget(testfix.Report("before-restart"), venueID); err != nil {
		t.Fatal(err)
	}
	report := wait(t, ctx, client.Messages)
	seq, e := report.Header.GetInt(34)
	if e != nil {
		t.Fatal(e)
	}
	// Model a receiver missing an already assigned outbound message. The genuine
	// FIX ResendRequest is processed by the gateway's own downstream store.
	if err = testfix.WaitTarget(ctx, clientID, seq+1); err != nil {
		t.Fatal(err)
	}
	if err = quickfix.SetNextTargetMsgSeqNum(clientID, seq); err != nil {
		t.Fatal(err)
	}
	req := quickfix.NewMessage()
	req.Header.SetString(35, "2")
	req.Body.SetInt(7, seq)
	req.Body.SetInt(16, seq)
	if err = quickfix.SendToTarget(req, clientID); err != nil {
		t.Fatal(err)
	}
	resent := wait(t, ctx, client.Messages)
	dup, _ := resent.Header.GetBool(43)
	orig, _ := resent.Header.GetString(122)
	id, _ := resent.Body.GetString(11)
	if !dup || orig == "" || id != "before-restart" {
		t.Fatal("downstream replay metadata/fidelity")
	}
	if r.Router.Counters.Recipients.Load() != 1 {
		t.Fatal("outbound replay caused new broadcast")
	}
	upBefore, err := quickfix.GetExpectedSenderNum(r.Application.Upstream)
	if err != nil {
		t.Fatal(err)
	}
	r.Stop()
	if r.Pending.Usage() != 0 {
		t.Fatal("engine queue credits did not follow actual wire sends")
	}
	wait(t, ctx, client.Logouts)
	wait(t, ctx, venue.Logouts)
	r, err = New(c)
	if err != nil {
		t.Fatal("restart failed", err)
	}
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, venue.Logons)
	wait(t, ctx, client.Logons)
	waitLogged(t, ctx, r, 2)
	upAfter, err := quickfix.GetExpectedSenderNum(r.Application.Upstream)
	if err != nil || upAfter <= upBefore {
		t.Fatal("upstream sequence state reset", upBefore, upAfter, err)
	}
	if err = quickfix.SendToTarget(testfix.Report("after-restart"), venueID); err != nil {
		t.Fatal(err)
	}
	next := wait(t, ctx, client.Messages)
	nextSeq, _ := next.Header.GetInt(34)
	if nextSeq <= seq {
		t.Fatal("downstream sequence reset")
	}
	// Fault injection: persist an assigned venue report without writing it to the
	// socket, then emit its successor. The real gap triggers upstream FIX resend.
	store, err := quickfix.GetMessageStore(venueID)
	if err != nil {
		t.Fatal(err)
	}
	missingSeq := store.NextSenderMsgSeqNum()
	missing := testfix.Report("recovered-gap")
	missing.Header.SetString(8, "FIX.4.4")
	missing.Header.SetString(49, venueID.SenderCompID)
	missing.Header.SetString(56, venueID.TargetCompID)
	missing.Header.SetInt(34, missingSeq)
	missing.Header.SetString(52, time.Now().UTC().Format("20060102-15:04:05.000"))
	if err = store.SaveMessageAndIncrNextSenderMsgSeqNum(missingSeq, missing.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = quickfix.SendToTarget(testfix.Report("after-gap"), venueID); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	for i := 0; i < 2; i++ {
		m := wait(t, ctx, client.Messages)
		id, _ := m.Body.GetString(11)
		got[id] = true
	}
	if !got["recovered-gap"] || !got["after-gap"] {
		t.Fatal("genuine replay lost", got)
	}
}

func waitLogged(t *testing.T, ctx context.Context, r *Runtime, count int) {
	t.Helper()
	for {
		states, ch := r.Registry.Watch()
		n := 0
		for _, s := range states {
			if s.LoggedOn {
				n++
			}
		}
		if n == count {
			return
		}
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("gateway logon registry timed out")
		}
	}
}
