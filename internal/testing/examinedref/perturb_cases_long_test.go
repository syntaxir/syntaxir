//go:build sxir_long

package examinedref_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// The perturbation and high-water tests run every producer on the same cases: the representative cases of
// the generated corpus, which have every construct of the language (Operators Rules, predicates,
// variables, recovery and the rest, which the reference evaluator does not cover), the checked-in csv
// grammar, and streamShapes.

// perturbMaxUnits bounds the inputs that the perturbation tests perturb: an input of n units has up to n
// change points, each parsed again with the whole trace kept, so the cost grows with the square of n. The
// high-water tests read every input.
const perturbMaxUnits = 128

// streamShapes are stream grammars for the high-water tests: where the parser must stop reading and what it
// must have read when an element ends in a literal, in a repetition, in a lookahead, at a line anchor or in
// a character whose UTF-8 encoding the input cuts or breaks, and when the start rule ends before the input
// does, which only the whole-input check examines.
var streamShapes = []codegentest.Case{
	{Name: "stream records", Src: `rule main = item* #stream $$
rule item = [a-z]+ ";"`, Inputs: []string{"", "ab;cd;", "ab;c", "ab;1", "a;;"}},
	{Name: "stream ends in a literal", Src: `rule main = h:("a" "b") rs:r* #stream "!"
rule r = "x" "y"`, Inputs: []string{"abxyxy!", "ab!", "abxyx!", "ab!z", "a"}},
	{Name: "stream start rule stops early", Src: `rule main = item* #stream
rule item = "ab"`, Inputs: []string{"", "abab", "aba", "abx", "ab"}},
	{Name: "stream lookahead", Src: `rule main = item* #stream $$
rule item = "a" &"b" / "b" !"c" / "c"`, Inputs: []string{"abab", "abc", "bc", "cab", "ac"}},
	{Name: "stream line anchors", Src: `rule main = line* #stream $$
rule line = ^ [a-z]* #atomic $ "\n"`, Inputs: []string{"ab\ncd\n", "ab\n\nc\n", "ab\nc", "\n\n"}},
	{Name: "stream unicode", Src: `rule main = ch* #stream $$
rule ch = "é" / "😀" / "\u{fffd}" / [a-z]`, Inputs: []string{"aé😀b", "é\xe2\x82", "\xff\xfe", "a\xe2\x82\xacb", "\xf0\x9f\x98", "😀😀", "\xe2\x82a"}},
	{Name: "stream any character", Src: `rule main = (. ";")* #stream $$`,
		Inputs: []string{"é;", "\xe2\x82;", "\xe2\x82", "😀;\xf0\x9f", "a;b", "\xc3"}},
	{Name: "stream left recursive elements", Src: `rule main = (e ";") * #stream $$
rule e = e "+" t / t
rule t = [0-9]+ #atomic`, Inputs: []string{"1+2;3;", "1+;", "1+2+3;4+5;", "12"}},
	{Name: "stream variables", Src: `var n int
rule main = {n := 0} item* #stream $$
rule item = "a" {n := n + 1} / "b" {n > 0}`, Inputs: []string{"ab", "b", "aab", "abb"}},
}

// perturbCases returns the cases of the perturbation and high-water tests.
func perturbCases(t *testing.T) []codegentest.Case {
	t.Helper()
	cases := append([]codegentest.Case(nil), codegentest.Representative(codegentest.GenCorpus(t))...)
	cases = append(cases, streamShapes...)
	dir := filepath.Join("..", "..", "..", "testdata", "grammars", "csv")
	src, err := os.ReadFile(filepath.Join(dir, "csv.sxir"))
	if err != nil {
		t.Fatal(err)
	}
	csv := codegentest.Case{Name: "csv", Src: string(src), Inputs: []string{"", "\ufeffa,b\r\n", "a,\"x\"\"y\"\n\n", "a,\"unterminated", "a\rb\r\n"}}
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no inputs of the csv grammar in %s (%v)", dir, err)
	}
	for _, f := range files {
		in, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		csv.Inputs = append(csv.Inputs, string(in))
	}
	return append(cases, csv)
}

// perturbUnits are the position units of the tests, with the names their messages use.
var perturbUnits = []struct {
	name string
	unit input.PositionUnit
}{{"bytes", input.Bytes}, {"code-points", input.CodePoints}}

var perturbBackends = []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative}

// traceEventOf converts an event of the engine.
func traceEventOf(e engine.TraceEvent) examinedref.Event {
	k := examinedref.EventSkipped
	switch e.Kind {
	case instrument.TraceEnter:
		k = examinedref.EventEnter
	case instrument.TraceExit:
		k = examinedref.EventExit
	}
	return examinedref.Event{Kind: k, Rule: e.Rule, Level: e.Level, Depth: e.Depth, Pos: e.Pos, Lookahead: e.Lookahead,
		Matched: e.Matched, End: e.End, Memo: e.Memo, Evals: e.Evals, ReusedElements: e.ReusedElements,
		Examined: e.Examined, SkipReason: string(e.SkipReason)}
}

// hasStreamStart reports whether the start rule of prog has a #stream repetition, which ParseStream needs.
func hasStreamStart(prog *engine.Program) bool {
	err := prog.ParseStreamWith("main", strings.NewReader(""), nil, engine.ParseOptions{})
	return err == nil || !strings.Contains(err.Error(), "has no #stream repetition")
}

// reportPerturb reports msgs as errors with prefix, the first limit of them, and their number.
func reportPerturb(t *testing.T, prefix string, msgs []string, limit int) {
	t.Helper()
	for i, m := range msgs {
		if i == limit {
			t.Errorf("%s: %d more", prefix, len(msgs)-limit)
			return
		}
		t.Errorf("%s: %s", prefix, m)
	}
}
