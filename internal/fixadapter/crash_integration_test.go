//go:build integration

package fixadapter

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
	"gopkg.in/yaml.v3"
)

func TestCrashHelper(t *testing.T) {
	if os.Getenv("FIX_MUX_CRASH_HELPER") != "1" {
		return
	}
	f, err := os.Open(os.Getenv("FIX_MUX_CRASH_CONFIG"))
	if err != nil {
		t.Fatal("helper config unavailable")
	}
	c, err := config.Load(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	waitLogged(t, ctx, r, 2)
	fmt.Println("HELPER_READY")
	select {}
}

func TestHardCrashRequiresLeaseRecoveryThenRestoresSequences(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess and FIX sockets")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	vl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer vl.Close()
	ml, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ml.Addr().String()
	ml.Close()
	c := runtimeConfig(t)
	c.Service.HealthBind = ""
	c.Downstream.Listen = address
	_, p, _ := net.SplitHostPort(vl.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(p)
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
	path := filepath.Join(t.TempDir(), "synthetic.yaml")
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashHelper$")
	child.Env = append(os.Environ(), "FIX_MUX_CRASH_HELPER=1", "FIX_MUX_CRASH_CONFIG="+path)
	pipe, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() })
	client := testfix.NewPeer()
	cs, clientID, err := testfix.Settings("CLIENT", c.Downstream.Target, address, c.Downstream.Dictionary, true)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := testfix.Connect(client, cs)
	if err != nil {
		t.Fatal(err)
	}
	defer ci.Stop()
	_ = clientID
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if scanner.Text() == "HELPER_READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	if !wait(t, ctx, ready) {
		t.Fatal("crash helper failed to start")
	}
	wait(t, ctx, venue.Logons)
	wait(t, ctx, client.Logons)
	if err = quickfix.SendToTarget(testfix.Report("persisted-before-kill"), venueID); err != nil {
		t.Fatal(err)
	}
	before := wait(t, ctx, client.Messages)
	seq, _ := before.Header.GetInt(34)
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	wait(t, ctx, client.Logouts)
	wait(t, ctx, venue.Logouts)
	if r, err := New(c); err == nil {
		r.Stop()
		t.Fatal("crash lease was silently bypassed")
	}
	// Simulate the documented operator step only after Wait proves the owner is dead.
	for _, root := range []string{c.Upstream.StoreDir, c.Downstream.StoreDir} {
		if err = os.Remove(filepath.Join(root, ".gateway-lock")); err != nil {
			t.Fatal(err)
		}
	}
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	wait(t, ctx, venue.Logons)
	wait(t, ctx, client.Logons)
	waitLogged(t, ctx, r, 2)
	if err = quickfix.SendToTarget(testfix.Report("after-kill-recovery"), venueID); err != nil {
		t.Fatal(err)
	}
	after := wait(t, ctx, client.Messages)
	next, _ := after.Header.GetInt(34)
	if next <= seq {
		t.Fatal("crash reset durable sequence state")
	}
}
