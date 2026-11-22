//go:build sxir_long && !sxir_budget_controls_reference

package engine

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestLimitErrorAgreementMutants checks that generated Go that counts a unit for each element it takes
// off the list of a repetition, which counts nothing, decides differently from generated Go without the
// count, which TestLimitErrorAgreement compares with the closure backend. The engine's mutants are in
// TestEngineLimitErrorsMutants.
func TestLimitErrorAgreementMutants(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	// The list of a repetition is taken off without a unit: generated Go that counts a unit for each
	// element it takes off fails limits at which generated Go without the count succeeds. The take-off
	// ends the parse, so the decisions are compared at every limit.
	t.Run("counted repetition list take-off", func(t *testing.T) {
		t.Parallel()
		codegentest.NeedsBudgetControls(t)
		const takeOff = "\tks := p.Nodes(len(p.KidStack) - base)\n"
		runtime := codegentest.LibCopyWith(t, func(file, src string) string {
			if file != "lib.go" {
				return src
			}
			if strings.Count(src, takeOff) != 1 {
				t.Fatal("the take-off of a list is not in lib.go")
			}
			return strings.Replace(src, takeOff, "\tp.Control.Charge(uint64(len(p.KidStack) - base))\n"+takeOff, 1)
		}, nil)
		var c limitAgreementCase
		for _, x := range limitAgreementCases {
			if x.name == "repetition" {
				c = x
			}
		}
		free, _ := generatedGoLimitReports(t, c, codegentest.LibCopy())
		node, _ := generatedGoLimitReports(t, c, runtime)
		if len(limitDecisionDisagreements(free, node)) == 0 {
			t.Error("generated Go that counts the take-off of a repetition's list decides what generated Go without the count does")
		}
	})
}
