// Package testfix contains synthetic local FIX peers for tests and the smoke demo.
// It never contacts an external venue and never logs FIX payloads.
package testfix

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"

	"github.com/quickfixgo/quickfix"
)

type Peer struct {
	Logons     chan quickfix.SessionID
	Logouts    chan quickfix.SessionID
	Messages   chan *quickfix.Message
	Admin      chan *quickfix.Message
	BeforeSend func(*quickfix.Message, quickfix.SessionID) error
}

func NewPeer() *Peer {
	return &Peer{Logons: make(chan quickfix.SessionID, 128), Logouts: make(chan quickfix.SessionID, 128), Messages: make(chan *quickfix.Message, 1024), Admin: make(chan *quickfix.Message, 1024)}
}
func (p *Peer) OnCreate(quickfix.SessionID)                   {}
func (p *Peer) OnLogon(id quickfix.SessionID)                 { p.Logons <- id }
func (p *Peer) OnLogout(id quickfix.SessionID)                { p.Logouts <- id }
func (p *Peer) ToAdmin(*quickfix.Message, quickfix.SessionID) {}
func (p *Peer) ToApp(m *quickfix.Message, id quickfix.SessionID) error {
	if p.BeforeSend != nil {
		return p.BeforeSend(m, id)
	}
	return nil
}
func (p *Peer) FromAdmin(m *quickfix.Message, _ quickfix.SessionID) quickfix.MessageRejectError {
	select {
	case p.Admin <- m:
	default:
	}
	return nil
}
func (p *Peer) FromApp(m *quickfix.Message, _ quickfix.SessionID) quickfix.MessageRejectError {
	p.Messages <- m
	return nil
}

func Settings(sender, target, address, dictionary string, initiator bool) (*quickfix.Settings, quickfix.SessionID, error) {
	s := quickfix.NewSettings()
	g := s.GlobalSettings()
	for k, v := range map[string]string{"BeginString": "FIX.4.4", "SenderCompID": sender, "TargetCompID": target, "HeartBtInt": "30", "ReconnectInterval": "1", "LogonTimeout": "5", "LogoutTimeout": "1", "ResetOnLogon": "N", "ResetOnLogout": "N", "ResetOnDisconnect": "N", "ValidateUserDefinedFields": "N", "DataDictionary": dictionary} {
		g.Set(k, v)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, quickfix.SessionID{}, err
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, quickfix.SessionID{}, err
	}
	if initiator {
		g.Set("SocketConnectHost", host)
		g.Set("SocketConnectPort", port)
	} else {
		g.Set("SocketAcceptHost", host)
		g.Set("SocketAcceptPort", port)
	}
	id, err := s.AddSession(quickfix.NewSessionSettings())
	return s, id, err
}

func Listen(p *Peer, l net.Listener, s *quickfix.Settings) (*quickfix.Acceptor, error) {
	a, err := quickfix.NewAcceptor(p, lockedFactory{}, s, quickfix.NewNullLogFactory())
	if err != nil {
		return nil, err
	}
	a.SetNewListenerCallback(func(string, *tls.Config) (net.Listener, error) { return l, nil })
	if err = a.Start(); err != nil {
		return nil, err
	}
	return a, nil
}

func Connect(p *Peer, s *quickfix.Settings) (*quickfix.Initiator, error) {
	i, err := quickfix.NewInitiator(p, lockedFactory{}, s, quickfix.NewNullLogFactory())
	if err != nil {
		return nil, err
	}
	if err = i.Start(); err != nil {
		return nil, err
	}
	return i, nil
}

func Receive[T any](ctx context.Context, ch <-chan T) (T, error) {
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		var v T
		return v, errors.New("local FIX scenario timed out")
	}
}

func Order(id string) *quickfix.Message {
	m := quickfix.NewMessage()
	m.Header.SetString(35, "D")
	for tag, value := range map[quickfix.Tag]string{11: id, 21: "1", 55: "TEST", 54: "1", 60: "20260101-12:00:00.000", 38: "1", 40: "2", 44: "10", 59: "0"} {
		m.Body.SetString(tag, value)
	}
	return m
}

func Report(id string) *quickfix.Message {
	m := quickfix.NewMessage()
	m.Header.SetString(35, "8")
	for tag, value := range map[quickfix.Tag]string{11: id, 37: "mock-order", 17: "mock-exec", 150: "0", 39: "0", 55: "TEST", 54: "1", 151: "1", 14: "0", 6: "0"} {
		m.Body.SetString(tag, value)
	}
	return m
}
