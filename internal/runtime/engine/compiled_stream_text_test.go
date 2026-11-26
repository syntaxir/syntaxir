package engine

import (
	"cmp"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestCompiledStreamTextBeforeCommitFails checks that a stream of a loaded module that takes the
// text of a node whose range begins before the last commit point, which the checker rejects in
// every grammar, ends with an error of invalid bytecode where it takes it, on every backend and
// however the reader splits the input. The module is that of a grammar whose predicate after the
// #stream repetition takes the text of a string node, with the capture it reads changed to one
// that holds a tuple node made before the repetition. Before the first commit the text is whole,
// and the predicate decides the parse as it would in a batch parse.
func TestCompiledStreamTextBeforeCommitFails(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `@start
rule main = h:("h" "h") g:"g" item* #stream { text($g) == "g" } $$
rule item = "a" ";"
`)
	loaded, _, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
	if err != nil {
		t.Fatal(err)
	}
	m := prog.Module()
	changed := false
	for i := range m.Actions {
		code := m.Actions[i].Code
		text := slices.IndexFunc(code, func(in bytecode.ActionInstr) bool { return in.Op == uint8(state.TText) })
		if text < 0 {
			continue
		}
		capture := slices.IndexFunc(code, func(in bytecode.ActionInstr) bool {
			return in.Op == uint8(state.TCap) && in.Dst == code[text].A
		})
		if capture < 0 {
			t.Fatalf("the text of action %d reads no capture\n%s", i, m.Disassemble())
		}
		// main captures h and g, in slots 0 and 1.
		code[capture].A = 1 - code[capture].A
		changed = true
	}
	if !changed {
		t.Fatalf("no action takes a text\n%s", m.Disassemble())
	}
	crafted, _, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
	if err != nil {
		t.Fatalf("loading the module that reads h: %v", err)
	}
	readers := map[string]func(string) io.Reader{
		"whole":    func(s string) io.Reader { return strings.NewReader(s) },
		"one byte": func(s string) io.Reader { return iotest.OneByteReader(strings.NewReader(s)) },
	}
	stream := func(p *Program, in string, b Backend, r func(string) io.Reader) string {
		n := 0
		err := p.ParseStreamWith("main", r(in), func(*value.Node) error { n++; return nil }, ParseOptions{Backend: b})
		if err != nil {
			return err.Error()
		}
		return strings.Repeat("element ", n) + "ok"
	}
	for _, b := range vmBackends {
		for name, r := range readers {
			for _, c := range []struct{ in, compiled, crafted string }{
				{"hhga;a;a;", "element element element ok", "invalid bytecode: the text of [0, 2) begins before the commit point 9"},
				{"hhga;", "element ok", "invalid bytecode: the text of [0, 2) begins before the commit point 5"},
				// Before the first commit the text of h is the whole "hh", and the predicate fails.
				{"hhg", "ok", ""},
			} {
				if got := stream(loaded, c.in, b, r); got != c.compiled {
					t.Errorf("%v, %s, %q: the compiled module gives %s, want %s", b, name, c.in, got, c.compiled)
				}
				got := stream(crafted, c.in, b, r)
				if c.crafted == "" && (strings.HasSuffix(got, "ok") || strings.Contains(got, "invalid bytecode")) || !strings.Contains(got, c.crafted) {
					t.Errorf("%v, %s, %q: the crafted module gives %s, want %s", b, name, c.in, got, cmp.Or(c.crafted, "a syntax error"))
				}
			}
		}
	}
}
