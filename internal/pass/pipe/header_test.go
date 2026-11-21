package pipe

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

type opts struct {
	Start     string
	Typed     bool
	MaxDepth  int
	NoOpt     []string
	Opt       map[string]int
	TSLib     string
	Mode      mode
	Writer    io.Writer
	unwritten bool
}

type mode int

var modeCodec = &Codec{
	Format: func(v reflect.Value) string { return [...]string{"a", "b"}[v.Int()] },
	Parse: func(s string, v reflect.Value) error {
		v.SetInt(int64(strings.Index("ab", s)))
		return nil
	},
}

func optsHeader() *Header[opts] {
	return &Header[opts]{
		Words: []HeaderWord{
			{Word: "options", Fields: []HeaderField{{Field: "Start"}}},
			{Word: "target", Fields: []HeaderField{{Field: "Typed"}, {Field: "MaxDepth"}, {Field: "NoOpt"}, {Field: "Opt"},
				{Field: "TSLib"}, {Field: "Mode", Key: "lib", Codec: modeCodec}}},
		},
		Omit: []string{"Writer"},
	}
}

// TestHeaderRoundTrip checks that every field a scheme writes reads back equal through the text,
// escapes included, and that the keys are the field names in lower case.
func TestHeaderRoundTrip(t *testing.T) {
	h := optsHeader()
	if err := h.Check(); err != nil {
		t.Fatal(err)
	}
	v := opts{Start: "a b%c", Typed: true, MaxDepth: -3, NoOpt: []string{"x,y", "z=1"}, Opt: map[string]int{"p": 2, "o%": 0},
		TSLib: "../lib.ts", Mode: 1}
	d := &Description{Version: Format, Header: []Line{h.Line("options", "", v), h.Line("target", "go", v)}, In: "t-mod", Out: "t-mod"}
	text := d.String()
	if !strings.Contains(text, "target go typed=true max-depth=-3 no-opt=x%252Cy,z%253D1 opt=o%2525=0,p=2 ts-lib=../lib.ts lib=b\n") {
		t.Errorf("the target line:\n%s", text)
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	var got opts
	for _, l := range back.Header {
		if err := h.Read(l, &got); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, v) {
		t.Errorf("read back %+v, want %+v", got, v)
	}
}

// TestHeaderCheck checks that a scheme that misses a field, gives one two places, names no field
// or writes a field without a text form fails, naming it.
func TestHeaderCheck(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(h *Header[opts])
		want string
	}{
		{"missing field", func(h *Header[opts]) { h.Omit = nil }, "field Writer is written by no header word and is not omitted"},
		{"two places", func(h *Header[opts]) { h.Omit = append(h.Omit, "Start") }, "field Start has 2 places (omitted, options)"},
		{"no such field", func(h *Header[opts]) { h.Omit = append(h.Omit, "Stop") }, "Stop is no exported field"},
		{"no text form", func(h *Header[opts]) {
			h.Omit = nil
			h.Words[0].Fields = append(h.Words[0].Fields, HeaderField{Field: "Writer"})
		}, "field Writer of type io.Writer has no text form"},
		{"key twice", func(h *Header[opts]) { h.Words[1].Fields[0].Key = "max-depth" }, `key "max-depth" of MaxDepth`},
	} {
		h := optsHeader()
		c.edit(h)
		if err := h.Check(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// TestHeaderReadRejects checks that a line must give every key of its word once and no other.
func TestHeaderReadRejects(t *testing.T) {
	h := optsHeader()
	full := h.Line("target", "go", opts{})
	for _, c := range []struct {
		name string
		l    Line
		want string
	}{
		{"missing key", Line{Word: "target", Fields: full.Fields[1:]}, "typed is missing"},
		{"unknown key", Line{Word: "target", Fields: append(append([]Field(nil), full.Fields...), Field{"typo", "1"})}, "unknown key typo"},
		{"unknown word", Line{Word: "debug"}, `unknown header word "debug"`},
		{"bad value", Line{Word: "target", Fields: append([]Field{{"typed", "maybe"}}, full.Fields[1:]...)}, "target: typed:"},
	} {
		var v opts
		if err := h.Read(c.l, &v); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}
