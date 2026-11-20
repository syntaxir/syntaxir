//go:build !zp_budget_controls_reference

package engine

import (
	"errors"
	"slices"
	"testing"

	"zeroparse.dev/zp/internal/runtime/state"
	"zeroparse.dev/zp/pkg/runtime"
)

// computeSweep returns, for a parse of input at each compute limit from 1 up, the distinct sizes of the
// reservations of more than one unit that fail, in the order of the limits, and the least limit at which
// the parse succeeds: the units it charges.
func computeSweep(t *testing.T, prog *Program, input string, b Backend) (sizes []uint64, total uint64) {
	t.Helper()
	for l := uint64(1); l < 20000; l++ {
		_, err := prog.ParseWith("main", input, ParseOptions{Backend: b, Limits: runtime.Limits{Compute: l}})
		var le *runtime.LimitError
		if err == nil || !errors.As(err, &le) {
			if err != nil {
				t.Fatalf("limit %d: %v", l, err)
			}
			return sizes, l
		}
		if le.Requested > 1 && (len(sizes) == 0 || sizes[len(sizes)-1] != le.Requested) {
			sizes = append(sizes, le.Requested)
		}
	}
	t.Fatal("the parse does not succeed within 20000 units")
	return nil, 0
}

// TestRunSiteListChargesNothing pins that a hot run site charges nothing for the list of its walk, on the
// closure and both bytecode backends alike: neither the take-off of the values that the walk gathered nor
// their copy into its record is an item of the cost (spec/runtime/parsing.md#cost). At a hot span of one
// the restarted lists of the grammar turn hot, and the input has walks of 7 and 6 values, whose take-offs
// were reservations of 14 and 12 units; none may appear, and the total of units that each backend charges
// is fixed. Five walks take the rest of them from a record, which ends them without an iteration, so
// they charge no unit for one. The bytecode machines charge ten reads of the input more than the
// closure backend, the reads of the class [x].
func TestRunSiteListChargesNothing(t *testing.T) {
	defer func(n, m int) { state.RunHotSpan, state.ScanHotSpan = n, m }(state.RunHotSpan, state.ScanHotSpan)
	state.RunHotSpan, state.ScanHotSpan = 1, 1
	prog := mustCompile(t, `rule main = (a "y" / "q" a / [x])* $$
rule a = x+
rule x = "x"`)
	const input = "xxxxxxxxx"
	for _, c := range []struct {
		backend Backend
		total   uint64
	}{{Closure, 288}, {Bytecode, 298}, {BytecodeIterative, 298}} {
		t.Run(c.backend.String(), func(t *testing.T) {
			sizes, total := computeSweep(t, prog, input, c.backend)
			for _, size := range []uint64{14, 12} {
				if slices.Contains(sizes, size) {
					t.Errorf("a reservation of %d units, a list take-off; the reservations are %v", size, sizes)
				}
			}
			if total != c.total {
				t.Errorf("the parse charges %d units, want %d", total, c.total)
			}
		})
	}
}

// TestFailedRepetitionDropsItsElements pins that a repetition that fails below its minimum makes no list: its
// three gathered elements are dropped for free, as flow.dropKids drops them (spec/flow-ir/operations.md), and
// the list of the second alternative is taken off for free too (spec/runtime/parsing.md#cost). No backend
// makes a reservation of six units, which a take-off of three elements was, and the parse charges the same
// units on every backend.
func TestFailedRepetitionDropsItsElements(t *testing.T) {
	prog := mustCompile(t, "@start\nrule main = [a-z]#repeat(5,) / [a-z]*")
	reservations := func(b Backend) (used []uint64) {
		for l := uint64(1); l < 200; l++ {
			_, err := prog.ParseWith("main", "abc", ParseOptions{Backend: b, Limits: runtime.Limits{Compute: l}})
			var le *runtime.LimitError
			if errors.As(err, &le) && le.Requested == 6 && !slices.Contains(used, le.Used) {
				used = append(used, le.Used)
			}
		}
		return used
	}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) {
			if got := reservations(b); len(got) != 0 {
				t.Errorf("reservations of six units after %v units, want none", got)
			}
			if _, total := computeSweep(t, prog, "abc", b); total != 25 {
				t.Errorf("the parse charges %d units, want 25", total)
			}
		})
	}
}
