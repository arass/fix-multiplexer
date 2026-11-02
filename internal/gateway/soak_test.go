//go:build soak

package gateway

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fix-multiplexer/internal/sessions"
)

type soakMessage struct {
	typ   string
	batch *sync.WaitGroup
}

func (m *soakMessage) Type() string            { return m.typ }
func (m *soakMessage) Size() int               { return 64 }
func (m *soakMessage) Clone() (Message, error) { v := *m; return &v, nil }

func TestRoutingSoak(t *testing.T) {
	before := runtime.NumGoroutine()
	start := time.Now()
	enabled := map[string]bool{"up": true}
	var clients []string
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("client_%d", i)
		enabled[id] = true
		clients = append(clients, id)
	}
	r := sessions.New(enabled, time.Now)
	for id := range enabled {
		_ = r.SetLoggedOn(id, true)
	}
	var attempts, failures atomic.Uint64
	g := New(Options{Upstream: "up", Clients: clients, UpstreamTypes: []string{"D"}, DownstreamTypes: []string{"8"}, UpstreamQueue: 128, DownstreamQueue: 128, ByteBudget: 1 << 20}, r, SendFunc(func(m Message, id string) error {
		n := attempts.Add(1)
		if batch := m.(*soakMessage).batch; batch != nil {
			defer batch.Done()
		}
		if id == "client_0" && n%17 == 0 {
			failures.Add(1)
			return errors.New("synthetic recipient failure")
		}
		return nil
	}))
	defer g.Close()
	var recipients uint64
	for batch := 0; batch < 500; batch++ {
		live := 10
		if batch%5 == 0 {
			_ = r.SetLoggedOn("client_0", false)
			live = 9
		} else if batch%5 == 1 {
			_ = r.SetLoggedOn("client_0", true)
		}
		var delivered sync.WaitGroup
		delivered.Add(live * 100)
		recipients += uint64(live * 100)
		for i := 0; i < 100; i++ {
			if err := g.Submit("client_1", &soakMessage{typ: "D"}); err != nil {
				t.Fatal(err)
			}
			if err := g.Broadcast(&soakMessage{typ: "8", batch: &delivered}); err != nil {
				t.Fatal(err)
			}
		}
		delivered.Wait()
	}
	g.Close()
	if g.QueueBytes() != 0 || g.QueueDepth() != 0 || g.Counters.Dropped.Load() != 0 {
		t.Fatal("resource/queue leak")
	}
	if attempts.Load() != 50000+recipients || g.Counters.Failed.Load() != failures.Load() {
		t.Fatal("partial-failure accounting")
	}
	if runtime.NumGoroutine() > before+4 {
		t.Fatal("worker goroutine leak")
	}
	t.Logf("50,000 submissions, %d fanout attempts, 10 clients, 100 disconnect/reconnect cycles in %s; synthetic failures=%d", recipients, time.Since(start), failures.Load())
}
