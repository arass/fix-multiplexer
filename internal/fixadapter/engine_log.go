package fixadapter

import (
	"strings"

	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
)

// Engine event formats sometimes interpolate raw frames or credentials. Never
// format them or emit their arguments; report a generic operational error only.
type logFactory struct {
	recorder *observability.Recorder
	pool     *transport.Pool
	pending  *transport.Pending
}

func (f logFactory) Create() (quickfix.Log, error) {
	return &engineLog{recorder: f.recorder, pool: f.pool, pending: f.pending}, nil
}
func (f logFactory) CreateSessionLog(id quickfix.SessionID) (quickfix.Log, error) {
	return &engineLog{recorder: f.recorder, id: id.String(), pool: f.pool, pending: f.pending}, nil
}

type engineLog struct {
	recorder *observability.Recorder
	id       string
	pool     *transport.Pool
	pending  *transport.Pending
}

func (l *engineLog) OnIncoming(b []byte) {
	if l.pool != nil {
		l.pool.Consume(l.id, len(b))
	}
}
func (l *engineLog) OnOutgoing(b []byte) {
	if l.pending != nil {
		l.pending.Sent(b)
	}
}
func (l *engineLog) OnEvent(format string)                    { l.event(format) }
func (l *engineLog) OnEventf(format string, _ ...interface{}) { l.event(format) }
func (l *engineLog) event(format string) {
	for _, prefix := range []string{"Invalid", "Msg Parse Error", "Resend Msg Parse Error", "error", "Error", "Failed", "Connection Terminated", "Message Rejected"} {
		if strings.HasPrefix(format, prefix) {
			l.recorder.EngineError(l.id)
			return
		}
	}
}
