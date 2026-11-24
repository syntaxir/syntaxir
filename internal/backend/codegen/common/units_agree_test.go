package common_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestUnitsAgree checks that every implementation counts the same compute units for the same parse
// (spec/runtime/parsing.md, Cost): on the cases of the generation corpus, in both position units,
// the engine's closure backend and its recursive and iterative bytecode machines, and the node and
// typed parses of generated Go and TypeScript parsers, each report the units of the closure
// backend, and the stream parses of generated Go parsers of stream grammars those of the engine's
// stream parse. A count that one implementation keeps and another does not (a call, a memo key, a value,
// an advanced position), or an event that one counts beyond the specification (a body evaluation,
// a repetition, a level of an Operators Rule), shows as a difference on some case.
//
// The default tier runs the representative cases of the corpus (codegentest.Representative), the
// long tier (go test -tags sxir_long) all of them.
func TestUnitsAgree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated Go and TypeScript parsers")
	}
	if !state.BudgetControlsEnabled {
		t.Skip("the reference build counts no compute units")
	}
	codegentest.NeedsBudgetControls(t)
	cases := codegentest.Representative(codegentest.GenCorpus(t))
	t.Run("engine", func(t *testing.T) {
		t.Parallel()
		for _, c := range cases {
			engineUnitsAgree(t, c)
		}
	})
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		for _, m := range codegentest.GeneratedParsersUnitsMismatches(t, goBin, cases, common.GenOptions{}, nil) {
			t.Error(m)
		}
	})
	t.Run("typescript", func(t *testing.T) {
		node := tooltest.RequireNode(t)
		t.Parallel()
		for _, m := range codegentest.TSParsersUnitsMismatches(t, node, cases, common.GenOptions{}, nil) {
			t.Error(m)
		}
	})
}

// engineUnitsAgree reports the inputs of c on which a backend of the engine counts other units than
// the closure backend, compiled as the generated parsers are compared (codegentest.Reference).
func engineUnitsAgree(t *testing.T, c codegentest.Case) {
	t.Helper()
	prog := compileBare(t, c.Src, check.Options{NoProjections: true})
	backends := []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative}
	for i, in := range c.Inputs {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			used := make([]uint64, len(backends))
			for k, b := range backends {
				_, _ = prog.ParseWith("main", in, engine.ParseOptions{Backend: b, Unit: unit, Expectations: true, ComputeReport: &used[k]})
			}
			if used[1] != used[0] || used[2] != used[0] {
				t.Errorf("%s: input %d %s (%v): units: closure %d, bytecode %d, iterative bytecode %d", c.Name, i, fmt.Sprintf("%.40q", in), unit, used[0], used[1], used[2])
			}
		}
	}
}
