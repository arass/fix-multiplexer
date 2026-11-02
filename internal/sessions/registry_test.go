package sessions

import (
	"sync"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := New(map[string]bool{"A": true, "B": false}, func() time.Time { return now })
	_, ch := r.Watch()
	if r.SetLoggedOn("A", true) != nil {
		t.Fatal("logon")
	}
	<-ch
	s, _ := r.Get("A")
	if !r.Live("A", s.Generation) {
		t.Fatal("not live")
	}
	if r.Live("A", 0) {
		t.Fatal("stale generation")
	}
	if r.SetLoggedOn("A", true) != nil {
		t.Fatal("idempotent logon")
	}
	if r.SetLoggedOn("B", true) == nil || r.SetLoggedOn("unknown", true) == nil {
		t.Fatal("unauthorized")
	}
	now = now.Add(time.Hour)
	if r.SetLoggedOn("A", false) != nil {
		t.Fatal("logout")
	}
	s, _ = r.Get("A")
	if s.LastDisconnect != now || r.Live("A", s.Generation) {
		t.Fatal("logout state")
	}
	if r.Disable("A") != nil || r.Disable("A") != nil || r.SetLoggedOn("A", true) == nil || r.Disable("unknown") == nil {
		t.Fatal("disable")
	}
	if r.SetLoggedOn("unknown", false) == nil {
		t.Fatal("unknown logout")
	}
	if got := r.Snapshot(); len(got) != 2 || got[0].ID != "A" {
		t.Fatal(got)
	}
}

func TestConcurrent(t *testing.T) {
	r := New(map[string]bool{"A": true}, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.SetLoggedOn("A", true)
				r.Snapshot()
				r.Watch()
				_ = r.SetLoggedOn("A", false)
			}
		}()
	}
	wg.Wait()
}
