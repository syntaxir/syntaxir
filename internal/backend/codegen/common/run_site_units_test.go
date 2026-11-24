package common_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestRunSiteUnitsAgree checks that generated Go and TypeScript parsers count the units of the
// engine's parse on the run-site grammars, on inputs long enough for their sites to turn hot at the
// span of the charge model (RunHotSpan), so that walks reuse records and copy their suffixes: a
// suffix copy counts a unit for each element it copies on every backend (spec/runtime/parsing.md,
// Compute Units). The corpus of TestUnitsAgree has no input that turns a site hot. The negative
// control builds the Go parsers against a library whose suffix copies count nothing, and expects a
// difference, so the inputs reach the copies.
func TestRunSiteUnitsAgree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated Go and TypeScript parsers")
	}
	if !state.BudgetControlsEnabled {
		t.Skip("the reference build counts no compute units")
	}
	codegentest.NeedsBudgetControls(t)
	cases := codegentest.RunSiteCases(4, true)
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		for _, m := range codegentest.GeneratedParsersUnitsMismatches(t, goBin, cases, common.GenOptions{}, nil) {
			t.Error(m)
		}
	})
	t.Run("go without suffix copies", func(t *testing.T) {
		t.Parallel()
		const charge = "p.unitsSized(n) // a suffix copy"
		lib := codegentest.LibCopyWith(t, func(name, src string) string {
			if name != "skip_runs_lib.go" {
				return src
			}
			if !strings.Contains(src, charge) {
				t.Fatalf("libgo/skip_runs_lib.go has no %q", charge)
			}
			return strings.Replace(src, charge, "_ = n // a suffix copy", 1)
		}, nil)
		if ms := codegentest.GeneratedParsersUnitsMismatchesWithLib(t, goBin, lib, cases, common.GenOptions{}, nil); len(ms) == 0 {
			t.Error("a library whose suffix copies count no unit agrees with the engine")
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
