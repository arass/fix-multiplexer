package testfix

import (
	"context"
	"sync"
	"time"

	"github.com/quickfixgo/quickfix"
)

// Fault injection may inspect/change sequence state from the test goroutine.
// Serialize that access with the peer engine; never patch the engine itself.
type lockedFactory struct{ base quickfix.MessageStoreFactory }

func (f lockedFactory) Create(id quickfix.SessionID) (quickfix.MessageStore, error) {
	base := f.base
	if base == nil {
		base = quickfix.NewMemoryStoreFactory()
	}
	s, err := base.Create(id)
	if err != nil {
		return nil, err
	}
	return &lockedStore{MessageStore: s, changed: make(chan struct{})}, nil
}

type lockedStore struct {
	quickfix.MessageStore
	mu      sync.Mutex
	changed chan struct{}
}

func (s *lockedStore) NextSenderMsgSeqNum() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.NextSenderMsgSeqNum()
}
func (s *lockedStore) NextTargetMsgSeqNum() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.NextTargetMsgSeqNum()
}
func (s *lockedStore) CreationTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.CreationTime()
}
func (s *lockedStore) SetCreationTime(v time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MessageStore.SetCreationTime(v)
}
func (s *lockedStore) IncrNextSenderMsgSeqNum() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.IncrNextSenderMsgSeqNum()
}
func (s *lockedStore) IncrNextTargetMsgSeqNum() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.MessageStore.IncrNextTargetMsgSeqNum()
	s.notify()
	return err
}
func (s *lockedStore) SetNextSenderMsgSeqNum(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.SetNextSenderMsgSeqNum(n)
}
func (s *lockedStore) SetNextTargetMsgSeqNum(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.MessageStore.SetNextTargetMsgSeqNum(n)
	s.notify()
	return err
}

func (s *lockedStore) notify() { close(s.changed); s.changed = make(chan struct{}) }
func WaitTarget(ctx context.Context, id quickfix.SessionID, expected int) error {
	base, err := quickfix.GetMessageStore(id)
	if err != nil {
		return err
	}
	s := base.(*lockedStore)
	for {
		s.mu.Lock()
		next, ch := s.MessageStore.NextTargetMsgSeqNum(), s.changed
		s.mu.Unlock()
		if next >= expected {
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *lockedStore) SaveMessage(n int, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.SaveMessage(n, b)
}
func (s *lockedStore) SaveMessageAndIncrNextSenderMsgSeqNum(n int, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.SaveMessageAndIncrNextSenderMsgSeqNum(n, b)
}
func (s *lockedStore) GetMessages(begin, end int) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.GetMessages(begin, end)
}
func (s *lockedStore) IterateMessages(begin, end int, cb func([]byte) error) error {
	messages, err := s.GetMessages(begin, end)
	if err != nil {
		return err
	}
	for _, b := range messages {
		if err = cb(b); err != nil {
			return err
		}
	}
	return nil
}
func (s *lockedStore) Reset() error { s.mu.Lock(); defer s.mu.Unlock(); return s.MessageStore.Reset() }
func (s *lockedStore) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.Refresh()
}
func (s *lockedStore) Close() error { s.mu.Lock(); defer s.mu.Unlock(); return s.MessageStore.Close() }
