//go:build sxir_long

package engine_test

import "testing"

// TestVMUnitsMutantsFail is the negative control of TestChargeAgreementEngine for the bytecode
// machines, which run Flow bodies: each mutant makes a machine count a unit that is no event, miss
// one that is, or skip an alternative whose calls the closure engine counts, so that its total
// differs from the closure engine's on a charge case.
func TestVMUnitsMutantsFail(t *testing.T) {
	const test = "^TestChargeAgreementEngine$"
	runPrintedMutants(t, "internal/runtime/vm/ivm.go", []printedMutant{
		{"the iterative machine counts a plain call twice",
			[]printedEdit{{"func (mc *ivmMachine) plainCallFrame(r *state.Rule, min int) iframe {\n\tp := mc.p\n", "func (mc *ivmMachine) plainCallFrame(r *state.Rule, min int) iframe {\n\tp := mc.p\n\tp.Spend()\n"}},
			test, []string{"."}},
	})
	runPrintedMutants(t, "internal/runtime/vm/flow.go", []printedMutant{
		{"a Flow loop step counts a unit",
			[]printedEdit{{"\t\tcase bytecode.FlowStep:\n", "\t\tcase bytecode.FlowStep:\n\t\t\tp.Spend()\n"}},
			test, []string{"."}},
		{"a Flow advance does not count its input",
			[]printedEdit{{"p.Advance(regs[in.Args[0]].(int))", "p.Pos += regs[in.Args[0]].(int)"}},
			test, []string{"."}},
		{"a Flow recovery builds its error without counting it",
			[]printedEdit{{"p.RecoveryError(regs[in.Args[0]].(int), exps)", "p.MakeError(regs[in.Args[0]].(int), exps)"}},
			test, []string{"."}},
		{"a guard of a literal counts a unit",
			[]printedEdit{{"\t_, hit := p.MatchChar(g.Rune, g.Fold)\n", "\tp.Spend()\n\t_, hit := p.MatchChar(g.Rune, g.Fold)\n"}},
			test, []string{"."}},
		{"a guard of a class counts a unit",
			[]printedEdit{{"\thit := ok && vm.has(p, int32(g.Class), ch)\n", "\tp.Spend()\n\thit := ok && vm.has(p, int32(g.Class), ch)\n"}},
			test, []string{"."}},
	})
	runPrintedMutants(t, "internal/driver/vm.go", []printedMutant{
		{"the Flow table skips alternatives that call rules without counting the calls",
			[]printedEdit{{"Guards: lowerdata.GuardsEvent,", "Guards: lowerdata.GuardsKeep,"}},
			test, []string{"."}},
	})
}
