//go:build integration && socketsoak

package fixadapter

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/fs"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/maintenance"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
)

func TestSocketSoak(t *testing.T) {
	if testing.Short() {
		t.Skip("extended FIX socket soak")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	start := time.Now()
	vl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer vl.Close()
	ml, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ml.Close()
	c := runtimeConfig(t)
	c.Downstream.Listen = ml.Addr().String()
	_, port, _ := net.SplitHostPort(vl.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(port)
	c.Downstream.Clients = nil
	for i := 0; i < 10; i++ {
		c.Downstream.Clients = append(c.Downstream.Clients, config.Client{Sender: fmt.Sprintf("SOAK_%02d", i)})
	}
	venue := testfix.NewPeer()
	vs, venueID, err := testfix.Settings(c.Upstream.Target, c.Upstream.Sender, vl.Addr().String(), c.Upstream.Dictionary, false)
	if err != nil {
		t.Fatal(err)
	}
	va, err := testfix.Listen(venue, vl, vs)
	if err != nil {
		t.Fatal(err)
	}
	defer va.Stop()
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return ml, nil })
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	wait(t, ctx, venue.Logons)
	peers := make([]*testfix.Peer, 10)
	clients := make([]*testfix.Initiator, 10)
	settings := make([]*quickfix.Settings, 10)
	ids := make([]quickfix.SessionID, 10)
	lastSeq := make([]int, 10)
	defer func() {
		for _, i := range clients {
			if i != nil {
				i.Stop()
			}
		}
	}()
	for i, cl := range c.Downstream.Clients {
		peers[i] = testfix.NewPeer()
		s, id, e := testfix.Settings(cl.Sender, c.Downstream.Target, c.Downstream.Listen, c.Downstream.Dictionary, true)
		if e != nil {
			t.Fatal(e)
		}
		s.GlobalSettings().Set("FileStorePath", filepath.Join(t.TempDir(), "client"))
		settings[i] = s
		ids[i] = id
		clients[i], e = testfix.ConnectDurable(peers[i], s)
		if e != nil {
			t.Fatal(e)
		}
		wait(t, ctx, peers[i].Logons)
	}
	waitLogged(t, ctx, r, 11)
	var recipients uint64
	for n := 0; n < 50000; n++ {
		if n%1000 == 0 {
			t.Logf("progress orders=%d elapsed=%s engine_queue_bytes=%d failed=%d", n, time.Since(start), r.Pending.Usage(), r.Router.Counters.Failed.Load())
		}
		offline := -1
		if n%500 == 0 {
			offline = (n/500)%9 + 1
			clients[offline].Stop()
			waitLogged(t, ctx, r, 10)
		}
		id := fmt.Sprintf("synthetic-soak-%d", n)
		if err = quickfix.SendToTarget(testfix.Order(id), ids[0]); err != nil {
			t.Fatal(err)
		}
		order := wait(t, ctx, venue.Messages)
		got, _ := order.Body.GetString(11)
		if got != id {
			t.Fatal("upstream order changed")
		}
		report := testfix.Report(id)
		report.Body.SetString(17, fmt.Sprintf("synthetic-exec-%d", n))
		if err = quickfix.SendToTarget(report, venueID); err != nil {
			t.Fatal(err)
		}
		deliveryContext, finishDelivery := context.WithTimeout(ctx, 15*time.Second)
		for i, p := range peers {
			if i == offline {
				continue
			}
			m, e := testfix.Receive(deliveryContext, p.Messages)
			if e != nil {
				finishDelivery()
				t.Fatalf("report delivery stalled: order=%d target=%d failed=%d dropped=%d engine_queue_bytes=%d ready=%t", n, i, r.Router.Counters.Failed.Load(), r.Router.Counters.Dropped.Load(), r.Pending.Usage(), r.Snapshot().Ready)
			}
			got, _ := m.Body.GetString(11)
			target, _ := m.Header.GetString(56)
			seq, _ := m.Header.GetInt(34)
			if got != id || target != ids[i].SenderCompID || seq <= lastSeq[i] {
				t.Fatal("fanout identity/order/offline queue regression")
			}
			lastSeq[i] = seq
			recipients++
		}
		finishDelivery()
		if offline >= 0 {
			peers[offline] = testfix.NewPeer()
			clients[offline], err = testfix.ConnectDurable(peers[offline], settings[offline])
			if err != nil {
				t.Fatal(err)
			}
			wait(t, ctx, peers[offline].Logons)
			waitLogged(t, ctx, r, 11)
		}
	}
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
	if r.Router.Counters.Forwarded.Load() != 50000+recipients || r.Router.Counters.Failed.Load() != 0 || r.Router.QueueBytes() != 0 || r.Pending.Usage() != 0 {
		t.Fatal("socket soak accounting/resource failure")
	}
	before := directoryBytes(t, c.Downstream.StoreDir)
	c.Retention.IdleTTL = time.Nanosecond
	if err = maintenance.Run(c, "prune", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	after := directoryBytes(t, c.Downstream.StoreDir)
	body, _ := filepath.Glob(filepath.Join(c.Downstream.StoreDir, "*", "*.body"))
	up, _ := filepath.Glob(filepath.Join(c.Upstream.StoreDir, "*", "*.body"))
	if len(body) != 0 || len(up) != 1 || after >= before {
		t.Fatal("offline cleanup failed to return disk to metadata baseline")
	}
	t.Logf("50,000 orders and reports; 10 socket clients; 100 reconnects; %d report deliveries; elapsed=%s; downstream bytes before=%d after=%d", recipients, time.Since(start), before, after)
}

func directoryBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return total
}
