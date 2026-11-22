package print_test

import (
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
)

// actionChargePairs are pairs of grammars whose second action expression handles one value more than the first at a
// place where a predicate or a binding forgets what its action expression left: the nodes an action expression created, when the
// action expression succeeds or fails, and the values it gathered, when it fails. A concat gathers the elements of
// its lists in place, so one more element of its first list, before the second fails, is one more
// value gathered and forgotten. delta is what the second spends more than the first on the input "a",
// on both entries: building and gathering the value, and the unit of forgetting it, which a library that
// forgets without charging leaves out (see TestGeneratedActionChargeMutants).
//
// typedParse is the same for the parse entry of a typed parser (ParseValue), whose Created and KidStack hold
// typed values and cost less to forget than nodes; its recognize entry runs the node parser's code. engine is
// the same for the engine, in every backend and entry. The three differ today, since every target charges
// the values of an action expression in its own way; they are pinned apart so that a change in any is a stated change.
var actionChargePairs = []struct {
	name, one, two string
	delta          int64
	typedParse     int64
	engine         int64
}{
	{"a predicate forgets the nodes its action expression created",
		actionPredicate(`list(T{N: 1})`), actionPredicate(`list(T{N: 1}, T{N: 2})`), 7, 5, 6},
	{"a binding forgets the nodes its action expression created",
		actionBinding(`len(list(T{N: 1}))`), actionBinding(`len(list(T{N: 1}, T{N: 2}))`), 7, 5, 6},
	{"a failed predicate forgets the values its action expression gathered",
		actionFailedPredicate(`concat(list(1), list(10 / (1 - len($x))))`), actionFailedPredicate(`concat(list(1, 2), list(10 / (1 - len($x))))`), 3, 2, 2},
	{"a failed binding forgets the values its action expression gathered",
		actionFailedBinding(`concat(list(1), list(10 / (1 - len($x))))`), actionFailedBinding(`concat(list(1, 2), list(10 / (1 - len($x))))`), 3, 2, 2},
	{"a failed predicate forgets the nodes its action expression created",
		actionFailedPredicate(`list(T{N: 1}, 10 / (1 - len($x)))`), actionFailedPredicate(`list(T{N: 1}, T{N: 2}, 10 / (1 - len($x)))`), 3, 1, 2},
	{"a failed binding forgets the nodes its action expression created",
		actionFailedBinding(`list(T{N: 1}, 10 / (1 - len($x)))`), actionFailedBinding(`list(T{N: 1}, T{N: 2}, 10 / (1 - len($x)))`), 3, 1, 2},
}

func actionPredicate(actionExpr string) string {
	return "type T = { N int }\n@start\nrule main = {" + actionExpr + "} \"a\" $$\n"
}

func actionBinding(actionExpr string) string {
	return "type T = { N int }\nvar n int = 0\n@start\nrule main = {n := " + actionExpr + "} \"a\" $$ => n\n"
}

// actionFailedPredicate and actionFailedBinding make an action expression that fails on the input "a", whose
// alternative then parses it.
func actionFailedPredicate(actionExpr string) string {
	return "type T = { N int }\n@start\nrule main = x:\"a\" ({len(" + actionExpr + ") > 0} \"!\" / \"\") $$\n"
}

func actionFailedBinding(actionExpr string) string {
	return "type T = { N int }\nvar n int = 7\n@start\nrule main = x:\"a\" ({n := len(" + actionExpr + ")} \"!\" / \"\") $$ => n\n"
}

// actionChargeDeltas builds the pairs of actionChargePairs against the library copy in rt and returns, by
// pair and entry, what the second grammar spends more than the first.
func actionChargeDeltas(t *testing.T, rt string) map[string]int64 {
	t.Helper()
	return actionChargeDeltasWith(t, rt, common.GenOptions{}, "Parse")
}

// actionChargeDeltasWith is actionChargeDeltas for the generator options and entry point of runBudgetProbesWith.
func actionChargeDeltasWith(t *testing.T, rt string, opts common.GenOptions, parse string) map[string]int64 {
	t.Helper()
	var cases []codegentest.Case
	for _, p := range actionChargePairs {
		cases = append(cases, codegentest.Case{Name: p.name + " (one)", Src: p.one, Inputs: []string{"a"}},
			codegentest.Case{Name: p.name + " (two)", Src: p.two, Inputs: []string{"a"}})
	}
	got, problems := budgetTotalsWith(t, rt, "actions", cases, "main", nil, opts, parse)
	for _, p := range problems {
		t.Error(p)
	}
	spend := func(name, entry string) int64 {
		v, ok := got["actions/"+name+" | 0 "+entry]
		if !ok {
			t.Fatalf("no report for %s, %s", name, entry)
		}
		f := strings.Fields(v)
		if f[2] != `"<nil>"` {
			t.Fatalf("%s, %s: the parse fails: %s", name, entry, v)
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	deltas := map[string]int64{}
	for _, p := range actionChargePairs {
		for _, entry := range []string{"parse", "recognize"} {
			deltas[p.name+" | "+entry] = spend(p.name+" (two)", entry) - spend(p.name+" (one)", entry)
		}
	}
	return deltas
}

// actionChargeWant returns the deltas of actionChargePairs in the form of actionChargeDeltas.
func actionChargeWant() map[string]int64 {
	want := map[string]int64{}
	for _, p := range actionChargePairs {
		for _, entry := range []string{"parse", "recognize"} {
			want[p.name+" | "+entry] = p.delta
		}
	}
	return want
}

// actionChargeWantTyped returns the deltas of actionChargePairs for a typed parser, in the form of
// actionChargeDeltas: its parse entry is typedParse, its recognize entry the node parser's delta.
func actionChargeWantTyped() map[string]int64 {
	want := map[string]int64{}
	for _, p := range actionChargePairs {
		want[p.name+" | parse"], want[p.name+" | recognize"] = p.typedParse, p.delta
	}
	return want
}

// TestGeneratedActionChargesPerValue checks the charges of generated Go for each value that a predicate
// or a binding forgets, which the totals of the budget goldens mix with everything else a parse spends.
func TestGeneratedActionChargesPerValue(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	got := actionChargeDeltas(t, codegentest.LibCopy())
	for k, want := range actionChargeWant() {
		if got[k] != want {
			t.Errorf("%s: one more value costs %d units, want %d", k, got[k], want)
		}
	}
}

// TestGeneratedTypedActionChargesPerValue is TestGeneratedActionChargesPerValue for the typed library: the
// predicate and the binding of a typed parser (TParser) forget the values of an action expression in their own code.
func TestGeneratedTypedActionChargesPerValue(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	got := actionChargeDeltasWith(t, codegentest.LibCopy(), common.GenOptions{Types: true}, "ParseValue")
	for k, want := range actionChargeWantTyped() {
		if got[k] != want {
			t.Errorf("%s: one more value costs %d units, want %d", k, got[k], want)
		}
	}
}

// TestEngineActionChargesPerValue pins the same deltas for the engine, whose closure backend and bytecode VMs
// charge alike here, with and without building the result.
func TestEngineActionChargesPerValue(t *testing.T) {
	t.Parallel()
	used := func(src string, b engine.Backend, recognize bool) int64 {
		prog := compileBare(t, src)
		var n uint64
		_, err := prog.ParseWith("main", "a", engine.ParseOptions{Backend: b, Recognize: recognize, Limits: runtime.Limits{Compute: 1 << 50},
			Trace: func(e engine.TraceEvent) { n = state.TraceParser(e).Control.Used }})
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return int64(n)
	}
	for _, p := range actionChargePairs {
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				if got := used(p.two, b, recognize) - used(p.one, b, recognize); got != p.engine {
					t.Errorf("%s (backend %d, recognize %v): one more value costs %d units, want %d", p.name, b, recognize, got, p.engine)
				}
			}
		}
	}
}
