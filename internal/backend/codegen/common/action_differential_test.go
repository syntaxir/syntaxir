package common_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// actionDifferentialCases is the number of action expressions of each seed. The tests that run in the process
// (the reference evaluator and the engine's backends) read actionProcessSeeds seeds, and the tests that
// build generated parsers actionGeneratedSeeds, which is larger under -tags sxir_long; SXIR_ACTION_SEEDS
// sets both for a longer search. The default of the first kills the mutants of the arithmetic that
// the few seeds of a build did not reach (a floored remainder, a division that floors).
const actionDifferentialCases = 60

const actionProcessSeedsDefault = 200

func envSeeds(def uint64) uint64 {
	if n, err := strconv.ParseUint(os.Getenv("SXIR_ACTION_SEEDS"), 10, 64); err == nil && n > 0 {
		return n
	}
	return def
}

var (
	actionProcessSeeds   = envSeeds(actionProcessSeedsDefault)
	actionBackendSeeds   = envSeeds(20)
	actionGeneratedSeeds = envSeeds(actionGeneratedSeedsDefault)
)

// wellTypedActionCases returns the generated action cases of the seed that the compiler accepts, and the
// number it rejected, which fails the test: the generator makes action expressions that are well typed by construction.
func wellTypedActionCases(t *testing.T, seed uint64, n int, unset bool) (cases []codegentest.ActionCase, rejected int) {
	t.Helper()
	return wellTypedCases(t, codegentest.ActionCases(seed, n, unset))
}

// wellTypedWideActionCases is wellTypedActionCases for the action expressions of codegentest.ActionCasesWide.
func wellTypedWideActionCases(t *testing.T, seed uint64, n int, unset bool) (cases []codegentest.ActionCase, rejected int) {
	t.Helper()
	return wellTypedCases(t, codegentest.ActionCasesWide(seed, n, unset))
}

func wellTypedCases(t *testing.T, all []codegentest.ActionCase) (cases []codegentest.ActionCase, rejected int) {
	t.Helper()
	for _, c := range all {
		g, err := parseTest(c.Src)
		if err == nil {
			_, err = entry.Compile(g, entry.Options{})
		}
		if err != nil {
			// The generator makes action expressions that are well typed by construction: a rejection is a fault of it,
			// in every test that reads its action expressions.
			t.Errorf("rejected: %v\n%s", err, c.Src)
			rejected++
			continue
		}
		cases = append(cases, c)
	}
	return cases, rejected
}

// actionSeedCases returns the well-typed action expressions of seed: the narrow action expressions for the seeds up to half, the
// wide action expressions of codegentest.ActionCasesWide for the others, so that a test with 2*half seeds reads both.
func actionSeedCases(t *testing.T, seed, half uint64, unset bool) (cases []codegentest.ActionCase, rejected int) {
	t.Helper()
	if seed > half {
		return wellTypedWideActionCases(t, seed-half, actionDifferentialCases, unset)
	}
	return wellTypedActionCases(t, seed, actionDifferentialCases, unset)
}

func actionCaseList(cs []codegentest.ActionCase) []codegentest.Case {
	out := make([]codegentest.Case, len(cs))
	for i, c := range cs {
		out[i] = c.Case
	}
	return out
}

// TestActionEngineBackends checks that the engine's closure backend, its bytecode VM and its iterative
// VM give one value or one error for every generated action expression, in both position units, and that the VMs
// running the program saved to a file and loaded again give it too. Their charges differ
// by design today (runtime.Limits: counts depend on backend); the test only requires that every backend
// reports one and logs how far apart the backends are.
func TestActionEngineBackends(t *testing.T) {
	t.Parallel()
	for seed := uint64(1); seed <= 2*actionBackendSeeds; seed++ {
		cases, _ := actionSeedCases(t, seed, actionBackendSeeds, true)
		for _, c := range cases {
			g, _ := parseTest(c.Src)
			prog, err := entry.Compile(g, entry.Options{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := prog.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := engine.LoadProgram(data)
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			for _, in := range c.Inputs {
				for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
					var want string
					for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
						// The units the parse counted, reported at its end, also when an action fails it: the
						// call of a rule counts after the trace reports its entry, which may be the last event.
						var used uint64
						n, err := prog.ParseWith("main", in, engine.ParseOptions{Backend: b, Unit: unit, Limits: sxirruntime.Limits{Compute: 1 << 50},
							ComputeReport: &used})
						got := codegentest.ResultJSON(n, err)
						if used == 0 {
							t.Errorf("%s: backend %d reports no charges", c.Name, b)
						}
						if b == engine.Closure {
							want = got
						} else if got != want {
							t.Errorf("%s: input %q unit %d: backend %d gives\n %s\nclosure gives\n %s", c.Name, in, unit, b, got, want)
						}
					}
					for _, b := range []engine.Backend{engine.Bytecode, engine.BytecodeIterative} {
						n, err := loaded.ParseWith("main", in, engine.ParseOptions{Backend: b, Unit: unit, Limits: sxirruntime.Limits{Compute: 1 << 50}})
						if got := codegentest.ResultJSON(n, err); got != want {
							t.Errorf("%s: input %q unit %d: loaded backend %d gives\n %s\nclosure gives\n %s", c.Name, in, unit, b, got, want)
						}
					}
				}
			}
		}
	}
}

// TestActionGeneratedTargets checks that generated Go (node and typed) and generated TypeScript give the
// engine's value or error for every generated action expression, in both position units. It runs in one fixture
// form in every tier (codegentest.OneMode): its thousands of action expressions make it the largest test of the
// long tier, and TestActionFixedGeneratedTargets and the negative controls below run the action expressions in both
// forms.
func TestActionGeneratedTargets(t *testing.T) {
	t.Parallel()
	codegentest.OneMode(t, testActionGeneratedTargets)
}

func testActionGeneratedTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	list := func(unset bool) []codegentest.Case {
		var all []codegentest.ActionCase
		for seed := uint64(1); seed <= 2*actionGeneratedSeeds; seed++ {
			cases, _ := actionSeedCases(t, seed, actionGeneratedSeeds, unset)
			all = append(all, cases...)
		}
		// The fixed action expressions read every kind of member, whether or not the constructor set it.
		cases := append(actionCaseList(all), actionCaseList(codegentest.MemberActionCases())...)
		for i := range cases {
			cases[i].Name = fmt.Sprintf("%d %s", i, cases[i].Name)
		}
		return cases
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := list(true)
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
	// A typed parser reads a field that a constructor leaves out as the zero value of its Type, like the
	// others, so the typed comparison reads unset fields too.
	codegentest.GeneratedTypesCorpus(t, goBin, list(true), common.GenOptions{ConvertTypes: true}, 0)
}

// TestActionFixedGeneratedTargets checks the fixed action expression sets of codegentest (conversions, structs, casts,
// list and tuple values, iteration, pushes and scalar operators) on generated Go (node and typed) and generated
// TypeScript: each gives the engine's value or error for every input, in both position units. The
// generated action expressions of TestActionGeneratedTargets do not reach every form these sets pin, and the charge
// goldens of the sets compare only the outcome, not the value.
func TestActionFixedGeneratedTargets(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testActionFixedGeneratedTargets)
}

func testActionFixedGeneratedTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	all, _ := wellTypedCases(t, fixedActionCases())
	cases := actionCaseList(all)
	for i := range cases {
		cases[i].Name = fmt.Sprintf("%d %s", i, cases[i].Name)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true}, 0)
}

// TestActionDifferentialDetectsBrokenTargets is the negative control of TestActionGeneratedTargets: with an
// operator of the generated Go or TypeScript parsers swapped for another, a failure of an action expression left
// unraised, the right operand of a logical operator always evaluated or an equality of texts negated,
// the comparison with the engine fails on some generated or fixed scalar action expression.
func TestActionDifferentialDetectsBrokenTargets(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.EachMode(t, testActionDifferentialDetectsBrokenTargets)
}

func testActionDifferentialDetectsBrokenTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases, _ := wellTypedActionCases(t, 1, actionDifferentialCases, true)
	// The fixed scalar action expressions fail, short-circuit and compare texts, which the generated action expressions rarely do.
	scalar, _ := wellTypedCases(t, codegentest.ScalarActionCases())
	list := actionCaseList(append(cases, scalar...))
	for i := range list {
		list[i].Name = fmt.Sprintf("%d %s", i, list[i].Name)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	// Each mutant changes one pattern of the code, which must occur in the generated code. Every action expression of
	// these cases is printed from Exec IR, which calls the compositions with the constant of the operator.
	for _, m := range []struct{ name, from, to string }{
		{"lowered: plus becomes minus", `c.P.ActionBinary(rt.OpAdd,`, `c.P.ActionBinary(rt.OpSub,`},
		{"lowered: times becomes plus", `c.P.ActionBinary(rt.OpMul,`, `c.P.ActionBinary(rt.OpAdd,`},
		{"lowered: quotient becomes product", `c.P.ActionBinary(rt.OpQuo,`, `c.P.ActionBinary(rt.OpMul,`},
		{"lowered: remainder becomes quotient", `c.P.ActionBinary(rt.OpRem,`, `c.P.ActionBinary(rt.OpQuo,`},
		{"lowered: less becomes less or equal", `c.P.ActionBinary(rt.OpLt,`, `c.P.ActionBinary(rt.OpLe,`},
		{"lowered: equal becomes not equal", `c.P.ActionBinary(rt.OpEq,`, `c.P.ActionBinary(rt.OpNe,`},
		{"lowered: negation becomes not", `c.P.ActionUnary(rt.OpNeg,`, `c.P.ActionUnary(rt.OpNot,`},
		{"lowered: not becomes negation", `c.P.ActionUnary(rt.OpNot,`, `c.P.ActionUnary(rt.OpNeg,`},
		{"lowered: end is read as start", `rt.MemberEnd,`, `rt.MemberStart,`},
		{"lowered: a failure is not returned", `return nil, tf`, `_ = tf`},
		{"lowered: a logical operator always evaluates its right operand", `if !tb`, `if true || !tb`},
		{"lowered: a logical operator always evaluates its right operand (||)", `if tb`, `if true || tb`},
		{"lowered: a text equality is negated", `any(c.P.TextEqual(`, `any(!c.P.TextEqual(`},
	} {
		hits := 0
		got := codegentest.GeneratedParsersMismatches(t, goBin, list, common.GenOptions{}, func(code []byte) []byte {
			s, hit := mutateEitherForm(string(code), m.from, m.to)
			if hit {
				hits++
			}
			return []byte(s)
		})
		t.Logf("Go: %s: parsers changed %d of %d cases, mismatches %d", m.name, hits, len(list), len(got))
		if hits == 0 {
			t.Errorf("Go: %s: the generated code has no %q", m.name, m.from)
		} else if len(got) == 0 {
			t.Errorf("Go: %s: no generated action expression notices", m.name)
		}
	}
	node := tooltest.RequireNode(t)
	for _, m := range []struct{ name, from, to string }{
		{"lowered: plus becomes minus", `c.p.actionBinary(0,`, `c.p.actionBinary(1,`},
		{"lowered: times becomes plus", `c.p.actionBinary(2,`, `c.p.actionBinary(0,`},
		{"lowered: quotient becomes product", `c.p.actionBinary(3,`, `c.p.actionBinary(2,`},
		{"lowered: remainder becomes quotient", `c.p.actionBinary(4,`, `c.p.actionBinary(3,`},
		{"lowered: less becomes less or equal", `c.p.actionBinary(7,`, `c.p.actionBinary(8,`},
		{"lowered: equal becomes not equal", `c.p.actionBinary(5,`, `c.p.actionBinary(6,`},
		{"lowered: negation becomes not", `c.p.actionUnary(0,`, `c.p.actionUnary(1,`},
		{"lowered: not becomes negation", `c.p.actionUnary(1,`, `c.p.actionUnary(0,`},
		{"lowered: length becomes zero", `c.p.actionLength(`, `((x: Value): [Value, Value] => (c.p.actionLength(x), [0, null]))(`},
		{"lowered: a failure is not returned", `return [null, tf`, `[null, tf`},
		{"lowered: a logical operator always evaluates its right operand", `if (!tb`, `if (true || !tb`},
		{"lowered: a logical operator always evaluates its right operand (||)", `if (tb`, `if (true || tb`},
	} {
		hits := 0
		got := codegentest.TSParsersMismatches(t, node, list, common.GenOptions{}, func(code string) string {
			code, hit := mutateEitherForm(code, m.from, m.to)
			if hit {
				hits++
			}
			return code
		})
		t.Logf("TypeScript: %s: parsers changed %d of %d cases, mismatches %d", m.name, hits, len(list), len(got))
		if hits == 0 {
			t.Errorf("TypeScript: %s: the generated code has no %q", m.name, m.from)
		} else if len(got) == 0 {
			t.Errorf("TypeScript: %s: no generated action expression notices", m.name)
		}
	}
}

// TestActionConstructorsDetectBrokenTargets is the negative control of TestActionFixedGeneratedTargets for
// the constructors printed from Exec IR (tuples, lists, concat and structs) on generated Go (node and
// typed) and TypeScript parsers: each mutant changes one pattern of the lowered code or, for typed Go, of
// the struct constructors it calls, which must occur, and the comparison with the engine (for typed Go,
// also with the parser that converts its nodes) on the fixed struct, hull, push and cast action expressions must then
// fail. The control changes nothing and must find no mismatch.
func TestActionConstructorsDetectBrokenTargets(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.EachMode(t, testActionConstructorsDetectBrokenTargets)
}

func testActionConstructorsDetectBrokenTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var all []codegentest.ActionCase
	for _, set := range [][]codegentest.ActionCase{codegentest.StructActionCases(), codegentest.HullActionCases(), codegentest.PushActionCases(),
		codegentest.CastActionCases()} {
		all = append(all, set...)
	}
	cases, _ := wellTypedCases(t, all)
	list := actionCaseList(cases)
	for i := range list {
		list[i].Name = fmt.Sprintf("%d %s", i, list[i].Name)
	}
	type mutant struct{ name, from, to string }
	check := func(target string, m mutant, hits int, got []string) {
		t.Logf("%s: %s: parsers changed %d of %d cases, mismatches %d", target, m.name, hits, len(list), len(got))
		switch {
		case hits == 0:
			t.Errorf("%s: %s: the generated code has no %q", target, m.name, m.from)
		case m.from == m.to && len(got) != 0:
			t.Errorf("%s: %s: %d mismatches without a change: %v", target, m.name, len(got), got)
		case m.from != m.to && len(got) == 0:
			t.Errorf("%s: %s: no generated action expression notices", target, m.name)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, m := range []mutant{
		{"control: unchanged", `c.P.ActionHull(c, `, `c.P.ActionHull(c, `},
		{"a tuple becomes a list", `c.P.ActionHull(c, true, `, `c.P.ActionHull(c, false, `},
		{"a list becomes a tuple", `c.P.ActionHull(c, false, `, `c.P.ActionHull(c, true, `},
		{"concat pushes no elements", `c.P.ActionPushItems(c, "concat", `, `c.P.ActionPushItems(c, "concat", any(nil)); _ = (`},
		{"an element is pushed as nil", `c.P.ActionPush(c, "list", `, `c.P.ActionPush(c, "list", any(nil)); _ = (`},
		{"a field is set to nil", `c.ActionSetField(`, `c.ActionSetFieldNil(`},
		{"a struct ignores the ranges of its fields", `c.P.ActionCover(`, `actionCoverFirst(c.P, `},
	} {
		hits := 0
		got := codegentest.GeneratedParsersMismatches(t, goBin, list, common.GenOptions{}, func(code []byte) []byte {
			s, hit := mutateEitherForm(string(code), m.from, m.to)
			if hit {
				hits++
				if strings.Contains(m.to, "ActionSetFieldNil") {
					s += "\nfunc tsfNil(c *rt.ActCtx, n *Node, name string, _ any) { c.ActionSetField(n, name, nil) }\n"
					s = strings.ReplaceAll(s, "c.ActionSetFieldNil(", "tsfNil(c, ")
				}
				if strings.HasPrefix(m.to, "actionCoverFirst(") {
					// Each field is covered as if it were the first, so the range is that of the last node.
					s += "\nfunc actionCoverFirst(p *rt.Parser, n *Node, v any, _ bool) bool { return p.ActionCover(n, v, true) }\n"
				}
			}
			return []byte(s)
		})
		check("Go", m, hits, got)
	}
	// Typed Go: the typed parsers against the parsers that convert their nodes and against the engine.
	for _, m := range []mutant{
		{"control: unchanged", `c.P.ActionHull(c, `, `c.P.ActionHull(c, `},
		{"a tuple becomes a list", `c.P.ActionHull(c, true, `, `c.P.ActionHull(c, false, `},
		{"a list becomes a tuple", `c.P.ActionHull(c, false, `, `c.P.ActionHull(c, true, `},
		{"concat pushes no elements", `c.P.ActionPushItems(c, "concat", `, `c.P.ActionPushItems(c, "concat", any(nil)); _ = (`},
		{"an element is pushed as nil", `c.P.ActionPush(c, "list", `, `c.P.ActionPush(c, "list", any(nil)); _ = (`},
		// The constructor takes its first two fields in the other order, as a call that passed them
		// out of declaration order would.
		{"two fields of a struct are swapped", `final bool, f0 any, f1 any`, `final bool, f1 any, f0 any`},
		{"a struct ignores the ranges of its fields", `return c.MadeOf(v, `, `return c.MadeOf(v) // `},
	} {
		hits := 0
		got := codegentest.GeneratedTypesMismatches(t, goBin, list, common.GenOptions{ConvertTypes: true}, func(code []byte) []byte {
			s, hit := mutateEitherForm(string(code), m.from, m.to)
			if hit {
				hits++
			}
			return []byte(s)
		}, 0)
		check("typed Go", m, hits, got)
	}
	node := tooltest.RequireNode(t)
	for _, m := range []mutant{
		{"control: unchanged", `c.p.actionHullLogical(c, `, `c.p.actionHullLogical(c, `},
		{"a tuple becomes a list", `c.p.actionHullLogical(c, true, `, `c.p.actionHullLogical(c, false, `},
		{"a list becomes a tuple", `c.p.actionHullLogical(c, false, `, `c.p.actionHullLogical(c, true, `},
		{"concat pushes no elements", `c.p.actionPushItems(c, "concat", `, `c.p.actionPushItems(c, "concat", null); void (`},
		{"an element is pushed as nil", `c.p.actionPush(c, "list", `, `c.p.actionPush(c, "list", null); void (`},
		{"a field is set to nil", `c.actionSetField(`, `((n: Node, f: string, _v: Value) => c.actionSetField(n, f, null))(`},
		{"a struct ignores the ranges of its fields", `c.p.actionCover(`, `((n: Node, v: Value, _f: boolean) => c.p.actionCover(n, v, true))(`},
	} {
		hits := 0
		got := codegentest.TSParsersMismatches(t, node, list, common.GenOptions{}, func(code string) string {
			code, hit := mutateEitherForm(code, m.from, m.to)
			if hit {
				hits++
			}
			return code
		})
		check("TypeScript", m, hits, got)
	}
}

// logicalCalls are the library calls that a generated parser that counts logical compute units makes under
// the name with the suffix Logical (design record 076): the hull of a list or tuple, the list that map makes
// of a projection and, in Go, the equality of two texts. The form of the fixtures decides whether a parser
// counts them, so a mutant of such a call must change it under either name.
var logicalCalls = []string{"c.P.ActionHull(", "c.p.actionHull(", "c.NewList(", "c.newList(", "c.P.TextEqual("}

// mutateEitherForm returns code with from replaced by to, and whether code has from. If from makes one of
// logicalCalls, the call under its Logical name, with to making the same call under that name, counts as
// from as well.
func mutateEitherForm(code, from, to string) (string, bool) {
	pairs := []string{from, to}
	for _, call := range logicalCalls {
		if strings.Contains(from, call) {
			logical := strings.TrimSuffix(call, "(") + "Logical("
			pairs = append(pairs, strings.Replace(from, call, logical, 1), strings.Replace(to, call, logical, 1))
		}
	}
	hit := false
	for i := 0; i < len(pairs); i += 2 {
		hit = hit || strings.Contains(code, pairs[i])
	}
	return strings.NewReplacer(pairs...).Replace(code), hit
}

// TestActionIterationsDetectBrokenTargets is the negative control of TestActionFixedGeneratedTargets for the
// loops of map, filter, foldl and foldr printed from Exec IR on generated Go (node and typed) and TypeScript
// parsers: each mutant changes one pattern of the lowered loops, which must occur, and the comparison with
// the engine on the fixed iteration action expressions must then fail. The control changes nothing and must find no
// mismatch.
func TestActionIterationsDetectBrokenTargets(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.EachMode(t, testActionIterationsDetectBrokenTargets)
}

// iterMutantHelpers are the functions that the mutants of TestActionIterationsDetectBrokenTargets call in
// generated Go, for the node and the typed parser alike.
const iterMutantHelpers = `
func itemAtFirst[S any, P interface{ ActionItemAt(S, int, bool) any }](p P, items S, i int, _ bool) any {
	return p.ActionItemAt(items, i, false)
}
func itemAtLast[S any, P interface{ ActionItemAt(S, int, bool) any }](p P, items S, i int, _ bool) any {
	return p.ActionItemAt(items, i, true)
}
func keepAll[P interface{ ActionFilterKeep(any) (bool, any) }](p P, v any) (bool, any) {
	_, f := p.ActionFilterKeep(v)
	return true, f
}
`

func testActionIterationsDetectBrokenTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases, _ := wellTypedCases(t, codegentest.IterActionCases())
	list := actionCaseList(cases)
	for i := range list {
		list[i].Name = fmt.Sprintf("%d %s", i, list[i].Name)
	}
	type mutant struct{ name, from, to string }
	check := func(target string, m mutant, hits int, got []string) {
		t.Logf("%s: %s: parsers changed %d of %d cases, mismatches %d", target, m.name, hits, len(list), len(got))
		switch {
		case hits == 0:
			t.Errorf("%s: %s: the generated code has no %q", target, m.name, m.from)
		case m.from == m.to && len(got) != 0:
			t.Errorf("%s: %s: %d mismatches without a change: %v", target, m.name, len(got), got)
		case m.from != m.to && len(got) == 0:
			t.Errorf("%s: %s: no generated action expression notices", target, m.name)
		}
	}
	goMutants := []mutant{
		{"control: unchanged", `c.P.ActionItemAt(`, `c.P.ActionItemAt(`},
		{"the folds take the elements from the first", `c.P.ActionItemAt(`, `itemAtFirst(c.P, `},
		{"the iterations take the elements from the last", `c.P.ActionItemAt(`, `itemAtLast(c.P, `},
		{"filter keeps every element", `c.P.ActionFilterKeep(`, `keepAll(c.P, `},
		{"map pushes nil for every element", `c.P.ActionPush(c, "map", `, `c.P.ActionPush(c, "map", any(nil)); _ = (`},
		{"map takes no elements", `c.P.ActionItems("map", `, `c.P.ActionItems("map", any(nil)); _ = (`},
		{"foldl takes no elements", `c.P.ActionItems("foldl", `, `c.P.ActionItems("foldl", any(nil)); _ = (`},
	}
	mutate := func(m mutant, hits *int) func(code []byte) []byte {
		return func(code []byte) []byte {
			s := string(code)
			if strings.Contains(s, m.from) {
				*hits++
				s = strings.ReplaceAll(s, m.from, m.to) + iterMutantHelpers
			}
			return []byte(s)
		}
	}
	// The map of a projected repetition makes a list of no elements. A typed parser holds node and typed
	// actions, which print the call as any(c.NewList(...)) and (c.NewList(...)), of the elements in a local
	// named ti; a parser that counts logical compute units calls it as c.NewListLogical (logicalCalls).
	noList := mutant{"the map of a projected repetition makes no elements", `c.NewList(ti`, `newListNone(c, ti`}
	mutateGo := func(m mutant, hits *int) func(code []byte) []byte {
		if m != noList {
			return mutate(m, hits)
		}
		return func(code []byte) []byte {
			s := strings.ReplaceAll(string(code), `c.NewListLogical(ti`, m.from)
			if strings.Contains(s, m.from) {
				*hits++
				s = strings.ReplaceAll(s, "any("+m.from, "any("+m.to)
				s = strings.ReplaceAll(s, m.from, "newListNoneT(c, ti")
				s += "\nfunc newListNone(c *rt.ActCtx, _ []*Node) *Node { return c.NewList(nil) }\n" +
					"func newListNoneT(c *rt.TActCtx, _ []any) any { return c.NewList(nil) }\n"
			}
			return []byte(s)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, m := range append(goMutants, noList) {
		hits := 0
		got := codegentest.GeneratedParsersMismatches(t, goBin, list, common.GenOptions{}, mutateGo(m, &hits))
		check("Go", m, hits, got)
	}
	for _, m := range append(goMutants, noList) {
		hits := 0
		got := codegentest.GeneratedTypesMismatches(t, goBin, list, common.GenOptions{ConvertTypes: true}, mutateGo(m, &hits), 0)
		check("typed Go", m, hits, got)
	}
	node := tooltest.RequireNode(t)
	for _, m := range []mutant{
		{"control: unchanged", `c.p.actionItemAt(`, `c.p.actionItemAt(`},
		{"the folds take the elements from the first", `c.p.actionItemAt(`, `((it: readonly (Node | null)[], i: number, _r: boolean) => c.p.actionItemAt(it, i, false))(`},
		{"the iterations take the elements from the last", `c.p.actionItemAt(`, `((it: readonly (Node | null)[], i: number, _r: boolean) => c.p.actionItemAt(it, i, true))(`},
		{"filter keeps every element", `c.p.actionFilterKeep(`, `((v: Value): [boolean, Value] => [true, c.p.actionFilterKeep(v)[1]])(`},
		{"map pushes nil for every element", `c.p.actionPush(c, "map", `, `c.p.actionPush(c, "map", null); void (`},
		{"map takes no elements", `c.p.actionItems("map", `, `c.p.actionItems("map", null); void (`},
		{"foldl takes no elements", `c.p.actionItems("foldl", `, `c.p.actionItems("foldl", null); void (`},
		{"the map of a projected repetition makes no elements", `c.newListLogical(`, `c.newListLogical([]); void (`},
	} {
		hits := 0
		got := codegentest.TSParsersMismatches(t, node, list, common.GenOptions{}, func(code string) string {
			code, hit := mutateEitherForm(code, m.from, m.to)
			if hit {
				hits++
			}
			return code
		})
		check("TypeScript", m, hits, got)
	}
}

// TestActionPassesDetectBrokenTargets is the negative control of the comparisons of generated parsers
// with the engine for the action expressions that the Action passes of the targets rewrite: the action
// expressions of typed Go rules that compare a field without boxing it (field-compare), the predicates
// that compare a captured text with literals, whose test of the literals gives way to the action
// expression as written under limit controls (text-dispatch), and the TypeScript action expressions with
// a cast to _, which checks nothing. Each mutant changes
// one pattern of the generated code, which must occur, and the comparison must then fail. Whether a parse
// runs under limit controls depends on the form of the fixtures, so each mutant of one of the two paths also
// replaces the test of the controls, so that the path it changes runs; a control that only does that, and one
// that changes nothing, must find no mismatch.
func TestActionPassesDetectBrokenTargets(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.EachMode(t, testActionPassesDetectBrokenTargets)
}

func testActionPassesDetectBrokenTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var literal []codegentest.Case
	for _, c := range codegentest.GenCorpus(t) {
		if strings.HasPrefix(c.Name, "literal text ") {
			literal = append(literal, c)
		}
	}
	fieldCompared := []codegentest.Case{
		{Name: "text fields", Src: codegentest.TextCompareGrammar, Inputs: codegentest.TextCompareInputs},
		{Name: "arithmetic", Src: "type T = { S string; N int }\nrule main = t:item $$ => $t.N == $t.N * 2 - 3 && $t.S != \"y\"\n" +
			"rule item T = s:[a-z] #atomic => T{S: text($s), N: 3}", Inputs: []string{"x", "y", ""}},
	}
	// A mutant replaces force, if any, by forced, and from by to.
	type mutant struct{ name, force, forced, from, to string }
	run := func(target string, cases []codegentest.Case, m mutant, mismatches func(mutate func(string) string) []string) {
		hits := 0
		got := mismatches(func(s string) string {
			if !strings.Contains(s, m.from) || m.force != "" && !strings.Contains(s, m.force) {
				return s
			}
			hits++
			if m.force != "" {
				s = strings.ReplaceAll(s, m.force, m.forced)
			}
			return strings.ReplaceAll(s, m.from, m.to)
		})
		t.Logf("%s: %s: parsers changed %d of %d cases, mismatches %d", target, m.name, hits, len(cases), len(got))
		switch {
		case hits == 0:
			t.Errorf("%s: %s: the generated code has no %q", target, m.name, m.from)
		case m.from == m.to && len(got) != 0:
			t.Errorf("%s: %s: %d mismatches without a change: %v", target, m.name, len(got), got)
		case m.from != m.to && len(got) == 0:
			t.Errorf("%s: %s: no generated action expression notices", target, m.name)
		}
	}
	goBytes := func(mutate func(string) string) func([]byte) []byte {
		return func(code []byte) []byte { return []byte(mutate(string(code))) }
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	// The Go parsers run under the default limit controls, so the action expression as written runs, and the mutants
	// of the test of the literals force it.
	const goForce, goForced = `if c.P.Control == nil {`, `if true {`
	for _, m := range []mutant{
		{"control: unchanged", "", "", `c.P.ActionBinary(rt.OpEq,`, `c.P.ActionBinary(rt.OpEq,`},
		{"a predicate's equality becomes not equal", "", "", `c.P.ActionBinary(rt.OpEq,`, `c.P.ActionBinary(rt.OpNe,`},
		{"a predicate's or does not decide", "", "", `if !tb`, `if tb`},
		{"control: the literals are tested", goForce, goForced, `case "a", `, `case "a", `},
		{"a tested literal is not matched", goForce, goForced, `case "a", `, `case "\x00", `},
	} {
		run("Go", literal, m, func(mutate func(string) string) []string {
			return codegentest.GeneratedParsersMismatches(t, goBin, literal, common.GenOptions{}, goBytes(mutate))
		})
	}
	for _, m := range []mutant{
		{"control: unchanged", "", "", `c.ActionCompare(rt.OpEq,`, `c.ActionCompare(rt.OpEq,`},
		{"a field equality becomes not equal", "", "", `c.ActionCompare(rt.OpEq,`, `c.ActionCompare(rt.OpNe,`},
		{"a field less becomes greater or equal", "", "", `c.ActionCompare(rt.OpLt,`, `c.ActionCompare(rt.OpGe,`},
		{"times becomes plus", "", "", `c.P.ActionBinary(rt.OpMul,`, `c.P.ActionBinary(rt.OpAdd,`},
		{"minus becomes plus", "", "", `c.P.ActionBinary(rt.OpSub,`, `c.P.ActionBinary(rt.OpAdd,`},
	} {
		run("typed Go", fieldCompared, m, func(mutate func(string) string) []string {
			return codegentest.GeneratedTypesMismatches(t, goBin, fieldCompared, common.GenOptions{ConvertTypes: true}, goBytes(mutate), len(fieldCompared))
		})
	}
	node := tooltest.RequireNode(t)
	// The action expression as written runs under limit controls, and the test of the literals without them.
	const tsForce, tsWritten, tsTested = `if (c.p.control === null) {`, `if (false) {`, `if (true) {`
	// TypeScript names an operator by its code: 5 is ==, 6 is !=; 0 is && and 1 is || for logic.
	for _, m := range []mutant{
		{"control: the action expression as written runs", tsForce, tsWritten, `c.p.actionLogic(1,`, `c.p.actionLogic(1,`},
		{"a predicate's equality becomes not equal", tsForce, tsWritten, `c.p.actionBinary(5,`, `c.p.actionBinary(6,`},
		{"a predicate's or does not decide", tsForce, tsWritten, `if (!tb`, `if (tb`},
		{"control: the literals are tested", tsForce, tsTested, `case "a": `, `case "a": `},
		{"a tested literal is not matched", tsForce, tsTested, `case "a": `, `case "\x00": `},
	} {
		run("TypeScript", literal, m, func(mutate func(string) string) []string {
			return codegentest.TSParsersMismatches(t, node, literal, common.GenOptions{}, mutate)
		})
	}
	casts := codegentest.CompareCases()
	for _, m := range []mutant{
		{"control: unchanged", "", "", `c.p.actionBinary(5,`, `c.p.actionBinary(5,`},
		{"a cast action expression's equality becomes not equal", "", "", `c.p.actionBinary(5,`, `c.p.actionBinary(6,`},
		{"a cast action expression's and does not decide", "", "", `if (tb`, `if (!tb`},
	} {
		run("TypeScript", casts, m, func(mutate func(string) string) []string {
			return codegentest.TSParsersMismatches(t, node, casts, common.GenOptions{}, mutate)
		})
	}
}

// referenceAction returns the action expression of the generated case: the action of the rule main, or the predicate
// that precedes the optional "b".
func referenceAction(t *testing.T, c codegentest.ActionCase) ast.ActionExpr {
	t.Helper()
	g, err := parseTest(c.Src)
	if err != nil {
		t.Fatal(err)
	}
	var found ast.ActionExpr
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Predicate:
			found = e.Action
		case *ast.Seq:
			for _, x := range e.Items {
				walk(x)
			}
		case *ast.Choice:
			for _, x := range e.Alts {
				walk(x)
			}
		case *ast.Group:
			walk(e.Expr)
		}
	}
	for _, d := range g.File.Decls {
		if r, ok := d.(*ast.RuleDef); ok && r.Name == "main" {
			if c.Kind == codegentest.ValueAction {
				return r.Action
			}
			walk(r.Expr)
		}
	}
	if found == nil {
		t.Fatalf("%s: no action expression", c.Name)
	}
	return found
}

// TestActionReferenceEvaluator checks the engine's backends against the reference evaluator of action
// expressions (internal/ir/code/exec/ref), which states the semantics of an action expression on naive
// tagged values: the same value or the same error for every generated action expression, in both
// position units. A predicate case compares whether the predicate held, by whether the input "ab"
// parses. The closure backend runs every case; the bytecode VM and its iterative form run the cases of
// the seeds that TestActionEngineBackends reads, so that each runs the action programs of the Action
// module against the reference directly.
func TestActionReferenceEvaluator(t *testing.T) {
	t.Parallel()
	var checked atomic.Int64
	// Each seed is a parallel subtest: the seeds share nothing, and the cases of one seed run in order as before.
	t.Run("seeds", func(t *testing.T) {
		for seed := uint64(1); seed <= 2*actionProcessSeeds; seed++ {
			t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
				t.Parallel()
				cases, _ := actionSeedCases(t, seed, actionProcessSeeds, true)
				backends := []engine.Backend{engine.Closure}
				if half := (seed-1)%actionProcessSeeds + 1; half <= actionBackendSeeds {
					backends = append(backends, engine.Bytecode, engine.BytecodeIterative)
				}
				for _, c := range cases {
					g, _ := parseTest(c.Src)
					prog, err := entry.Compile(g, entry.Options{})
					if err != nil {
						t.Fatal(err)
					}
					actionExpr := referenceAction(t, c)
					for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
						w := utf8.RuneCountInString(c.Lit)
						if unit == input.Bytes {
							w = len(c.Lit)
						}
						env := &ref.ActionEnv{Captures: map[string]ref.V{"x": ref.Node(ref.StringNode, c.Lit, 0, w, nil, nil)}, Bytes: unit == input.Bytes,
							Start: 0, End: w, Line: 1, Col: 1, Zeros: referenceZeros(t, entry.Checked(prog), "R", "Inner", "Leaf")}
						in := c.Lit
						if c.Kind == codegentest.PredicateAction {
							in += "b"
						}
						checkReference(t, c, prog, actionExpr, env, unit, in, false, backends...)
						checked.Add(1)
					}
				}
			})
		}
	})
	if checked.Load() == 0 {
		t.Fatal("no case was checked")
	}
}

// checkReference compares the backends of the engine on the input in with the reference evaluator in env: the
// value or the error of an action, and for a predicate, which precedes an optional "b", whether the predicate
// held, by whether an input that ends in "b" parses. The parse records expectations if expectations is set. A
// case that inserts missing tokens runs on the closure backend only, which backends lists first, with the
// insertion enabled.
func checkReference(t *testing.T, c codegentest.ActionCase, prog *engine.Program, actionExpr ast.ActionExpr, env *ref.ActionEnv, unit input.PositionUnit, in string,
	expectations bool, backends ...engine.Backend) {
	t.Helper()
	for _, m := range referenceMismatches(c, prog, actionExpr, env, unit, in, expectations, backends...) {
		t.Error(m)
	}
}

// referenceMismatches returns the mismatches that checkReference reports, one for each backend that
// does not give what the reference evaluator gives.
func referenceMismatches(c codegentest.ActionCase, prog *engine.Program, actionExpr ast.ActionExpr, env *ref.ActionEnv, unit input.PositionUnit, in string,
	expectations bool, backends ...engine.Backend) []string {
	var out []string
	v, msg := env.Eval(actionExpr)
	missing := 0
	if c.MissingTokens {
		backends, missing = backends[:1], 4
	}
	for _, b := range backends {
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Backend: b, Unit: unit, MissingTokens: missing, Expectations: expectations})
		var recovered value.SyntaxErrors
		if c.Recovers && errors.As(err, &recovered) {
			err = nil // the recovered syntax errors are not what the action expression gives; the value, nil among them, is
		}
		if c.Kind == codegentest.PredicateAction {
			held := err == nil && n != nil
			if want := !strings.HasSuffix(in, "b") || ref.Holds(v, msg); held != want {
				out = append(out, fmt.Sprintf("%s: input %q unit %d backend %d: the engine parses: %v, the reference evaluator: %v (%s)", c.Name, in, unit, b, held, want, msg))
			}
			continue
		}
		want := map[string]any{"node": nil}
		if msg != "" {
			want["err"] = "action in main: " + msg
		} else {
			want["node"] = env.Plain(v)
		}
		wantJSON, _ := json.Marshal(want)
		got := codegentest.ResultJSON(n, err)
		var gotAny, wantAny any
		if json.Unmarshal([]byte(got), &gotAny) != nil || json.Unmarshal(wantJSON, &wantAny) != nil || !reflect.DeepEqual(gotAny, wantAny) {
			out = append(out, fmt.Sprintf("%s: input %q unit %d expectations %v backend %d: the engine gives\n %s\nthe reference evaluator\n %s", c.Name, in, unit,
				expectations, b, got, wantJSON))
		}
	}
	return out
}

// fixedActionCases returns the fixed action expression sets of codegentest other than the members, which
// TestActionGeneratedTargets reads: conversions, structs, casts, list and tuple values, iteration, pushes,
// scalar operators and the action expressions over recovered values.
func fixedActionCases() []codegentest.ActionCase {
	var all []codegentest.ActionCase
	for _, set := range [][]codegentest.ActionCase{codegentest.ConvertActionCases(), codegentest.StructActionCases(), codegentest.CastActionCases(),
		codegentest.HullActionCases(), codegentest.IterActionCases(), codegentest.PushActionCases(), codegentest.ScalarActionCases(), codegentest.ErrorCastActionCases(),
		codegentest.RecoveredActionCases(), codegentest.RecoveredStructActionCases(false)} {
		all = append(all, set...)
	}
	return all
}

// TestActionFixedReference checks the engine's backends (closure, VM and iterative VM) against the reference
// evaluator on the fixed action expression sets, the members and the action expressions over a missing token, its fields and structs that
// hold one (closure only), in both position units, and for a case that recovers, with and without recording
// expectations, which sets the message of a recovered value: the same value or error for every input of an action, and
// whether a predicate held. The captures come from the hand-written tables of the cases, so the
// reference evaluator reads the nodes that the rules define, not the engine's. With
// TestActionFixedGeneratedTargets, which compares the generated targets with the engine on the same
// cases, every target gives the reference value.
func TestActionFixedReference(t *testing.T) {
	t.Parallel()
	checked := 0
	cases := append(append(codegentest.MemberActionCases(), fixedActionCases()...), codegentest.MissingActionCases()...)
	for _, c := range append(append(cases, codegentest.MissingFieldActionCases()...), codegentest.RecoveredStructActionCases(true)...) {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		prog, err := entry.Compile(g, entry.Options{})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		var structs []string
		for _, d := range g.File.Decls {
			if td, ok := d.(*ast.TypeDef); ok {
				if _, ok := td.Type.(*ast.StructType); ok {
					structs = append(structs, td.Name)
				}
			}
		}
		zeros := referenceZeros(t, entry.Checked(prog), structs...)
		actionExpr := referenceAction(t, c)
		recording := []bool{false}
		if c.Recovers {
			recording = append(recording, true)
		}
		for _, in := range c.Inputs {
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				for _, exp := range recording {
					bytes := unit == input.Bytes
					end := utf8.RuneCountInString(in)
					if bytes {
						end = len(in)
					}
					env := &ref.ActionEnv{Captures: referenceCaptures(c.Captures(in), in, bytes, exp), Bytes: bytes, Start: 0, End: end, Line: 1, Col: 1,
						Zeros: zeros}
					checkReference(t, c, prog, actionExpr, env, unit, in, exp, engine.Closure, engine.Bytecode, engine.BytecodeIterative)
					checked++
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no case was checked")
	}
}

// referenceCaptures turns the captures of a fixed case, in characters of the input in, into nodes of the
// reference evaluator in the position unit; a capture that did not match is left out. The message of a
// recovered value is the position where it starts, which is where its error was recorded, as line:column in
// the unit, and its diagnostic if the parse records expectations (expectations), or "syntax error" otherwise
// (spec/runtime/parsing.md, Syntax Errors).
func referenceCaptures(caps map[string]codegentest.Capture, in string, bytes, expectations bool) map[string]ref.V {
	runes := []rune(in)
	offset := func(i int) int {
		if bytes {
			return len(string(runes[:i]))
		}
		return i
	}
	var node func(c codegentest.Capture) ref.V
	node = func(c codegentest.Capture) ref.V {
		switch c.Shape {
		case codegentest.ErrorCapture, codegentest.MissingCapture:
			before := string(runes[:c.From])
			line := 1 + strings.Count(before, "\n")
			col := 1 + offset(c.From) - offset(len([]rune(before[:strings.LastIndex(before, "\n")+1])))
			diagnostic := "syntax error"
			if expectations {
				diagnostic = c.Diagnostic
			}
			message := fmt.Sprintf("%d:%d: %s", line, col, diagnostic)
			if c.Shape == codegentest.ErrorCapture {
				return ref.ErrorValue(string(runes[c.From:c.To]), offset(c.From), offset(c.To), message)
			}
			return ref.MissingToken(offset(c.From), c.Expected, message)
		}
		shape := map[codegentest.CaptureShape]ref.Shape{codegentest.StringCapture: ref.StringNode, codegentest.TupleCapture: ref.TupleNode,
			codegentest.ListCapture: ref.ListNode}
		if c.Shape == codegentest.Unmatched {
			return ref.Nil
		}
		var items []ref.V
		for _, e := range c.Elems {
			items = append(items, node(e))
		}
		return ref.Node(shape[c.Shape], string(runes[c.From:c.To]), offset(c.From), offset(c.To), items, referenceCaptures(c.Caps, in, bytes, expectations))
	}
	out := map[string]ref.V{}
	for name, c := range caps {
		if c.Shape != codegentest.Unmatched {
			out[name] = node(c)
		}
	}
	return out
}

// referenceZeros returns the zero values of the fields of the struct types names that the grammar declares for the reference
// evaluator, decoded from the zero descriptors that the compiled program holds
// (check.Checker.StructZeroOf). A struct field holds the struct with no field set.
func referenceZeros(t testing.TB, prog *frontend.Result, names ...string) map[string]map[string]ref.V {
	t.Helper()
	var decode func(d string) (ref.V, string)
	decode = func(d string) (ref.V, string) {
		switch d[0] {
		case 'i':
			return ref.Int(0), d[1:]
		case 's':
			return ref.Str(""), d[1:]
		case 'b':
			return ref.Bool(false), d[1:]
		case 'n':
			return ref.Nil, d[1:]
		case 'l':
			return ref.List(), d[1:]
		case '(':
			var items []ref.V
			d = d[1:]
			for d[0] != ')' {
				var e ref.V
				e, d = decode(d)
				items = append(items, e)
			}
			return ref.Tuple(items...), d[1:]
		case '{':
			name, rest, _ := strings.Cut(d[1:], "}")
			return ref.V{Kind: ref.KStruct, Type: name, Fields: map[string]ref.V{}}, rest
		}
		t.Fatalf("unknown zero descriptor %q", d)
		return ref.Nil, ""
	}
	zeros := map[string]map[string]ref.V{}
	for _, name := range names {
		sz := prog.Checker().StructZeroOf(name)
		if sz == nil {
			continue // the type is declared by the grammars of some generators only
		}
		zeros[name] = map[string]ref.V{}
		for _, f := range sz.Names {
			if sz.Zeros[f] != check.NoZeroDesc {
				zeros[name][f], _ = decode(sz.Zeros[f])
			}
		}
	}
	return zeros
}
