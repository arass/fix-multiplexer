package fixadapter

import (
	"bytes"
	"errors"

	"fix-multiplexer/internal/gateway"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/datadictionary"
)

// Message owns an immutable wire snapshot plus an independent outbound engine model.
type Message struct {
	raw  []byte
	typ  string
	dict *datadictionary.DataDictionary
	out  *quickfix.Message
}

func Snapshot(m *quickfix.Message, d *datadictionary.DataDictionary) (*Message, error) {
	t, err := m.MsgType()
	if err != nil {
		return nil, errors.New("missing application message type")
	}
	return &Message{raw: append([]byte(nil), m.Bytes()...), typ: t, dict: d}, nil
}
func (m *Message) Type() string { return m.typ }
func (m *Message) Size() int    { return len(m.raw) }

func (m *Message) Clone() (gateway.Message, error) {
	parsed := quickfix.NewMessage()
	if err := quickfix.ParseMessageWithDataDictionary(parsed, bytes.NewBuffer(append([]byte(nil), m.raw...)), m.dict, m.dict); err != nil {
		return nil, errors.New("cannot reconstruct application message")
	}
	out := quickfix.NewMessage()
	out.Header.SetString(35, m.typ)
	def, ok := m.dict.Messages[m.typ]
	if !ok {
		return nil, errors.New("message type absent from dictionary")
	}
	// Do not use Message.CopyInto: FieldMap.CopyInto copies only the first
	// TagValue of a group in engine v0.9.12. Rebuild with engine group APIs.
	for _, tag := range parsed.Body.Tags() {
		if f, ok := def.Fields[int(tag)]; ok && f.IsGroup() {
			group := quickfix.NewRepeatingGroup(tag, template(f.Fields))
			if err := parsed.Body.GetGroup(group); err != nil {
				return nil, errors.New("cannot reconstruct repeating group")
			}
			rebuilt, err := copyGroup(group, f.Fields)
			if err != nil {
				return nil, err
			}
			out.Body.SetGroup(rebuilt)
		} else {
			v, err := parsed.Body.GetBytes(tag)
			if err != nil {
				return nil, errors.New("cannot reconstruct application field")
			}
			out.Body.SetBytes(tag, append([]byte(nil), v...))
		}
	}
	return &Message{raw: append([]byte(nil), m.raw...), typ: m.typ, dict: m.dict, out: out}, nil
}

// Parsed group fields in this engine retain tail slices for GetGroup. Their
// Write method must not be reused directly: rebuild each element with setters.
func copyGroup(source *quickfix.RepeatingGroup, fields []*datadictionary.FieldDef) (*quickfix.RepeatingGroup, error) {
	out := quickfix.NewRepeatingGroup(source.Tag(), template(fields))
	for i := 0; i < source.Len(); i++ {
		from, to := source.Get(i), out.Add()
		for _, f := range fields {
			tag := quickfix.Tag(f.Tag())
			if !from.Has(tag) {
				continue
			}
			if f.IsGroup() {
				nested := quickfix.NewRepeatingGroup(tag, template(f.Fields))
				if err := from.GetGroup(nested); err != nil {
					return nil, errors.New("cannot reconstruct nested group")
				}
				copy, err := copyGroup(nested, f.Fields)
				if err != nil {
					return nil, err
				}
				to.SetGroup(copy)
			} else {
				v, err := from.GetBytes(tag)
				if err != nil {
					return nil, errors.New("cannot reconstruct group field")
				}
				to.SetBytes(tag, append([]byte(nil), v...))
			}
		}
	}
	return out, nil
}

func template(fields []*datadictionary.FieldDef) quickfix.GroupTemplate {
	t := make(quickfix.GroupTemplate, 0, len(fields))
	for _, f := range fields {
		if f.IsGroup() {
			t = append(t, quickfix.NewRepeatingGroup(quickfix.Tag(f.Tag()), template(f.Fields)))
		} else {
			t = append(t, quickfix.GroupElement(quickfix.Tag(f.Tag())))
		}
	}
	return t
}

type EngineSender struct{ IDs map[string]quickfix.SessionID }

func (s EngineSender) Send(m gateway.Message, id string) error {
	f, ok := m.(*Message)
	if !ok || f.out == nil {
		return errors.New("invalid engine message")
	}
	target, ok := s.IDs[id]
	if !ok {
		return errors.New("unknown target")
	}
	return quickfix.SendToTarget(f.out, target)
}
