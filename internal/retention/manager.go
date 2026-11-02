// Package retention bounds engine-owned store files without implementing FIX
// sequencing or removing replay history from a live session.
package retention

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrStorage = errors.New("gateway: durable store unavailable or budget exhausted")
	ErrRetired = errors.New("gateway: session retired; coordinate reset/reprovision while stopped")
	ErrExpired = errors.New("gateway: requested replay exceeds configured history window; coordinate reset")
)

type State struct {
	Active         bool      `json:"active"`
	LastDisconnect time.Time `json:"last_disconnect"`
	Retired        bool      `json:"retired"`
}
type entry struct {
	root, path string
	state      State
}
type Manager struct {
	mu                  sync.Mutex
	entries             map[string]*entry
	roots               []string
	now                 func() time.Time
	limit, sessionLimit int64
	failed              atomic.Bool
	closed              bool
	maintenance         bool
	CleanupCount        atomic.Uint64
}

func OpenMaintenance(roots []string, limit, sessionLimit int64, now func() time.Time) (*Manager, error) {
	return open(roots, limit, sessionLimit, now, true)
}

// Open acquires exclusive root leases, also used by offline maintenance. A crash
// leaves a lease behind: an operator must verify the process is stopped before
// removing .gateway-lock. Never automatically guess that a lease is stale.
func Open(roots []string, limit, sessionLimit int64, now func() time.Time) (*Manager, error) {
	return open(roots, limit, sessionLimit, now, false)
}
func open(roots []string, limit, sessionLimit int64, now func() time.Time, maintenance bool) (*Manager, error) {
	m := &Manager{entries: make(map[string]*entry), now: now, limit: limit, sessionLimit: sessionLimit, maintenance: maintenance}
	for _, root := range roots {
		path, err := filepath.Abs(root)
		if err != nil {
			m.Close()
			return nil, ErrStorage
		}
		if err = os.MkdirAll(path, 0700); err != nil {
			m.Close()
			return nil, ErrStorage
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			m.Close()
			return nil, ErrStorage
		}
		if err = os.Mkdir(filepath.Join(path, ".gateway-lock"), 0700); err != nil {
			m.Close()
			return nil, ErrStorage
		}
		m.roots = append(m.roots, path)
	}
	if _, err := m.usage(); err != nil {
		m.Close()
		return nil, ErrStorage
	}
	return m, nil
}

func key(id string) string { sum := sha256.Sum256([]byte(id)); return hex.EncodeToString(sum[:16]) }

func (m *Manager) Register(id, root string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", ErrStorage
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", ErrStorage
	}
	owned := false
	for _, r := range m.roots {
		if abs == r {
			owned = true
		}
	}
	if !owned {
		return "", ErrStorage
	}
	if e, ok := m.entries[id]; ok {
		return e.path, nil
	}
	e := &entry{root: abs, path: filepath.Join(abs, key(id)), state: State{LastDisconnect: m.now()}}
	meta := filepath.Join(abs, key(id)+".json")
	data, err := os.ReadFile(meta)
	if err == nil {
		if len(data) > 4096 || json.Unmarshal(data, &e.state) != nil {
			return "", ErrStorage
		}
	} else if !os.IsNotExist(err) {
		return "", ErrStorage
	}
	if e.state.Retired {
		m.entries[id] = e
		return "", ErrRetired
	}
	if err = os.MkdirAll(e.path, 0700); err != nil {
		return "", ErrStorage
	}
	if err = m.save(e); err != nil {
		return "", ErrStorage
	}
	m.entries[id] = e
	return e.path, nil
}

func (m *Manager) Connected(id string, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || m.closed || e.state.Retired {
		return ErrStorage
	}
	e.state.Active = active
	if !active {
		e.state.LastDisconnect = m.now()
	}
	if err := m.save(e); err != nil {
		m.failed.Store(true)
		return ErrStorage
	}
	return nil
}

func (m *Manager) CanUse(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	return !m.closed && ok && !e.state.Retired && !m.failed.Load()
}
func (m *Manager) Healthy() bool { return !m.failed.Load() }
func (m *Manager) Fail()         { m.failed.Store(true) }

// Write accounts for body/index/sequence overhead before delegating to the engine.
// Store errors are latched until restart; replay-required bytes are never pruned.
func (m *Manager) Write(id string, extra int64, write func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || m.closed || m.failed.Load() || e.state.Retired {
		return ErrStorage
	}
	total, err := m.usage()
	if err != nil {
		m.failed.Store(true)
		return ErrStorage
	}
	own, err := inventory(e.path)
	if err != nil || extra < 0 || extra > m.limit-total || extra > m.sessionLimit-own {
		m.failed.Store(true)
		return ErrStorage
	}
	if err = write(); err != nil {
		m.failed.Store(true)
		return ErrStorage
	}
	return nil
}

// Expire prevents subsequent logons but does not delete any open store files.
func (m *Manager) Expire(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || m.closed {
		return ErrStorage
	}
	e.state.Retired = true
	if err := m.save(e); err != nil {
		m.failed.Store(true)
		return ErrStorage
	}
	return ErrExpired
}

// Prune requires exclusive offline leases and only removes an idle, inactive
// session. The retirement marker lives outside the removed store directory.
func (m *Manager) Prune(id string, ttl time.Duration, dryRun bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || m.closed || !m.maintenance {
		return false, ErrStorage
	}
	if e.state.Active || m.now().Sub(e.state.LastDisconnect) < ttl {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	e.state.Retired = true
	if err := m.save(e); err != nil {
		return false, ErrStorage
	}
	if _, err := inventory(e.path); err != nil {
		return false, ErrStorage
	}
	// e.path is derived from an owned absolute root and a hash, never user input.
	if err := os.RemoveAll(e.path); err != nil {
		return false, ErrStorage
	}
	m.CleanupCount.Add(1)
	return true, nil
}

// Reset is an explicit offline reprovisioning action. The peer must also agree
// to restart at sequence 1. The exclusive lease proves no gateway owns the root.
func (m *Manager) Reset(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || m.closed || !m.maintenance {
		return ErrStorage
	}
	e.state.Retired = true
	if err := m.save(e); err != nil {
		return ErrStorage
	}
	if _, err := inventory(e.path); err != nil {
		return ErrStorage
	}
	if err := os.RemoveAll(e.path); err != nil {
		return ErrStorage
	}
	e.state = State{LastDisconnect: m.now()}
	if err := m.save(e); err != nil {
		return ErrStorage
	}
	return nil
}

// LoadExisting inventories a provisioned client without creating session state.
func (m *Manager) LoadExisting(id, root string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.maintenance {
		return false, ErrStorage
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return false, ErrStorage
	}
	owned := false
	for _, r := range m.roots {
		if abs == r {
			owned = true
		}
	}
	if !owned {
		return false, ErrStorage
	}
	data, err := os.ReadFile(filepath.Join(abs, key(id)+".json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || len(data) > 4096 {
		return false, ErrStorage
	}
	e := &entry{root: abs, path: filepath.Join(abs, key(id))}
	if json.Unmarshal(data, &e.state) != nil {
		return false, ErrStorage
	}
	m.entries[id] = e
	return true, nil
}
func (m *Manager) State(id string) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return State{}, false
	}
	return e.state, true
}

func (m *Manager) Usage() (int64, error) { m.mu.Lock(); defer m.mu.Unlock(); return m.usage() }
func (m *Manager) usage() (int64, error) {
	var total int64
	for _, r := range m.roots {
		n, err := inventory(r)
		if err != nil {
			return 0, err
		}
		total += n
	}
	if total > m.limit && !m.maintenance {
		return total, ErrStorage
	}
	return total, nil
}
func inventory(root string) (int64, error) {
	var n int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrStorage
		}
		if !d.IsDir() {
			if !d.Type().IsRegular() {
				return ErrStorage
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			n += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return n, err
}

func (m *Manager) save(e *entry) error {
	data, err := json.Marshal(e.state)
	if err != nil {
		return err
	}
	path := filepath.Join(e.root, filepath.Base(e.path)+".json")
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	var result error
	for _, r := range m.roots {
		if err := os.Remove(filepath.Join(r, ".gateway-lock")); err != nil {
			result = ErrStorage
		}
	}
	return result
}
