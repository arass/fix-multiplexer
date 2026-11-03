package fixadapter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strconv"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/transport"
	"github.com/quickfixgo/quickfix"
)

func (r *Runtime) prepareUpstreamTransport(c config.Config, settings *quickfix.Settings) error {
	var secure *tls.Config
	if c.Upstream.TLSEnabled {
		secure = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.Upstream.TLS.ServerName}
		if secure.ServerName == "" {
			secure.ServerName = c.Upstream.Host
		}
		if path := c.Upstream.TLS.CAFile; path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return errors.New("cannot read upstream TLS CA")
			}
			secure.RootCAs = x509.NewCertPool()
			if !secure.RootCAs.AppendCertsFromPEM(data) {
				return errors.New("invalid upstream TLS CA")
			}
		}
		if c.Upstream.TLS.CertFile != "" {
			cert, err := tls.LoadX509KeyPair(c.Upstream.TLS.CertFile, c.Upstream.TLS.KeyFile)
			if err != nil {
				return errors.New("cannot load upstream TLS client identity")
			}
			secure.Certificates = []tls.Certificate{cert}
		}
	}
	privateTLS, err := privateBridgeTLS(settings)
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.New("cannot bind private upstream transport")
	}
	r.UpstreamTransport = transport.New(1, c.Service.MaxMessageBytes, func() { r.Observer.EngineError("upstream_transport") })
	dial := func(ctx context.Context) (net.Conn, error) {
		d := &net.Dialer{Timeout: 5 * time.Second}
		address := net.JoinHostPort(c.Upstream.Host, strconv.Itoa(c.Upstream.Port))
		if secure != nil {
			return (&tls.Dialer{NetDialer: d, Config: secure}).DialContext(ctx, "tcp", address)
		}
		return d.DialContext(ctx, "tcp", address)
	}
	r.bridge = transport.NewBridge(tls.NewListener(l, privateTLS), r.UpstreamTransport, r.Application.Upstream.String(), dial)
	host, port, _ := net.SplitHostPort(l.Addr().String())
	// The engine authenticates the private loopback bridge separately from the
	// bridge's independently verified TLS connection to the counterparty.
	settings.GlobalSettings().Set("SocketConnectHost", host)
	settings.GlobalSettings().Set("SocketConnectPort", port)
	return nil
}
