//go:build sxir_long && !sxir_budget_controls_reference

package engine_test

import "testing"

// TestRepetitionListTakeOffMutants is the negative control of the tests that the lists the parse builds
// charge nothing: a closure backend whose Parser.Kids charges the take-off of a repetition's elements, and
// bytecode backends that charge it where a repetition ends with its list, fail
// TestRuntimeValuesChargeNothing, and a hot run site that charges the take-off of its walk's list fails
// TestRunSiteListChargesNothing.
func TestRepetitionListTakeOffMutants(t *testing.T) {
	t.Parallel()
	const tests = "TestRuntimeValuesChargeNothing/repetition"
	t.Run("closure", func(t *testing.T) {
		runPrintedMutants(t, "internal/runtime/state/alloc.go", []printedMutant{{"Kids charges the take-off",
			[]printedEdit{{"\tks := p.Nodes(n)\n\tcopy(ks, p.KidStack[base:])", "\tks := p.Nodes(n)\n\tp.SpendValue(2 * n)\n\tcopy(ks, p.KidStack[base:])"}},
			tests, []string{"."}}})
	})
	t.Run("vm", func(t *testing.T) {
		runPrintedMutants(t, "internal/runtime/vm/vm.go", []printedMutant{{"a repetition's list charges the take-off",
			[]printedEdit{{"\t\t\t\t\tkids = p.Nodes(n)\n", "\t\t\t\t\tkids = p.Nodes(n)\n\t\t\t\t\tp.SpendValue(2 * n)\n"}},
			tests, []string{"."}}})
	})
	t.Run("run site", func(t *testing.T) {
		const tests = "TestRunSiteListChargesNothing"
		runPrintedMutants(t, "internal/runtime/state/skip_runs.go", []printedMutant{
			{"a run site charges the take-off of its list",
				[]printedEdit{{"func (p *Parser) SkipKids() []*value.Node {\n", "func (p *Parser) SkipKids() []*value.Node {\n\tp.SpendValue(2 * len(p.walk().vals))\n"}}, tests, []string{"."}},
			{"a run site charges placing its values in its records",
				[]printedEdit{{"place := func(dst []*value.Node) { copy(dst, vals) }", "place := func(dst []*value.Node) { CopySpend(p, dst, vals) }"}}, tests, []string{"."}},
		})
	})
}
