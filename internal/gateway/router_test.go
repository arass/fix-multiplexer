package gateway

import (
	"errors"
	"sync"
	"testing"
	"time"

	"fix-multiplexer/internal/sessions"
)

type testMessage struct {
	typ   string
	size  int
	value string
	fail  bool
}

func (m *testMessage) Type() string { return m.typ }
func (m *testMessage) Size() int    { return m.size }
func (m *testMessage) Clone() (Message, error) {
	if m.fail {
		return nil, errors.New("copy failed")
	}
	v := *m
	return &v, nil
}

type delivery struct {
	id string
	m  Message
}

func setup(t *testing.T, send SendFunc) (*Router, *sessions.Registry) {
	t.Helper()
	r := sessions.New(map[string]bool{"up": true, "a": true, "b": true, "off": true, "disabled": false}, time.Now)
	for _, id := range []string{"up", "a", "b"} {
		if err := r.SetLoggedOn(id, true); err != nil {
			t.Fatal(err)
		}
	}
	g := New(Options{Upstream: "up", Clients: []string{"a", "b", "off", "disabled"}, UpstreamTypes: []string{"D", "F", "A"}, DownstreamTypes: []string{"8", "9", "A"}, DenyTypes: []string{"F", "9"}, UpstreamQueue: 1, DownstreamQueue: 1, ByteBudget: 1024}, r, send)
	t.Cleanup(g.Close)
	return g, r
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("event timed out")
		var v T
		return v
	}
}

func TestForwardPolicyAndFailure(t *testing.T) {
	sent := make(chan delivery, 10)
	g, r := setup(t, func(m Message, id string) error { sent <- delivery{id, m}; return nil })
	m := &testMessage{typ: "D", size: 10, value: "same-id"}
	if err := g.Submit("a", m); err != nil {
		t.Fatal(err)
	}
	d := receive(t, sent)
	if d.id != "up" || d.m == m || d.m.(*testMessage).value != "same-id" {
		t.Fatal("bad forwarding")
	}
	for _, typ := range []string{"F", "A", "?"} {
		if !errors.Is(g.Submit("a", &testMessage{typ: typ}), ErrPolicy) {
			t.Fatal(typ)
		}
	}
	for _, id := range []string{"unknown", "disabled", "off"} {
		if !errors.Is(g.Submit(id, m), sessions.ErrUnavailable) {
			t.Fatal(id)
		}
	}
	if !g.Ready() {
		t.Fatal("not ready")
	}
	_ = r.SetLoggedOn("up", false)
	if g.Ready() || !errors.Is(g.Submit("a", m), ErrUnavailable) {
		t.Fatal("outage")
	}
	_ = r.SetLoggedOn("up", true)
	if err := g.Submit("a", &testMessage{typ: "D", fail: true}); !errors.Is(err, ErrSend) {
		t.Fatal(err)
	}
	if err := g.Submit("a", &testMessage{typ: "D", size: 1025}); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	if err := g.Submit("a", &testMessage{typ: "D", size: -1}); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	g.Close()
	g.Close()
	if g.Ready() || !errors.Is(g.Submit("a", m), ErrStopped) {
		t.Fatal("shutdown")
	}
	if g.QueueBytes() != 0 {
		t.Fatal("budget leak")
	}
}

func TestPerClientPolicies(t *testing.T) {
	sent := make(chan delivery, 20)
	r := sessions.New(map[string]bool{"up": true, "a": true, "b": true, "c": true}, time.Now)
	for _, id := range []string{"up", "a", "b", "c"} {
		if err := r.SetLoggedOn(id, true); err != nil {
			t.Fatal(err)
		}
	}
	g := New(Options{Upstream: "up", Clients: []string{"a", "b", "c"}, ClientPolicies: map[string]ClientPolicy{"a": {ReceiveTypes: []string{"9"}}, "b": {ReadOnly: true, ReceiveTypes: []string{"8"}}, "c": {ReadOnly: true, ReceiveTypes: []string{}}}, UpstreamTypes: []string{"D", "F", "G", "H", "V", "x"}, DownstreamTypes: []string{"8", "9"}, UpstreamQueue: 4, DownstreamQueue: 4, ByteBudget: 1024}, r, SendFunc(func(m Message, id string) error { sent <- delivery{id, m}; return nil }))
	t.Cleanup(g.Close)
	for _, typ := range []string{"D", "F", "G", "H", "V", "x"} {
		if err := g.Submit("b", &testMessage{typ: typ}); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if g.Counters.Blocked.Load() != 6 || g.QueueDepth() != 0 {
		t.Fatal("blocked submissions were queued or not counted")
	}
	if err := g.Submit("a", &testMessage{typ: "D"}); err != nil {
		t.Fatal(err)
	}
	if d := receive(t, sent); d.id != "up" {
		t.Fatal(d.id)
	}
	if err := g.Broadcast(&testMessage{typ: "8"}); err != nil {
		t.Fatal(err)
	}
	if d := receive(t, sent); d.id != "b" {
		t.Fatal(d.id)
	}
	if err := g.Broadcast(&testMessage{typ: "9"}); err != nil {
		t.Fatal(err)
	}
	if d := receive(t, sent); d.id != "a" {
		t.Fatal(d.id)
	}
	if err := g.BroadcastNotice(&testMessage{typ: "j"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for i := 0; i < 3; i++ {
		got[receive(t, sent).id] = true
	}
	if len(got) != 3 {
		t.Fatal(got)
	}
}

func TestSubscriberInheritanceAndGlobalDeny(t *testing.T) {
	sent := make(chan delivery, 4)
	r := sessions.New(map[string]bool{"up": true, "a": true, "b": true}, time.Now)
	for _, id := range []string{"up", "a", "b"} {
		if err := r.SetLoggedOn(id, true); err != nil {
			t.Fatal(err)
		}
	}
	g := New(Options{Upstream: "up", Clients: []string{"a", "b"}, ClientPolicies: map[string]ClientPolicy{"b": {ReadOnly: true}}, UpstreamTypes: []string{}, DownstreamTypes: []string{"8", "9"}, DenyTypes: []string{"9"}, UpstreamQueue: 2, DownstreamQueue: 2, ByteBudget: 1024}, r, SendFunc(func(m Message, id string) error { sent <- delivery{id, m}; return nil }))
	t.Cleanup(g.Close)
	if err := g.Submit("a", &testMessage{typ: "D"}); !errors.Is(err, ErrPolicy) {
		t.Fatal(err)
	}
	if err := g.Submit("b", &testMessage{typ: "D"}); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	if err := g.Submit("up", &testMessage{typ: "D"}); !errors.Is(err, sessions.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := g.Broadcast(&testMessage{typ: "9"}); !errors.Is(err, ErrPolicy) {
		t.Fatal(err)
	}
	if err := g.Broadcast(&testMessage{typ: "8"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		got[receive(t, sent).id] = true
	}
	if !got["a"] || !got["b"] {
		t.Fatal(got)
	}
}

func TestFanoutIndependentAndOffline(t *testing.T) {
	sent := make(chan delivery, 10)
	g, _ := setup(t, func(m Message, id string) error {
		sent <- delivery{id, m}
		if id == "a" {
			return errors.New("broken")
		}
		return nil
	})
	m := &testMessage{typ: "8", size: 20, value: "report"}
	if err := g.Broadcast(m); err != nil {
		t.Fatal(err)
	}
	a, b := receive(t, sent), receive(t, sent)
	if a.id == b.id || a.m == b.m || a.m == m || b.m == m {
		t.Fatal("shared/incorrect recipients")
	}
	if err := g.Broadcast(&testMessage{typ: "9"}); !errors.Is(err, ErrPolicy) {
		t.Fatal(err)
	}
	g.Close()
	if g.Counters.Recipients.Load() != 2 || g.Counters.Failed.Load() != 1 || g.Counters.Forwarded.Load() != 1 || g.QueueBytes() != 0 {
		t.Fatal("counters")
	}
}

func TestSlowConsumerQueueAndGeneration(t *testing.T) {
	entered := make(chan string, 5)
	release := make(chan struct{})
	g, r := setup(t, func(m Message, id string) error {
		entered <- id
		if id == "a" {
			<-release
		}
		return nil
	})
	_ = r.SetLoggedOn("b", false)
	m := &testMessage{typ: "8", size: 40}
	_ = g.Broadcast(m)
	if receive(t, entered) != "a" {
		t.Fatal("target")
	}
	_ = g.Broadcast(m)
	_ = g.Broadcast(m)
	if g.Counters.Dropped.Load() != 1 {
		t.Fatal("queue overflow not counted")
	}
	_ = r.SetLoggedOn("b", true)
	_ = g.Broadcast(m)
	if receive(t, entered) != "b" {
		t.Fatal("slow consumer blocked healthy client")
	}
	_ = r.SetLoggedOn("a", false)
	_ = r.SetLoggedOn("a", true)
	close(release)
	g.Close()
	if g.QueueBytes() != 0 {
		t.Fatal("budget leak")
	}
}

func TestUpstreamQueueAndConcurrentShutdown(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	g, _ := setup(t, func(Message, string) error { entered <- struct{}{}; <-release; return errors.New("failed") })
	m := &testMessage{typ: "D", size: 1}
	first := make(chan error, 1)
	go func() { first <- g.Submit("a", m) }()
	receive(t, entered)
	// Enqueue directly to make saturation deterministic without sleeps.
	if err := g.enqueue(g.upstream, job{target: "up", generation: 1, msg: m}); err != nil {
		t.Fatal(err)
	}
	if err := g.Submit("b", m); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); g.Close() }()
	}
	close(release)
	wg.Wait()
	if err := receive(t, first); !errors.Is(err, ErrSend) {
		t.Fatal(err)
	}
	if g.QueueBytes() != 0 {
		t.Fatal("budget leak")
	}
}

func TestPolicyAndByteBudget(t *testing.T) {
	for _, typ := range []string{"0", "1", "2", "3", "4", "5", "A"} {
		if allowed(typ, []string{typ}, nil) {
			t.Fatal(typ)
		}
	}
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	g, _ := setup(t, func(Message, string) error { entered <- struct{}{}; <-release; return nil })
	m := &testMessage{typ: "D", size: 1024}
	done := make(chan error, 1)
	go func() { done <- g.Submit("a", m) }()
	receive(t, entered)
	if err := g.Submit("b", m); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	close(release)
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}
