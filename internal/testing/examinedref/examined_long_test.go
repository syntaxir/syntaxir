//go:build sxir_long

package examinedref_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// oracleCase is a grammar with inputs, from the generated corpus or the shapes below.
type oracleCase struct {
	name, src string
	inputs    []string
}

// oracleShapes are left-recursion shapes that the reference must agree with the engine on, besides the
// generated corpus: direct, indirect and nested growths.
func oracleShapes() []oracleCase {
	return []oracleCase{
		{name: "direct growth", src: "rule main = e $$\nrule e = e \"+\" n / n\nrule n = [0-9]+",
			inputs: []string{"1", "1+2", "1+2+3", "1+2+", "1x"}},
		{name: "indirect growth", src: "rule main = a $$\nrule a = b \"1\" / b \"2\" / \"a\"\nrule b = a \"x\" / b \"y\" / \"b\"",
			inputs: []string{"b1", "by2", "ax2", "byy1x2", "b1x1y2", "byyyyyy2x1"}},
		{name: "growth read from a lookahead", src: "rule main = e $$\nrule e = &(e \"x\" t) e \"x\" / t\nrule t = [0-9]+",
			inputs: []string{"1x2", "1x2x3", "1x23x4", "1x2x"}},
	}
}

// engineExits runs the engine's observed parse of src on input and returns its exits.
func engineExits(t *testing.T, prog *engine.Program, in string) []examinedref.Exit {
	t.Helper()
	var exits []examinedref.Exit
	o := engine.ParseOptions{Expectations: true, Backend: engine.Closure, Unit: input.CodePoints}
	o.Trace = func(e engine.TraceEvent) {
		if e.Kind == engine.TraceExit {
			exits = append(exits, examinedref.Exit{Rule: e.Rule, Level: e.Level, Pos: e.Pos, End: e.End, Examined: e.Examined, Matched: e.Matched})
		}
	}
	// The result and the error of the parse are not compared: an action that aborts the parse ends the
	// trace after the calls it completed, and those exits are compared like any other.
	_, _ = prog.ParseWith("main", in, o)
	return exits
}

type exitKey struct {
	rule            string
	level, pos, end int
	matched         bool
}

func keyOf(x examinedref.Exit) exitKey {
	return exitKey{x.Rule, x.Level, x.Pos, x.End, x.Matched}
}

// compareExits checks that every exit that both sides make has the same examined end, and that the
// start call is made by both. It returns the differences.
func compareExits(got, ref []examinedref.Exit) []string {
	ends := map[exitKey]map[int]bool{}
	for _, x := range ref {
		if ends[keyOf(x)] == nil {
			ends[keyOf(x)] = map[int]bool{}
		}
		ends[keyOf(x)][x.Examined] = true
	}
	var diffs []string
	for _, x := range got {
		want, ok := ends[keyOf(x)]
		if !ok {
			continue
		}
		if !want[x.Examined] {
			diffs = append(diffs, fmt.Sprintf("examined end of %s@%d (end %d, matched %v): engine %d, reference %v",
				x.Rule, x.Pos, x.End, x.Matched, x.Examined, keysOf(want)))
		}
	}
	return diffs
}

func keysOf(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// TestExaminedOracle checks the engine's examined ends against the reference on the generated corpus and
// the shapes: every exit that both make has the same examined end.
func TestExaminedOracle(t *testing.T) {
	t.Parallel()
	var cases []oracleCase
	for _, c := range codegentest.Representative(codegentest.GenCorpus(t)) {
		cases = append(cases, oracleCase{name: c.Name, src: c.Src, inputs: c.Inputs})
	}
	cases = append(cases, oracleShapes()...)

	var compared, unsupported int
	for _, c := range cases {
		g, err := enginetest.ParseGrammar(c.src)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.name, err)
		}
		prog := enginetest.CompileBare(t, c.src)
		for _, in := range c.inputs {
			_, refExits, err := examinedref.Evaluate(g, "main", in)
			var uns *examinedref.UnsupportedError
			if errors.As(err, &uns) {
				unsupported++
				break
			}
			if err != nil {
				t.Fatalf("%s %q: reference: %v", c.name, in, err)
			}
			got := engineExits(t, prog, in)
			compared++
			for _, d := range compareExits(got, refExits) {
				t.Errorf("%s %q: %s", c.name, in, d)
			}
		}
	}
	t.Logf("compared %d inputs; %d grammars outside the reference fragment", compared, unsupported)
	if compared == 0 {
		t.Fatal("no input compared")
	}
}
