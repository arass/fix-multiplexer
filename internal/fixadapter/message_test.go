package fixadapter

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/testfix"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/datadictionary"
)

func dictionary(t testing.TB) *datadictionary.DataDictionary {
	t.Helper()
	d, err := datadictionary.Parse("../../spec/FIX44.xml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func framed(typ string) *quickfix.Message {
	m := quickfix.NewMessage()
	m.Header.SetString(8, "FIX.4.4")
	m.Header.SetString(35, typ)
	m.Header.SetString(49, "SOURCE")
	m.Header.SetString(56, "DEST")
	m.Header.SetInt(34, 77)
	m.Header.SetString(52, "20260101-12:00:00.000")
	return m
}

func TestCopyGroupsAndSessionIsolation(t *testing.T) {
	d := dictionary(t)
	m := framed("8")
	m.Header.SetBool(43, true)
	m.Header.SetString(122, "20260101-11:59:59.000")
	m.Header.SetString(115, "OTHER")
	m.Header.SetString(128, "ELSEWHERE")
	for tag, value := range map[quickfix.Tag]string{11: "original-client", 41: "original-previous", 37: "original-order", 17: "original-exec", 5001: "extension"} {
		m.Body.SetString(tag, value)
	}
	partyTemplate := quickfix.GroupTemplate{quickfix.GroupElement(448), quickfix.GroupElement(447), quickfix.GroupElement(452), quickfix.NewRepeatingGroup(802, quickfix.GroupTemplate{quickfix.GroupElement(523), quickfix.GroupElement(803)})}
	parties := quickfix.NewRepeatingGroup(453, partyTemplate)
	for _, id := range []string{"PARTY_A", "PARTY_B"} {
		g := parties.Add()
		g.SetString(448, id)
		g.SetString(447, "D")
		g.SetInt(452, 1)
		sub := quickfix.NewRepeatingGroup(802, quickfix.GroupTemplate{quickfix.GroupElement(523), quickfix.GroupElement(803)})
		sub.Add().SetString(523, "sub-one")
		sub.Get(0).SetInt(803, 1)
		sub.Add().SetString(523, "sub-two")
		sub.Get(1).SetInt(803, 2)
		g.SetGroup(sub)
	}
	m.Body.SetGroup(parties)
	raw := append([]byte(nil), m.Bytes()...)
	parsed := quickfix.NewMessage()
	if err := quickfix.ParseMessageWithDataDictionary(parsed, bytes.NewBuffer(raw), d, d); err != nil {
		t.Fatal(err)
	}
	s, err := Snapshot(parsed, d)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	x, y := a.(*Message), b.(*Message)
	if x.out == y.out || s.Size() != len(raw) || s.Type() != "8" {
		t.Fatal("shared copy")
	}
	if tags := x.out.Header.Tags(); len(tags) != 1 || tags[0] != 35 {
		t.Fatal("session header leak", tags)
	}
	for _, out := range []*quickfix.Message{x.out, y.out} {
		for tag, value := range map[quickfix.Tag]string{11: "original-client", 41: "original-previous", 37: "original-order", 17: "original-exec", 5001: "extension"} {
			v, e := out.Body.GetString(tag)
			if e != nil || v != value {
				t.Fatalf("tag %d changed", tag)
			}
		}
		p := quickfix.NewRepeatingGroup(453, partyTemplate)
		if e := out.Body.GetGroup(p); e != nil || p.Len() != 2 {
			t.Fatal("party loss", e, p.Len())
		}
		for i, id := range []string{"PARTY_A", "PARTY_B"} {
			v, _ := p.Get(i).GetString(448)
			if v != id {
				t.Fatal("group order changed")
			}
			sub := quickfix.NewRepeatingGroup(802, quickfix.GroupTemplate{quickfix.GroupElement(523), quickfix.GroupElement(803)})
			if err := p.Get(i).GetGroup(sub); err != nil || sub.Len() != 2 {
				t.Fatal("nested group loss", err)
			}
			v, _ = sub.Get(1).GetString(523)
			if v != "sub-two" {
				t.Fatal(v)
			}
		}
	}
	x.out.Body.SetString(11, "changed")
	v, _ := y.out.Body.GetString(11)
	if v != "original-client" || !bytes.Equal(raw, parsed.Bytes()) {
		t.Fatal("input or sibling mutated")
	}
}

func TestGroupedReportWireRoundTrip(t *testing.T) {
	d := dictionary(t)
	m := testfix.Report("synthetic")
	for tag, value := range map[quickfix.Tag]string{8: "FIX.4.4", 49: "SOURCE", 56: "DEST", 34: "2", 52: "20260101-12:00:00.000"} {
		m.Header.SetString(tag, value)
	}
	m.Body.SetString(5001, "custom-report")
	tmpl := quickfix.GroupTemplate{quickfix.GroupElement(448), quickfix.GroupElement(447), quickfix.GroupElement(452)}
	g := quickfix.NewRepeatingGroup(453, tmpl)
	for _, id := range []string{"A", "B"} {
		p := g.Add()
		p.SetString(448, id)
		p.SetString(447, "D")
		p.SetInt(452, 1)
	}
	m.Body.SetGroup(g)
	parse := func(m *quickfix.Message) *quickfix.Message {
		t.Helper()
		p := quickfix.NewMessage()
		if err := quickfix.ParseMessageWithDataDictionary(p, bytes.NewBuffer(m.Bytes()), nil, d); err != nil {
			t.Fatal(err)
		}
		if err := quickfix.NewValidator(quickfix.ValidatorSettings{CheckFieldsOutOfOrder: true, RejectInvalidMessage: true, CheckFieldsHaveValues: true}, d, nil).Validate(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	s, err := Snapshot(parse(m), d)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	out := cl.(*Message).out
	for tag, value := range map[quickfix.Tag]string{8: "FIX.4.4", 49: "NEW_SOURCE", 56: "NEW_DEST", 34: "2", 52: "20260101-12:00:00.000"} {
		out.Header.SetString(tag, value)
	}
	parse(out)
}

func TestEncodedAndErrors(t *testing.T) {
	d := dictionary(t)
	m := framed("8")
	data := "encoded-text"
	m.Body.SetInt(354, len(data))
	m.Body.SetString(355, data)
	s, err := Snapshot(m, d)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := clone.(*Message).out.Body.GetString(355)
	if got != data {
		t.Fatal("encoded data changed")
	}
	// Engine v0.9.12 only length-decodes XMLData, not EncodedText containing SOH.
	// Keep a regression proving explicit failure rather than lossy forwarding.
	m = framed("8")
	m.Body.SetInt(354, 5)
	m.Body.SetString(355, "ab\x01cd")
	unsupported, _ := Snapshot(m, d)
	if _, err := unsupported.Clone(); err == nil {
		t.Fatal("embedded-SOH engine limitation unexpectedly changed; review fidelity policy")
	}
	if _, err := Snapshot(quickfix.NewMessage(), d); err == nil {
		t.Fatal("missing type")
	}
	for _, s := range []*Message{{raw: []byte("bad"), dict: d}, {raw: framed("ZZ").Bytes(), typ: "ZZ", dict: d}} {
		if _, err := s.Clone(); err == nil {
			t.Fatal("bad message accepted")
		}
	}
	if (EngineSender{}).Send(&Message{}, "unknown") == nil {
		t.Fatal("invalid outbound accepted")
	}
	s, _ = Snapshot(framed("D"), d)
	clone, err = s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if (EngineSender{}).Send(clone, "unknown") == nil {
		t.Fatal("unknown target accepted")
	}
}

func TestResolveSecret(t *testing.T) {
	t.Setenv("MUX_TEST_SECRET", "synthetic-only")
	v, err := ResolveSecret(config.Secret{Env: "MUX_TEST_SECRET"})
	if err != nil || v != "synthetic-only" {
		t.Fatal(err)
	}
	for _, value := range []string{"", "bad\x01value", "bad\nvalue"} {
		t.Setenv("MUX_TEST_SECRET", value)
		if _, err := ResolveSecret(config.Secret{Env: "MUX_TEST_SECRET"}); err == nil {
			t.Fatal("bad secret")
		}
	}
	if _, err := ResolveSecret(config.Secret{Env: "MUX_MISSING_TEST_SECRET"}); err == nil {
		t.Fatal("missing env")
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("synthetic-only\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err = ResolveSecret(config.Secret{File: path})
	if err != nil || v != "synthetic-only" {
		t.Fatal(err)
	}
	for _, value := range []string{"", strings.Repeat("x", 4097), "bad\x01value"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveSecret(config.Secret{File: path}); err == nil {
			t.Fatal("bad file secret")
		}
	}
	if _, err := ResolveSecret(config.Secret{File: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("missing file")
	}
	if v, err := ResolveSecret(config.Secret{}); v != "" || err != nil {
		t.Fatal("empty reference")
	}
}
