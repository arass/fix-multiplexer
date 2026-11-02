package fixadapter

import (
	"crypto/subtle"
	"errors"
	"io"
	"os"
	"strings"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/sessions"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/datadictionary"
)

type Application struct {
	Registry                                 *sessions.Registry
	Router                                   *gateway.Router
	Upstream                                 quickfix.SessionID
	UpstreamDictionary, DownstreamDictionary *datadictionary.DataDictionary
	MaxMessageBytes                          int
	Username, Password                       string
	ClientPasswords                          map[string]string
	created                                  []quickfix.SessionID
}

func (a *Application) OnCreate(id quickfix.SessionID) { a.created = append(a.created, id) }
func (a *Application) OnLogon(id quickfix.SessionID)  { _ = a.Registry.SetLoggedOn(id.String(), true) }
func (a *Application) OnLogout(id quickfix.SessionID) { _ = a.Registry.SetLoggedOn(id.String(), false) }
func (a *Application) ToAdmin(m *quickfix.Message, id quickfix.SessionID) {
	if id == a.Upstream && m.IsMsgTypeOf("A") {
		if a.Username != "" {
			m.Body.SetString(553, a.Username)
		}
		if a.Password != "" {
			m.Body.SetString(554, a.Password)
		}
	}
}

// ToApp is invoked for outbound replay too. It never routes any message.
func (a *Application) ToApp(*quickfix.Message, quickfix.SessionID) error { return nil }
func (a *Application) FromAdmin(m *quickfix.Message, id quickfix.SessionID) quickfix.MessageRejectError {
	if len(m.Bytes()) > a.MaxMessageBytes {
		return quickfix.NewMessageRejectError("gateway: message exceeds limit", 5, nil)
	}
	if id == a.Upstream || !m.IsMsgTypeOf("A") {
		return nil
	}
	s, ok := a.Registry.Get(id.String())
	if !ok || !s.Enabled {
		return quickfix.RejectLogon{Text: "gateway: client not authorized"}
	}
	if m.Body.Has(141) {
		reset, _ := m.Body.GetBool(141)
		if reset {
			return quickfix.RejectLogon{Text: "gateway: coordinate reset while stopped"}
		}
	}
	if expected := a.ClientPasswords[id.String()]; expected != "" {
		actual, err := m.Body.GetString(554)
		if err != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
			return quickfix.RejectLogon{Text: "gateway: authentication failed"}
		}
	}
	return nil
}
func (a *Application) FromApp(m *quickfix.Message, id quickfix.SessionID) quickfix.MessageRejectError {
	if len(m.Bytes()) > a.MaxMessageBytes {
		return reject(m, "gateway: message exceeds limit", 5)
	}
	dict := a.DownstreamDictionary
	if id == a.Upstream {
		dict = a.UpstreamDictionary
	}
	msg, err := Snapshot(m, dict)
	if err != nil {
		return reject(m, "gateway: unsupported application message", 3)
	}
	if id == a.Upstream {
		_ = a.Router.Broadcast(msg)
		return nil
	}
	if err = a.Router.Submit(id.String(), msg); err != nil {
		return reject(m, err.Error(), 4)
	}
	return nil
}
func reject(m *quickfix.Message, text string, reason int) quickfix.MessageRejectError {
	ref, _ := m.Body.GetString(11)
	return quickfix.NewBusinessMessageRejectErrorWithRefID(text, reason, ref, nil)
}

func ResolveSecret(s config.Secret) (string, error) {
	if s.Env != "" {
		value, ok := os.LookupEnv(s.Env)
		if !ok || value == "" {
			return "", errors.New("credential environment reference unavailable")
		}
		if strings.ContainsAny(value, "\x00\x01\r\n") {
			return "", errors.New("credential contains forbidden control bytes")
		}
		return value, nil
	}
	if s.File != "" {
		f, err := os.Open(s.File)
		if err != nil {
			return "", errors.New("credential file unavailable")
		}
		defer f.Close()
		buf := make([]byte, 4097)
		n, err := io.ReadFull(f, buf)
		if (err != nil && err != io.EOF && err != io.ErrUnexpectedEOF) || n > 4096 {
			return "", errors.New("credential file invalid")
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(buf[:n]), "\n"), "\r")
		if value == "" || strings.ContainsAny(value, "\x00\x01\r\n") {
			return "", errors.New("credential file invalid")
		}
		return value, nil
	}
	return "", nil
}
