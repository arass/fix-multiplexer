package fixadapter

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/gateway"
	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/retention"
	"fix-multiplexer/internal/sessions"
	"fix-multiplexer/internal/transport"
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
	Stores                                   *retention.Manager
	Observer                                 *observability.Recorder
	Pending                                  *transport.Pending
}

func (a *Application) OnCreate(id quickfix.SessionID) { a.created = append(a.created, id) }
func (a *Application) OnLogon(id quickfix.SessionID) {
	if a.Stores != nil && (!a.Stores.Healthy() || a.Stores.Connected(id.String(), true) != nil) {
		_ = a.Registry.Disable(id.String())
		if a.Observer != nil {
			a.Observer.EngineError(id.String())
		}
		return
	}
	_ = a.Registry.SetLoggedOn(id.String(), true)
	if a.Observer != nil {
		a.Observer.Session(id.String(), true)
	}
}
func (a *Application) OnLogout(id quickfix.SessionID) {
	if a.Stores != nil {
		_ = a.Stores.Connected(id.String(), false)
	}
	_ = a.Registry.SetLoggedOn(id.String(), false)
	if a.Observer != nil {
		a.Observer.Session(id.String(), false)
	}
}
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
func (a *Application) ToApp(m *quickfix.Message, id quickfix.SessionID) error {
	dup, _ := m.Header.GetBool(43)
	if a.Observer != nil {
		if dup {
			a.Observer.Replay.Add(1)
		}
		a.record(m, id, "outbound", "prepared")
	}
	// Engine resends synchronously wait for the writer; never reject them for
	// queue credit (the engine would gap-fill a rejected replay).
	if !dup && a.Pending != nil {
		return a.Pending.Reserve(id.String(), m.Bytes())
	}
	return nil
}
func (a *Application) FromAdmin(m *quickfix.Message, id quickfix.SessionID) quickfix.MessageRejectError {
	if len(m.Bytes()) > a.MaxMessageBytes {
		return quickfix.NewMessageRejectError("gateway: message exceeds limit", 5, nil)
	}
	if m.IsMsgTypeOf("A") && a.Stores != nil && !a.Stores.CanUse(id.String()) {
		return quickfix.RejectLogon{Text: "gateway: store unavailable or session retired"}
	}
	if id == a.Upstream {
		if m.IsMsgTypeOf("3") && a.Router != nil {
			notice := newUpstreamRejectNotice(m)
			_ = a.Router.BroadcastNotice(notice)
			a.record(m, id, "upstream", "reject_notice_attempted")
		}
		return nil
	}
	if !m.IsMsgTypeOf("A") {
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
			if a.Observer != nil {
				a.Observer.AuthenticationFailure(id.String())
			}
			return quickfix.RejectLogon{Text: "gateway: authentication failed"}
		}
	}
	return nil
}

// The Reject belongs to the upstream session. A new BusinessMessageReject
// describes that Reject rather than asserting that any particular downstream
// message was rejected. Tag 45 is deliberately absent from the new message.
type upstreamRejectNotice struct{ text string }

func newUpstreamRejectNotice(m *quickfix.Message) upstreamRejectNotice {
	text := "gateway: upstream session Reject (35=3)"
	for _, field := range []struct {
		tag  quickfix.Tag
		name string
	}{{45, "upstream RefSeqNum"}, {372, "upstream RefMsgType"}, {371, "upstream RefTagID"}, {373, "upstream SessionRejectReason"}, {58, "exchange Text"}} {
		if value, err := m.Body.GetString(field.tag); err == nil && value != "" {
			// Limit untrusted counterparty text and remove delimiter/control bytes.
			var cleaned strings.Builder
			for _, b := range []byte(value) {
				if cleaned.Len() >= 512 {
					break
				}
				if b < 32 || b == 127 {
					b = ' '
				}
				cleaned.WriteByte(b)
			}
			text += fmt.Sprintf("; %s=%s", field.name, cleaned.String())
		}
	}
	return upstreamRejectNotice{text: text}
}
func (n upstreamRejectNotice) Type() string { return "j" }
func (n upstreamRejectNotice) Size() int    { return len(n.text) + 256 }
func (n upstreamRejectNotice) Clone() (gateway.Message, error) {
	m := quickfix.NewMessage()
	m.Header.SetString(35, "j")
	m.Body.SetString(372, "3") // The exchange's session Reject is the referenced message.
	m.Body.SetInt(380, 0)      // Other; no downstream business rejection reason is claimed.
	m.Body.SetString(58, n.text)
	return &Message{out: m}, nil
}
func (a *Application) FromApp(m *quickfix.Message, id quickfix.SessionID) quickfix.MessageRejectError {
	if a.Stores != nil && !a.Stores.Healthy() {
		return reject(m, "gateway: durable store unhealthy", 4)
	}
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
		err := a.Router.Broadcast(msg)
		outcome := "fanout_enqueued"
		if err != nil {
			outcome = "policy_drop"
		}
		a.record(m, id, "upstream", outcome)
		return nil
	}
	if err = a.Router.Submit(id.String(), msg); err != nil {
		a.record(m, id, "downstream", "forward_failed")
		reason := 4
		if errors.Is(err, gateway.ErrPolicy) {
			reason = 3
		} else if errors.Is(err, gateway.ErrReadOnly) {
			reason = 0
		}
		return reject(m, err.Error(), reason)
	}
	a.record(m, id, "downstream", "queued_upstream")
	return nil
}

func (a *Application) record(m *quickfix.Message, id quickfix.SessionID, direction, outcome string) {
	if a.Observer == nil {
		return
	}
	typ, _ := m.MsgType()
	seq, _ := m.Header.GetInt(34)
	a.Observer.Application(direction, id.String(), typ, seq, outcome)
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
		if len(value) > 4096 || strings.ContainsAny(value, "\x00\x01\r\n") {
			return "", errors.New("credential environment reference invalid")
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
