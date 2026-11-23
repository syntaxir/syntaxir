package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// insertionLimits are the missing-token limits of the incremental insertion tests: one and two
// reach the limit on some inputs, and the last none.
var insertionLimits = []int{1, 2, 64}

// incrementalInsertionCases are InsertionCorpus with cases whose repetitions a Document resumes and
// a stream commits: elements that insert, and elements whose insertion backtracks, which takes an
// attempt of the limit and records no error.
func incrementalInsertionCases() []codegentest.Case {
	item := func(body string) string { return "rule main = item* $$\nrule item = " + body }
	return append(codegentest.InsertionCorpus(),
		codegentest.Case{Name: "elements that insert", Src: item(`"a" ";" #missing`),
			Inputs: []string{"a;a;a;", "a;aa;a", "aaa", "a;a;a;a;a", "a"}},
		codegentest.Case{Name: "elements whose insertion backtracks", Src: item(`"a" ";" #missing "!" / "a" "?"`),
			Inputs: []string{"a?", "a?a?a?", "a;!a?", "a?a?a?a?a?", "a!a?"}},
	)
}

// TestDocumentInsertionMatchesFromScratch checks that a Document with a missing-token limit parses
// as a parse from scratch of its text does, on every backend, in both units, at several limits:
// the same results, errors and insertions after each edit from one input of a case to the next,
// and after a parse without an edit. The Document records the runs of every repetition, so that the
// parses after an edit resume them. With RunIgnoresInsertions, which keeps the runs whose elements
// reached an insertion, some parse must differ.
func TestDocumentInsertionMatchesFromScratch(t *testing.T) {
	t.Parallel()
	for _, mutant := range []bool{false, true} {
		differences := 0
		for _, c := range incrementalInsertionCases() {
			prog := mustCompile(t, c.Src)
			for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					for _, limit := range insertionLimits {
						tune := state.Tuning{MinRec: 1}
						if mutant {
							tune.RunMut = state.RunIgnoresInsertions
						}
						for _, d := range documentInsertionDifferences(t, prog, c.Inputs, ParseOptions{Backend: b, Unit: u, Expectations: true, MissingTokens: limit, Tune: tune}) {
							differences++
							if !mutant {
								t.Errorf("%s %v %v limit %d: %s", c.Name, b, u, limit, d)
							}
						}
					}
				}
			}
		}
		if mutant && differences == 0 {
			t.Error("RunIgnoresInsertions: every Document parse matched the parse from scratch")
		}
	}
}

// documentInsertionDifferences parses a Document of the first input with o, edits it into each
// other input in turn and back into the first, parsing after each edit and once more without one,
// and returns how each parse differs from the parse from scratch of the Document's text.
func documentInsertionDifferences(t *testing.T, prog *Program, inputs []string, o ParseOptions) []string {
	t.Helper()
	d, err := prog.NewDocumentWith("main", inputs[0], o)
	if err != nil {
		t.Fatalf("new document: %v", err)
	}
	var out []string
	parse := func(what string) {
		n, err := d.ParseWith(o)
		got := codegentest.ResultJSON(n, err)
		want := codegentest.ResultJSON(prog.ParseWith("main", d.Text(), ParseOptions{Backend: o.Backend, Unit: o.Unit, Expectations: true, MissingTokens: o.MissingTokens}))
		if got != want {
			out = append(out, fmt.Sprintf("%s, text %q:\n document     %s\n from scratch %s", what, d.Text(), got, want))
		}
	}
	parse("first parse")
	for _, next := range append(inputs[1:], inputs[0]) {
		start, end, text := minimalEdit(d.Text(), next, o.Unit)
		if err := d.Edit(start, end, text); err != nil {
			t.Fatalf("edit [%d,%d) %q of %q: %v", start, end, text, d.Text(), err)
		}
		parse("after an edit")
	}
	parse("again")
	return out
}

// minimalEdit returns the edit, in positions of unit u, that replaces the characters between the
// common prefix and the common suffix of from and to. Every invalid UTF-8 byte is a character.
func minimalEdit(from, to string, u sxirinput.PositionUnit) (start, end int, text string) {
	chars := func(s string) []string {
		var out []string
		for s != "" {
			_, n := utf8.DecodeRuneInString(s)
			out, s = append(out, s[:n]), s[n:]
		}
		return out
	}
	a, b := chars(from), chars(to)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	pos := func(chars []string) int {
		if u == sxirinput.Bytes {
			return len(strings.Join(chars, ""))
		}
		return len(chars)
	}
	return pos(a[:p]), pos(a[:len(a)-s]), strings.Join(b[p:len(b)-s], "")
}

// TestDocumentInsertionLimitKeepsSnapshot checks that a reparse of a Document that reaches the
// missing-token limit fails without a snapshot, keeping the one published before it, and that the
// parse after an edit within the limit again matches the parse from scratch.
func TestDocumentInsertionLimitKeepsSnapshot(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "rule main = item* $$\nrule item = \"a\" \";\" #missing")
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		o := ParseOptions{Backend: b, MissingTokens: 2}
		d, err := prog.NewChangeDocumentWith("main", "a;a", o, 8)
		if err != nil {
			t.Fatal(err)
		}
		first, err := d.SnapshotWith(o)
		if first == nil || !isSyntaxErrors(err) {
			t.Fatalf("%v: first snapshot %v, %v", b, first, err)
		}
		before := snapshotJSON(t, first)
		if err := d.Edit(3, 3, "aa"); err != nil {
			t.Fatal(err)
		}
		var limit *runtime.MissingTokenLimitError
		if s, err := d.SnapshotWith(o); s != nil || !errors.As(err, &limit) {
			t.Fatalf("%v: past the limit: snapshot %v, error %v", b, s, err)
		}
		if after := snapshotJSON(t, first); after != before {
			t.Errorf("%v: the published snapshot changed:\n before %s\n after  %s", b, before, after)
		}
		if err := d.Edit(3, 5, ";a"); err != nil {
			t.Fatal(err)
		}
		n, err := d.ParseWith(o)
		if got, want := codegentest.ResultJSON(n, err), codegentest.ResultJSON(prog.ParseWith("main", "a;a;a", o)); got != want {
			t.Errorf("%v: after the failed parse:\n document     %s\n from scratch %s", b, got, want)
		}
	}
}

func isSyntaxErrors(err error) bool {
	var errs value.SyntaxErrors
	return errors.As(err, &errs)
}

// snapshotJSON renders the text, tree and error of the snapshot s.
func snapshotJSON(t *testing.T, s interface {
	Text() string
	Materialize() *value.Node
	Error() error
}) string {
	t.Helper()
	data, err := json.Marshal(s.Materialize())
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%q %s %v", s.Text(), data, s.Error())
}

// TestStreamInsertionMatchesBatch checks that a stream parse with a missing-token limit inserts what
// the batch parse of its input does, on every backend, in both units, at several limits: the
// elements it delivers are those of the batch result, with the same errors, and it fails where the
// batch parse fails, with its error. The stream rule repeats the start rule of each case, which
// ends at the end of the input, so that an element that inserts at the end follows it. With
// MissingLimitPerElement, which gives each element a limit of its own, some parse must differ.
func TestStreamInsertionMatchesBatch(t *testing.T) {
	t.Parallel()
	for _, mutant := range []bool{false, true} {
		differences := 0
		for _, c := range incrementalInsertionCases() {
			prog := mustCompile(t, "rule streamed = items:main* #stream\n"+c.Src)
			for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					for _, limit := range insertionLimits {
						for _, in := range c.Inputs {
							o := ParseOptions{Backend: b, Unit: u, Expectations: true, MissingTokens: limit}
							if mutant {
								o.Tune.MissingMut = state.MissingLimitPerElement
							}
							if d := streamInsertionDifference(t, prog, in, o); d != "" {
								differences++
								if !mutant {
									t.Errorf("%s %v %v limit %d, input %q: %s", c.Name, b, u, limit, in, d)
								}
							}
						}
					}
				}
			}
		}
		if mutant && differences == 0 {
			t.Error("MissingLimitPerElement: every stream parse matched the batch parse")
		}
	}
}

// streamInsertionDifference returns how the stream parse of in with o differs from the batch
// parse, or "".
func streamInsertionDifference(t *testing.T, prog *Program, in string, o ParseOptions) string {
	t.Helper()
	var elements []string
	streamErr := prog.ParseStreamWith("streamed", strings.NewReader(in), func(n *value.Node) error {
		elements = append(elements, codegentest.ResultJSON(n, nil))
		return nil
	}, o)
	batch := o
	batch.Tune = state.Tuning{}
	whole, err := prog.ParseWith("streamed", in, batch)
	errText := func(err error) string {
		if err == nil {
			return "<nil>"
		}
		return err.Error()
	}
	if errText(streamErr) != errText(err) {
		return fmt.Sprintf("stream error %s, batch error %s", errText(streamErr), errText(err))
	}
	if whole == nil {
		return ""
	}
	var want []string
	for _, n := range nodes.D(whole.Field("items").(*value.Node)).Children {
		want = append(want, codegentest.ResultJSON(n, nil))
	}
	if got, want := strings.Join(elements, "\n"), strings.Join(want, "\n"); got != want {
		return fmt.Sprintf("elements\n %s\nbatch elements\n %s", got, want)
	}
	return ""
}
