package transport

import (
	"sync"
	"testing"
)

func TestPendingBudgetsAndCredits(t *testing.T) {
	p := NewPending(400, 300)
	first, second := []byte("synthetic-one"), []byte("synthetic-two")
	if p.Reserve("A", first) != nil {
		t.Fatal("first reservation")
	}
	if p.Reserve("A", first) == nil {
		t.Fatal("duplicate reservation")
	}
	if p.Reserve("B", second) != nil {
		t.Fatal("independent session")
	}
	if p.Reserve("A", []byte("third")) == nil {
		t.Fatal("global budget")
	}
	p.Sent(first)
	p.Sent(first)
	p.Sent(second)
	if p.Usage() != 0 {
		t.Fatal("credit leak")
	}
	p = NewPending(1000, 150)
	if p.Reserve("A", first) != nil || p.Reserve("A", second) == nil {
		t.Fatal("per-session budget")
	}
	if p.Reserve("B", second) != nil {
		t.Fatal("unrelated client blocked")
	}
}

func TestPendingConcurrentAccess(t *testing.T) {
	p := NewPending(1<<20, 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(v byte) {
			defer wg.Done()
			data := []byte{v}
			for j := 0; j < 100; j++ {
				if p.Reserve("A", data) == nil {
					p.Sent(data)
				}
				p.Usage()
			}
		}(byte(i))
	}
	wg.Wait()
	if p.Usage() != 0 {
		t.Fatal("reservation leak")
	}
}
