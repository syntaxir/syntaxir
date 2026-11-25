//go:build sxir_long

package print_test

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// TestActionChargeGoldensDetectBrokenCharges checks that the action expression charges notice generated Go that
// charges other than the library for a lowered action expression. Each mutant edits the generated parsers of a case set
// and must occur in them, and the Go charges of the node and the typed parser must differ from the golden: the
// elements that a list, a tuple or a concat makes count no unit.
func TestActionChargeGoldensDetectBrokenCharges(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	for _, m := range []struct {
		name, golden, prefix string
		cases                func() []codegentest.ActionCase
		mutate               func([]byte) ([]byte, int)
	}{
		{"pushed elements count no unit", "testdata/action_charges_push.golden", "actionchargespush", codegentest.PushActionCases, dropElementUnits},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			cases := actionChargeCases(t, m.cases())
			hits := map[string]*atomic.Int64{"go-node": {}, "go-typed": {}}
			got := goActionCharges(t, m.prefix, cases, func(target string, src []byte) []byte {
				out, n := m.mutate(src)
				hits[target].Add(int64(n))
				return out
			})
			// The parts that differ from the golden.
			against := m.golden
			parts := actionChargesGoldenFile(m.golden, "", "").Differences(t, got)
			differ := map[string]int{}
			for _, part := range parts {
				// The target is the last word of the part's name, with -recognize for the node parser's Recognize.
				target := part[strings.LastIndex(part, " ")+1:]
				target = strings.TrimSuffix(target, "-recognize")
				differ[target]++
				if differ[target] <= 3 {
					t.Logf("%s differs from %s", part, against)
				}
			}
			// The mutant must apply to, and change the charges of, the node and the typed parser alike: a
			// pattern that only one of them prints would leave the other's charges unchecked.
			for _, target := range []string{"go-node", "go-typed"} {
				switch n := hits[target].Load(); {
				case n == 0:
					t.Errorf("the mutant %q matches no generated %s parser", m.name, target)
				case differ[target] == 0:
					t.Errorf("the mutant %q (%d edits) changes no %s charge against %s", m.name, n, target, against)
				}
			}
		})
	}
}

// dropElementUnits removes the units that generated Go counts for the elements a lowered action expression makes:
// the unit of each element (c.P.Fuel--) and the count of the elements it pushes at once (c.P.UnitsSized),
// returning the code and the number of counts removed.
func dropElementUnits(src []byte) ([]byte, int) {
	const step, sized = "c.P.Fuel--\n", "c.P.UnitsSized("
	n := bytes.Count(src, []byte(step)) + bytes.Count(src, []byte(sized))
	out := strings.ReplaceAll(string(src), step, "\n")
	return []byte(strings.ReplaceAll(out, sized, "_ = (")), n
}
