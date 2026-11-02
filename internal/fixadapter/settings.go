package fixadapter

import (
	"net"
	"strconv"

	"fix-multiplexer/internal/config"
	"github.com/quickfixgo/quickfix"
)

// Settings builds typed engine settings, never interpolating credentials into INI.
func Settings(c config.Config) (*quickfix.Settings, *quickfix.Settings, error) {
	u := baseSettings()
	d := baseSettings()
	u.GlobalSettings().Set("SocketConnectHost", c.Upstream.Host)
	u.GlobalSettings().Set("SocketConnectPort", strconv.Itoa(c.Upstream.Port))
	u.GlobalSettings().Set("FileStorePath", c.Upstream.StoreDir)
	d.GlobalSettings().Set("FileStorePath", c.Downstream.StoreDir)
	host, port, _ := net.SplitHostPort(c.Downstream.Listen)
	d.GlobalSettings().Set("SocketAcceptHost", host)
	d.GlobalSettings().Set("SocketAcceptPort", port)
	for _, p := range []struct {
		s    *quickfix.Settings
		path string
	}{{u, c.Upstream.Dictionary}, {d, c.Downstream.Dictionary}} {
		path := p.path
		if path == "" {
			path = "spec/FIX44.xml"
		}
		p.s.GlobalSettings().Set("DataDictionary", path)
	}
	setTLS(u.GlobalSettings(), c.Upstream.TLSEnabled, c.Upstream.TLS)
	setTLS(d.GlobalSettings(), c.Downstream.TLSEnabled, c.Downstream.TLS)
	if _, err := u.AddSession(sessionSettings(c.Upstream.Sender, c.Upstream.Target)); err != nil {
		return nil, nil, err
	}
	for _, cl := range c.Downstream.Clients {
		if cl.Disabled {
			continue
		}
		if _, err := d.AddSession(sessionSettings(c.Downstream.Target, cl.Sender)); err != nil {
			return nil, nil, err
		}
	}
	return u, d, nil
}

func baseSettings() *quickfix.Settings {
	s := quickfix.NewSettings()
	g := s.GlobalSettings()
	for k, v := range map[string]string{"BeginString": "FIX.4.4", "HeartBtInt": "30", "ReconnectInterval": "1", "LogonTimeout": "5", "LogoutTimeout": "1", "ResetOnLogon": "N", "ResetOnLogout": "N", "ResetOnDisconnect": "N", "FileStoreSync": "Y", "ValidateUserDefinedFields": "N", "ValidateFieldsOutOfOrder": "Y", "RejectInvalidMessage": "Y", "SocketTimeout": "5s"} {
		g.Set(k, v)
	}
	// No StartTime/EndTime: engine has no daily session boundary, preserving
	// recovery across restarts. Coordinated reset is an explicit maintenance action.
	return s
}
func sessionSettings(sender, target string) *quickfix.SessionSettings {
	s := quickfix.NewSessionSettings()
	s.Set("SenderCompID", sender)
	s.Set("TargetCompID", target)
	return s
}
func setTLS(g *quickfix.SessionSettings, enabled bool, t config.TLS) {
	if !enabled {
		return
	}
	g.Set("SocketUseSSL", "Y")
	g.Set("SocketInsecureSkipVerify", "N")
	for k, v := range map[string]string{"SocketCAFile": t.CAFile, "SocketCertificateFile": t.CertFile, "SocketPrivateKeyFile": t.KeyFile, "SocketServerName": t.ServerName} {
		if v != "" {
			g.Set(k, v)
		}
	}
}
