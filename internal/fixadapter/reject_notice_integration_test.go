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

func TestUpstreamRejectNotifiesAllLiveClients(t *testing.T) {
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
	c.Downstream.Clients = append(c.Downstream.Clients, config.Client{Sender: "CLIENT2", ReadOnly: true, ReceiveMessageTypes: []string{}})
	// Gateway notices must survive even a policy excluding business rejects.
	c.Fanout.AppTypes = []string{"8"}
	c.Fanout.DenyTypes = []string{"j"}
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
	clients := []*testfix.Peer{testfix.NewPeer(), testfix.NewPeer()}
	for i, name := range []string{"CLIENT", "CLIENT2"} {
		settings, _, err := testfix.Settings(name, c.Downstream.Target, c.Downstream.Listen, c.Downstream.Dictionary, true)
		if err != nil {
			t.Fatal(err)
		}
		initiator, err := testfix.Connect(clients[i], settings)
		if err != nil {
			t.Fatal(err)
		}
		defer initiator.Stop()
		wait(t, ctx, clients[i].Logons)
	}
	waitLogged(t, ctx, r, 3)
	reject := quickfix.NewMessage()
	reject.Header.SetString(35, "3")
	reject.Body.SetInt(45, 123)
	reject.Body.SetString(372, "D")
	reject.Body.SetInt(371, 55)
	reject.Body.SetInt(373, 1)
	reject.Body.SetString(58, "synthetic missing field")
	if err = quickfix.SendToTarget(reject, venueID); err != nil {
		t.Fatal(err)
	}
	for _, client := range clients {
		m := wait(t, ctx, client.Messages)
		typ, _ := m.MsgType()
		refType, _ := m.Body.GetString(372)
		reason, _ := m.Body.GetInt(380)
		text, _ := m.Body.GetString(58)
		if typ != "j" || refType != "3" || reason != 0 || m.Body.Has(45) || !strings.Contains(text, "upstream RefSeqNum=123") || !strings.Contains(text, "upstream RefMsgType=D") || !strings.Contains(text, "upstream RefTagID=55") || !strings.Contains(text, "upstream SessionRejectReason=1") || !strings.Contains(text, "synthetic missing field") {
			t.Fatal("incorrect reject notification")
		}
	}
}
