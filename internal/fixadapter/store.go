package fixadapter

import (
	"bytes"
	"errors"
	"sync"
	"time"

	"fix-multiplexer/internal/retention"
	"github.com/quickfixgo/quickfix"
)

// guardedStore delegates all sequencing/persistence to the engine's file store.
// It only enforces resource budgets and the replay-age policy.
type guardedStore struct {
	quickfix.MessageStore
	manager *retention.Manager
	id      string
	window  time.Duration
	now     func() time.Time
	mu      sync.Mutex
}

func (s *guardedStore) write(extra int64, f func() error) error {
	return s.manager.Write(s.id, extra, func() error { s.mu.Lock(); defer s.mu.Unlock(); return f() })
}

func (s *guardedStore) NextSenderMsgSeqNum() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.NextSenderMsgSeqNum()
}
func (s *guardedStore) NextTargetMsgSeqNum() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MessageStore.NextTargetMsgSeqNum()
}
func (s *guardedStore) SaveMessage(n int, b []byte) error {
	stored, err := s.storageBytes(b)
	if err != nil {
		return err
	}
	return s.write(int64(len(stored)+128), func() error { return s.MessageStore.SaveMessage(n, stored) })
}
func (s *guardedStore) SaveMessageAndIncrNextSenderMsgSeqNum(n int, b []byte) error {
	stored, err := s.storageBytes(b)
	if err != nil {
		return err
	}
	return s.write(int64(len(stored)+160), func() error { return s.MessageStore.SaveMessageAndIncrNextSenderMsgSeqNum(n, stored) })
}

// QuickFIX/Go gap-fills administrative messages during resend. Retain only
// Logon framing and sequence metadata in its store; the original bytes still
// go to the counterparty unchanged. Application replay bytes are untouched.
func (s *guardedStore) storageBytes(b []byte) ([]byte, error) {
	m := quickfix.NewMessage()
	if err := quickfix.ParseMessage(m, bytes.NewBuffer(b)); err != nil {
		s.manager.Fail()
		return nil, retention.ErrStorage
	}
	typ, err := m.MsgType()
	if err != nil {
		s.manager.Fail()
		return nil, retention.ErrStorage
	}
	if typ != "A" {
		return b, nil
	}
	out := quickfix.NewMessage()
	for _, tag := range []quickfix.Tag{8, 35, 34, 49, 56, 52} {
		value, err := m.Header.GetBytes(tag)
		if err != nil {
			s.manager.Fail()
			return nil, retention.ErrStorage
		}
		out.Header.SetBytes(tag, append([]byte(nil), value...))
	}
	return out.Bytes(), nil
}
func (s *guardedStore) IncrNextSenderMsgSeqNum() error {
	return s.write(32, s.MessageStore.IncrNextSenderMsgSeqNum)
}
func (s *guardedStore) IncrNextTargetMsgSeqNum() error {
	return s.write(32, s.MessageStore.IncrNextTargetMsgSeqNum)
}
func (s *guardedStore) SetNextSenderMsgSeqNum(n int) error {
	return s.write(32, func() error { return s.MessageStore.SetNextSenderMsgSeqNum(n) })
}
func (s *guardedStore) SetNextTargetMsgSeqNum(n int) error {
	return s.write(32, func() error { return s.MessageStore.SetNextTargetMsgSeqNum(n) })
}

// The daemon does not allow unsolicited sequence resets. Offline reset destroys
// state only under an exclusive maintenance lease, with explicit peer agreement.
func (s *guardedStore) Reset() error { return retention.ErrRetired }
func (s *guardedStore) Refresh() error {
	if !s.manager.CanUse(s.id) {
		return retention.ErrStorage
	}
	if err := s.MessageStore.Refresh(); err != nil {
		s.manager.Fail()
		return retention.ErrStorage
	}
	return nil
}
func (s *guardedStore) IterateMessages(begin, end int, cb func([]byte) error) error {
	if !s.manager.CanUse(s.id) {
		return retention.ErrStorage
	}
	err := s.MessageStore.IterateMessages(begin, end, func(b []byte) error {
		if s.window > 0 {
			m := quickfix.NewMessage()
			if err := quickfix.ParseMessage(m, bytes.NewBuffer(b)); err != nil {
				s.manager.Fail()
				return retention.ErrStorage
			}
			if typ, e := m.MsgType(); e == nil && !isAdmin(typ) {
				stamp, e := m.Header.GetTime(52)
				if e != nil {
					s.manager.Fail()
					return retention.ErrStorage
				}
				if s.now().Sub(stamp) > s.window {
					return s.manager.Expire(s.id)
				}
			}
		}
		return cb(b)
	})
	if err != nil && !errors.Is(err, retention.ErrExpired) {
		s.manager.Fail()
		return retention.ErrStorage
	}
	return err
}
func (s *guardedStore) GetMessages(begin, end int) ([][]byte, error) {
	var out [][]byte
	err := s.IterateMessages(begin, end, func(b []byte) error { out = append(out, b); return nil })
	return out, err
}
func isAdmin(t string) bool {
	switch t {
	case "0", "1", "2", "3", "4", "5", "A":
		return true
	}
	return false
}
