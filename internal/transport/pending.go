package transport

import (
	"crypto/sha256"
	"sync"
)

type reservation struct {
	id    string
	bytes int64
}

// Pending bounds application bytes admitted to the engine's internal send queue.
// Hashing uses the engine's serialized frame, without parsing or logging it.
// OnOutgoing releases local queue credit, not a delivery acknowledgment.
type Pending struct {
	mu                      sync.Mutex
	limit, perSession, used int64
	sessions                map[string]int64
	frames                  map[[32]byte]reservation
}

func NewPending(limit, perSession int64) *Pending {
	return &Pending{limit: limit, perSession: perSession, sessions: make(map[string]int64), frames: make(map[[32]byte]reservation)}
}
func (p *Pending) Reserve(id string, b []byte) error {
	hash := sha256.Sum256(b)
	cost := int64(len(b) + 128)
	p.mu.Lock()
	defer p.mu.Unlock()
	if cost > p.limit-p.used || cost > p.perSession-p.sessions[id] {
		return ErrLimit
	}
	if _, exists := p.frames[hash]; exists {
		return ErrLimit
	}
	p.frames[hash] = reservation{id, cost}
	p.used += cost
	p.sessions[id] += cost
	return nil
}
func (p *Pending) Sent(b []byte) {
	hash := sha256.Sum256(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.frames[hash]; ok {
		p.used -= r.bytes
		p.sessions[r.id] -= r.bytes
		delete(p.frames, hash)
	}
}
func (p *Pending) Usage() int64 { p.mu.Lock(); defer p.mu.Unlock(); return p.used }
