//go:build sxir_long && !sxir_budget_controls_reference

package engine_test

import "testing"

// TestEngineLimitErrorsMutants checks that TestEngineLimitErrors fails for an engine whose compute
// LimitError requests a unit, whose check stops a parse that used no more than the limit, or whose
// bytecode machine counts the steps of its loops, which the closure backend does not count.
func TestEngineLimitErrorsMutants(t *testing.T) {
	t.Parallel()
	const tests = "^TestEngineLimitErrors$"
	t.Run("engine", func(t *testing.T) {
		runPrintedMutants(t, "internal/runtime/state/budget_control.go", []printedMutant{
			{"a limit error requests a unit",
				[]printedEdit{{"\t\t\tUsed: c.Used - c.Doc, Hotspot: c.hotspot(),\n", "\t\t\tUsed: c.Used - c.Doc, Requested: 1, Hotspot: c.hotspot(),\n"}}, tests, []string{"."}},
			{"a check stops a parse at the limit",
				[]printedEdit{
					{"c.Limits.Compute != 0 && c.Used > c.Limits.Compute {\n\t\tc.chargeSlow()", "c.Limits.Compute != 0 && c.Used >= c.Limits.Compute {\n\t\tc.chargeSlow()"},
					{"if c.Limits.Compute != 0 && c.Used > c.Limits.Compute {\n\t\tpanic(", "if c.Limits.Compute != 0 && c.Used >= c.Limits.Compute {\n\t\tpanic("},
				}, tests, []string{"."}},
		})
	})
	t.Run("vm", func(t *testing.T) {
		runPrintedMutants(t, "internal/runtime/vm/flow.go", []printedMutant{{"a loop step counts a unit",
			[]printedEdit{{"\t\tcase bytecode.FlowStep:\n", "\t\tcase bytecode.FlowStep:\n\t\t\tp.SpendN(1)\n"}},
			tests, []string{"."}}})
	})
}
