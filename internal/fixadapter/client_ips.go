package fixadapter

import (
	"errors"
	"net"
	"net/netip"

	"fix-multiplexer/internal/observability"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
)

type connectionValidator struct {
	pool       *transport.Pool
	allowedIPs map[string][]netip.Prefix
	observer   *observability.Recorder
}

// QuickFIX/Go supplies the socket and claimed session before binding it or
// processing its Logon. Never take the source address from FIX message fields.
func (v connectionValidator) Validate(c net.Conn, id quickfix.SessionID) error {
	prefixes, known := v.allowedIPs[id.String()]
	if !known {
		return errors.New("gateway: client not authorized")
	}
	if prefixes != nil {
		var source netip.Addr
		if remote, ok := c.RemoteAddr().(*net.TCPAddr); ok && remote != nil {
			source = remote.AddrPort().Addr().Unmap().WithZone("")
		}
		allowed := false
		for _, prefix := range prefixes {
			if prefix.Contains(source) {
				allowed = true
				break
			}
		}
		if !allowed {
			if v.observer != nil {
				v.observer.IPDenied(id.String(), source)
			}
			return errors.New("gateway: source IP not allowed")
		}
	}
	return v.pool.Bind(id.String(), c)
}
