package fixadapter

import (
	"strings"
	"testing"
)

func FuzzMessageCopy(f *testing.F) {
	f.Add("synthetic-order", "synthetic-extension")
	d := dictionary(f)
	f.Fuzz(func(t *testing.T, id, extension string) {
		if len(id) > 256 || len(extension) > 256 || strings.ContainsAny(id+extension, "\x00\x01") {
			return
		}
		m := framed("8")
		m.Body.SetString(11, id)
		m.Body.SetString(5001, extension)
		s, err := Snapshot(m, d)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := s.Clone()
		if err != nil {
			t.Fatal(err)
		}
		out := copy.(*Message).out
		got, _ := out.Body.GetString(11)
		ext, _ := out.Body.GetString(5001)
		if got != id || ext != extension {
			t.Fatal("body changed")
		}
		if out.Header.Has(49) || out.Header.Has(34) || out.Header.Has(43) {
			t.Fatal("session leakage")
		}
	})
}
