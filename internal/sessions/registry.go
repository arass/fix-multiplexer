// Package sessions maintains allowlisted connectivity independently of a FIX engine.
package sessions

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("session is not authorized and logged on")

type State struct {
	ID             string
	Enabled        bool
	LoggedOn       bool
	Generation     uint64
	LastDisconnect time.Time
}

type Registry struct {
	mu      sync.RWMutex
	states  map[string]State
	now     func() time.Time
	changed chan struct{}
}

func New(enabled map[string]bool, now func() time.Time) *Registry {
	r := &Registry{states: make(map[string]State), now: now, changed: make(chan struct{})}
	for id, on := range enabled {
		r.states[id] = State{ID: id, Enabled: on, LastDisconnect: now()}
	}
	return r
}

func (r *Registry) SetLoggedOn(id string, loggedOn bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[id]
	if !ok || (loggedOn && !s.Enabled) {
		return ErrUnavailable
	}
	if s.LoggedOn != loggedOn {
		s.Generation++
		s.LoggedOn = loggedOn
		if !loggedOn {
			s.LastDisconnect = r.now()
		}
		r.states[id] = s
		r.notify()
	}
	return nil
}

func (r *Registry) Disable(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[id]
	if !ok {
		return ErrUnavailable
	}
	if s.Enabled {
		s.Enabled = false
		s.Generation++
		r.states[id] = s
		r.notify()
	}
	return nil
}

func (r *Registry) Get(id string) (State, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.states[id]
	return s, ok
}

func (r *Registry) Live(id string, generation uint64) bool {
	s, ok := r.Get(id)
	return ok && s.Enabled && s.LoggedOn && s.Generation == generation
}

func (r *Registry) Snapshot() []State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]State, 0, len(r.states))
	for _, s := range r.states {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Watch atomically captures connectivity and a notification channel, avoiding polling.
func (r *Registry) Watch() ([]State, <-chan struct{}) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]State, 0, len(r.states))
	for _, s := range r.states {
		out = append(out, s)
	}
	return out, r.changed
}

func (r *Registry) notify() { close(r.changed); r.changed = make(chan struct{}) }
