//go:build integration

package fixadapter

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/fileoutput"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
)

func TestFileOutputWithoutLoggedOnDownstreamClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	venueListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer venueListener.Close()
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer clientListener.Close()
	c := runtimeConfig(t)
	c.Downstream.Listen = clientListener.Addr().String()
	_, port, _ := net.SplitHostPort(venueListener.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(port)
	c.Outputs = []config.Output{{Name: "dropcopy", Type: "file", Directory: filepath.Join(t.TempDir(), "capture"), ReceiveMessageTypes: []string{"8"}}}
	venue := testfix.NewPeer()
	settings, venueID, err := testfix.Settings(c.Upstream.Target, c.Upstream.Sender, venueListener.Addr().String(), c.Upstream.Dictionary, false)
	if err != nil {
		t.Fatal(err)
	}
	acceptor, err := testfix.Listen(venue, venueListener, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer acceptor.Stop()
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	captured := make(chan struct{}, 1)
	r.Application.FileOutputs = filePublishFunc(func(m fileoutput.Message) { r.FileOutputs.Publish(m); captured <- struct{}{} })
	r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return clientListener, nil })
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, venue.Logons)
	waitLogged(t, ctx, r, 1)
	if err = quickfix.SendToTarget(testfix.Report("no-network-subscribers"), venueID); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, captured)
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
	records := readCaptureRecords(t, c.Outputs[0].Directory)
	if len(records) != 1 || records[0].MsgType != "8" || records[0].PossDupFlag != "" || len(records[0].FIX) == 0 {
		t.Fatal("file subscriber required a logged-on downstream client or lost metadata")
	}
}
