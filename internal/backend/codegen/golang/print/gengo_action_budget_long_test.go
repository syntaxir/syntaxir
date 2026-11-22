//go:build sxir_long

package print_test

import (
	"bytes"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// hullUnitsCount is the count of the elements of a list or tuple that an action expression builds
// from the values it gathered, as the library writes it in ActionHullLogical (units.go) for the node
// and the typed Parser alike.
const hullUnitsCount = "p.unitsSized(len(kids))"

// libWithoutHullUnits returns a library copy whose ActionHullLogical builds lists and tuples without
// counting their elements, failing if units.go has no such count.
func libWithoutHullUnits(t *testing.T) string {
	t.Helper()
	patched := false
	rt := codegentest.LibCopyWith(t, func(name, src string) string {
		if name != "units.go" || !strings.Contains(src, hullUnitsCount) {
			return src
		}
		patched = true
		return strings.ReplaceAll(src, hullUnitsCount, "_ = len(kids)")
	}, nil)
	if !patched {
		t.Fatalf("units.go has no %s", hullUnitsCount)
	}
	return rt
}

// pushUnitsCount is the count that the printer writes before the elements an action expression
// pushes in place, such as those of a list literal that a concat gathers.
const pushUnitsCount = "c.P.UnitsSized("

// dropPushUnits makes every printed count of pushed elements in code a plain expression statement,
// as a printer would if it lost the units of the elements it pushes.
func dropPushUnits(code []byte) []byte {
	return bytes.ReplaceAll(code, []byte(pushUnitsCount), []byte("_ = ("))
}

// TestGeneratedActionChargeMutants checks that TestGeneratedActionChargesPerValue notices a parser that
// builds the lists of its action expressions without counting their elements, in each of the two
// places where the count lives: the library's ActionHullLogical, and the count that the printed code
// makes before pushing elements in place. With either dropped, some pair counts other than its units.
func TestGeneratedActionChargeMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	actionCountMutants(t, common.GenOptions{}, "Parse", "")
}

// TestGeneratedTypedActionChargeMutants is TestGeneratedActionChargeMutants for a typed parser, whose
// action expressions build typed values (TActCtx) and count their elements the same way.
func TestGeneratedTypedActionChargeMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	actionCountMutants(t, common.GenOptions{Types: true}, "ParseValue", "typed ")
}

// actionCountMutants runs the two mutants of TestGeneratedActionChargeMutants for the options and
// entry point of actionChargeDeltasWith.
func actionCountMutants(t *testing.T, opts common.GenOptions, parse, form string) {
	t.Run("hull", func(t *testing.T) {
		t.Parallel()
		got := actionChargeDeltasWith(t, libWithoutHullUnits(t), opts, parse, nil)
		requireChargesChange(t, "a "+form+"list or tuple counts no element", got, actionChargeWant())
	})
	t.Run("push", func(t *testing.T) {
		t.Parallel()
		got := actionChargeDeltasWith(t, codegentest.LibCopy(), opts, parse, requireMutated(t, dropPushUnits))
		requireChargesChange(t, "a "+form+"pushed element counts no unit", got, actionChargeWant())
	})
}

// requireMutated returns mutate, failing the test if it changes the code of no parser it is given.
func requireMutated(t *testing.T, mutate func([]byte) []byte) func([]byte) []byte {
	t.Helper()
	changed := false
	t.Cleanup(func() {
		if !changed && !t.Failed() {
			t.Error("the mutant changes no generated parser")
		}
	})
	return func(code []byte) []byte {
		out := mutate(code)
		changed = changed || !bytes.Equal(out, code)
		return out
	}
}

// requireChargesChange fails unless some delta of got differs from want.
func requireChargesChange(t *testing.T, name string, got, want map[string]int64) {
	t.Helper()
	differ := 0
	for k, v := range want {
		if got[k] != v {
			differ++
			t.Logf("%s: %d, unmutated %d", k, got[k], v)
		}
	}
	if differ == 0 {
		t.Errorf("the mutant %q changes no delta", name)
	}
}
