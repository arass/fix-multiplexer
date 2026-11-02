// Package maintenance performs explicit offline client-store administration.
package maintenance

import (
	"errors"
	"fmt"
	"io"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/retention"
	"github.com/quickfixgo/quickfix"
)

func Run(c config.Config, command, client string, dryRun bool, out io.Writer) error {
	m, err := retention.OpenMaintenance([]string{c.Upstream.StoreDir, c.Downstream.StoreDir}, c.Retention.DiskLimitBytes, c.Retention.SessionLimitBytes, time.Now)
	if err != nil {
		return err
	}
	defer m.Close()
	found := false
	for _, cl := range c.Downstream.Clients {
		if command == "reset" && cl.Sender != client {
			continue
		}
		found = true
		id := quickfix.SessionID{BeginString: "FIX.4.4", SenderCompID: c.Downstream.Target, TargetCompID: cl.Sender}.String()
		exists, err := m.LoadExisting(id, c.Downstream.StoreDir)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = fmt.Fprintf(out, "%s: no stored session\n", cl.Sender); err != nil {
				return err
			}
			continue
		}
		s, _ := m.State(id)
		switch command {
		case "list":
			_, err = fmt.Fprintf(out, "%s: active=%t retired=%t last_disconnect=%s\n", cl.Sender, s.Active, s.Retired, s.LastDisconnect.UTC().Format(time.RFC3339))
		case "prune":
			eligible, e := m.Prune(id, c.Retention.IdleTTL, dryRun)
			if e != nil {
				return e
			}
			_, err = fmt.Fprintf(out, "%s: eligible=%t dry_run=%t\n", cl.Sender, eligible, dryRun)
		case "reset":
			if !dryRun {
				if err = m.Reset(id); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(out, "%s: coordinated_reset dry_run=%t (peer must also reset to sequence 1)\n", cl.Sender, dryRun)
		default:
			return errors.New("unknown maintenance operation")
		}
		if err != nil {
			return err
		}
	}
	if !found {
		return errors.New("maintenance client is not allowlisted")
	}
	return nil
}
