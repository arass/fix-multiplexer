// Package demo runs synthetic local counterparties. It has no venue credentials.
package demo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/fixadapter"
	"fix-multiplexer/internal/sessions"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
)

func Run(ctx context.Context, out io.Writer) error {
	state, err := os.MkdirTemp("", "fix-mux-demo-")
	if err != nil {
		return errors.New("cannot allocate demo state")
	}
	defer os.RemoveAll(state)
	venueListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer venueListener.Close()
	muxListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer muxListener.Close()
	dictionary := "spec/FIX44.xml"
	venue := testfix.NewPeer()
	vs, venueID, err := testfix.Settings("MOCKVENUE", "MUX_UP", venueListener.Addr().String(), dictionary, false)
	if err != nil {
		return err
	}
	acceptor, err := testfix.Listen(venue, venueListener, vs)
	if err != nil {
		return err
	}
	defer acceptor.Stop()
	c := config.Defaults()
	c.Service.HealthBind = ""
	c.Upstream.Host = "127.0.0.1"
	_, port, _ := net.SplitHostPort(venueListener.Addr().String())
	c.Upstream.Port, _ = strconv.Atoi(port)
	c.Upstream.Sender = "MUX_UP"
	c.Upstream.Target = "MOCKVENUE"
	c.Upstream.StoreDir = filepath.Join(state, "upstream")
	c.Downstream.Listen = muxListener.Addr().String()
	c.Downstream.Target = "MUX_DOWN"
	c.Downstream.StoreDir = filepath.Join(state, "downstream")
	for _, id := range []string{"DEV_A", "DEV_B", "QA_1"} {
		c.Downstream.Clients = append(c.Downstream.Clients, config.Client{Sender: id})
	}
	mux, err := fixadapter.New(c)
	if err != nil {
		return err
	}
	mux.SetListenerFactory(func(string, *tls.Config) (net.Listener, error) { return muxListener, nil })
	if err = mux.Start(); err != nil {
		return err
	}
	defer mux.Stop()
	if _, err = testfix.Receive(ctx, venue.Logons); err != nil {
		return err
	}
	var peers []*testfix.Peer
	var ids []quickfix.SessionID
	var initiators []*quickfix.Initiator
	defer func() {
		for _, i := range initiators {
			i.Stop()
		}
	}()
	for _, cl := range c.Downstream.Clients {
		p := testfix.NewPeer()
		// Valid inbound PossDup traffic must still be forwarded. The other FIX
		// boundary regenerates its own replay flags and timing headers.
		p.BeforeSend = func(m *quickfix.Message, _ quickfix.SessionID) error {
			if m.IsMsgTypeOf("D") {
				m.Header.SetBool(43, true)
				m.Header.SetString(122, time.Now().UTC().Add(-time.Second).Format("20060102-15:04:05.000"))
			}
			return nil
		}
		s, id, e := testfix.Settings(cl.Sender, c.Downstream.Target, c.Downstream.Listen, dictionary, true)
		if e != nil {
			return e
		}
		i, e := testfix.Connect(p, s)
		if e != nil {
			return e
		}
		initiators = append(initiators, i)
		if _, e = testfix.Receive(ctx, p.Logons); e != nil {
			return e
		}
		peers = append(peers, p)
		ids = append(ids, id)
	}
	if err = Wait(ctx, mux.Registry, func(states []sessions.State) bool {
		count := 0
		for _, s := range states {
			if s.LoggedOn {
				count++
			}
		}
		return count == 4
	}); err != nil {
		return err
	}
	for idx, id := range ids {
		order := testfix.Order(fmt.Sprintf("synthetic-%d", idx))
		order.Body.SetString(5001, "custom-extension")
		if err = quickfix.SendToTarget(order, id); err != nil {
			return err
		}
		var received *quickfix.Message
		var e error
		select {
		case received = <-venue.Messages:
		case rej := <-peers[idx].Messages:
			typ, _ := rej.MsgType()
			return fmt.Errorf("demo order received application rejection (type %s)", typ)
		case rej := <-peers[idx].Admin:
			if rej.IsMsgTypeOf("3") {
				reason, _ := rej.Body.GetString(58)
				tag, _ := rej.Body.GetString(371)
				return fmt.Errorf("demo synthetic order rejected: %s (tag %s)", reason, tag)
			}
			received, e = testfix.Receive(ctx, venue.Messages)
			if e != nil {
				return errors.New("demo waiting for venue order timed out")
			}
		case <-ctx.Done():
			return errors.New("demo waiting for venue order timed out")
		}
		value, _ := received.Body.GetString(11)
		custom, _ := received.Body.GetString(5001)
		sender, _ := received.Header.GetString(49)
		if value != fmt.Sprintf("synthetic-%d", idx) || custom != "custom-extension" || sender != c.Upstream.Sender {
			return errors.New("demo order fidelity failed")
		}
	}
	report := testfix.Report("unknown-order-is-still-broadcast")
	report.Body.SetString(5001, "custom-report")
	partyTemplate := quickfix.GroupTemplate{quickfix.GroupElement(448), quickfix.GroupElement(447), quickfix.GroupElement(452)}
	parties := quickfix.NewRepeatingGroup(453, partyTemplate)
	for _, party := range []string{"SYNTHETIC_A", "SYNTHETIC_B"} {
		g := parties.Add()
		g.SetString(448, party)
		g.SetString(447, "D")
		g.SetInt(452, 1)
	}
	report.Body.SetGroup(parties)
	if err = quickfix.SendToTarget(report, venueID); err != nil {
		return err
	}
	for idx, p := range peers {
		m, e := waitReport(ctx, p, venue)
		if e != nil {
			return fmt.Errorf("demo waiting for client report (forwarded=%d failed=%d recipients=%d): %w", mux.Router.Counters.Forwarded.Load(), mux.Router.Counters.Failed.Load(), mux.Router.Counters.Recipients.Load(), e)
		}
		target, _ := m.Header.GetString(56)
		sender, _ := m.Header.GetString(49)
		id, _ := m.Body.GetString(11)
		custom, _ := m.Body.GetString(5001)
		if target != ids[idx].SenderCompID || sender != c.Downstream.Target || id != "unknown-order-is-still-broadcast" || custom != "custom-report" {
			return errors.New("demo fanout fidelity failed")
		}
		got := quickfix.NewRepeatingGroup(453, partyTemplate)
		if e = m.Body.GetGroup(got); e != nil || got.Len() != 2 {
			return errors.New("demo repeating group lost")
		}
		last, _ := got.Get(1).GetString(448)
		if last != "SYNTHETIC_B" {
			return errors.New("demo group order changed")
		}
	}
	// Prove live-only selection after a real Logout, with the remaining peers active.
	initiators[0].Stop()
	if err = Wait(ctx, mux.Registry, func(states []sessions.State) bool {
		for _, s := range states {
			if s.ID == "FIX.4.4:MUX_DOWN->DEV_A" {
				return !s.LoggedOn
			}
		}
		return false
	}); err != nil {
		return err
	}
	if err = quickfix.SendToTarget(testfix.Report("while-a-offline"), venueID); err != nil {
		return err
	}
	for _, p := range peers[1:] {
		m, e := testfix.Receive(ctx, p.Messages)
		if e != nil {
			return e
		}
		id, _ := m.Body.GetString(11)
		if id != "while-a-offline" {
			return errors.New("live-only fanout failed")
		}
	}
	// Standard gateway error on outage, without a fabricated execution report.
	acceptor.Stop()
	if err = Wait(ctx, mux.Registry, func(states []sessions.State) bool {
		for _, s := range states {
			if s.ID == mux.Application.Upstream.String() {
				return !s.LoggedOn
			}
		}
		return false
	}); err != nil {
		return err
	}
	if mux.Router.Ready() {
		return errors.New("outage readiness failed")
	}
	if err = quickfix.SendToTarget(testfix.Order("outage-order"), ids[1]); err != nil {
		return err
	}
	rej, err := testfix.Receive(ctx, peers[1].Messages)
	if err != nil {
		return err
	}
	text, _ := rej.Body.GetString(58)
	ref, _ := rej.Body.GetString(379)
	if !rej.IsMsgTypeOf("j") || text != "gateway: upstream unavailable" || ref != "outage-order" {
		return errors.New("gateway error semantics failed")
	}
	_, err = fmt.Fprintln(out, "PASS: 3 clients, 1 upstream, unchanged orders/custom fields, live-only fanout, gateway FIX outage error")
	return err
}

func waitReport(ctx context.Context, p, venue *testfix.Peer) (*quickfix.Message, error) {
	for {
		select {
		case m := <-p.Messages:
			return m, nil
		case m := <-venue.Admin:
			if m.IsMsgTypeOf("3") {
				tag, _ := m.Body.GetString(371)
				reason, _ := m.Body.GetString(373)
				return nil, fmt.Errorf("mock venue received session reject: tag=%s reason=%s", tag, reason)
			}
		case m := <-p.Admin:
			if m.IsMsgTypeOf("3") {
				tag, _ := m.Body.GetString(371)
				reason, _ := m.Body.GetString(373)
				return nil, fmt.Errorf("mock client received session reject: tag=%s reason=%s", tag, reason)
			}
		case <-ctx.Done():
			return nil, errors.New("report timed out")
		}
	}
}

func Wait(ctx context.Context, r *sessions.Registry, predicate func([]sessions.State) bool) error {
	for {
		states, changed := r.Watch()
		if predicate(states) {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.New("session transition timed out")
		}
	}
}
