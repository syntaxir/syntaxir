//go:build !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// chargeTableUnits holds the compute units of the charge cases, worked out by hand from the
// grammar-level cost (Cost in spec/runtime/parsing.md), one total for each input of the case. The
// inputs are ASCII, so both position units give the same totals:
//
//   - the input as a whole: one unit for each unit of its length;
//   - each call, the entry call included: one unit, and the lookup of a memoized rule one unit for
//     each variable of its key and for each byte of its string values;
//   - each evaluation of a rule body: one unit;
//   - each position that a terminal examines, the end of the input included, and the test of the
//     end after the start rule: one unit;
//   - each evaluation of the element of a repetition, the last one included: one unit.
//
// The other charge cases are left out until their items are made exact.
var chargeTableUnits = map[string][]uint64{
	// I0 + entry call and body 2 + iterations + examined positions + the end test after main.
	"literal":             {23, 18, 5, 4},
	"class repetition":    {14, 5, 15},
	"class sequence":      {17, 26, 13},
	"choice of sequences": {8, 10, 8},
	"repeat or class":     {22, 29, 23},
	// item is memoized (called from two places, calls rules) with an empty key; word and num are not.
	"rules":     {49, 29, 19},
	"lookahead": {21, 18},
}

// TestChargeTable checks the units of the reference backend against chargeTableUnits, in both
// position units, so that every backend, which the agreement tests compare with the closure
// backend, is held to the units of the grammar and not to those of one backend.
func TestChargeTable(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range chargeCases {
		want, ok := chargeTableUnits[c.name]
		if !ok {
			continue
		}
		if len(want) != len(c.inputs) {
			t.Fatalf("%s: %d totals for %d inputs", c.name, len(want), len(c.inputs))
		}
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				ref := engineChargeBackends(t, c.grammar, unit)[0]
				for i, in := range c.inputs {
					if got := chargeTotal(ref.run, in); got != want[i] {
						reportChargeDivergence(t, c.name, "table", fmt.Sprintf("input %d (%d bytes): %s %d, table %d", i, len(in), ref.name, got, want[i]))
					}
				}
			})
		}
	}
}
