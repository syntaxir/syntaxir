//go:build sxir_long

package print_test

import (
	"bytes"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// TestActionChargeGoldensDetectBrokenCharges checks that the action expression charges notice generated Go that
// charges other than the library for a lowered action expression. Each mutant edits the generated parsers of a case set
// and must occur in them, and the Go charges of the node and the typed parser must change:
//   - the elements that a list, a tuple or a concat makes count no unit: the units of the golden change;
//   - a member read of a capture passes the bare capture name instead of the name cut from the member
//     name, so that the controlled comparison with the names of the captures can take its constant path
//     for the same string data;
//   - the fields that a struct literal sets charge no step.
//
// The last two change no compute unit, and need not: a comparison counts the length of the shorter string,
// which both names share, and a struct is no list (spec/runtime/parsing.md, Compute Units). The budget that
// the Document form charges notices them, so they run on the Document form against the unmutated Document form.
func TestActionChargeGoldensDetectBrokenCharges(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cutName := regexp.MustCompile(`"\$(\w+)"\[1:\]`)
	for _, m := range []struct {
		name, golden, prefix string
		cases                func() []codegentest.ActionCase
		document             bool
		mutate               func([]byte) ([]byte, int)
	}{
		{"pushed elements count no unit", "testdata/action_charges_push.golden", "actionchargespush", codegentest.PushActionCases, false, dropElementUnits},
		{"a capture member passes the bare name", "", "actionchargesmember", codegentest.MemberActionCases, true,
			func(src []byte) ([]byte, int) {
				return cutName.ReplaceAll(src, []byte(`"$1"`)), len(cutName.FindAllIndex(src, -1))
			}},
		{"struct fields charge no step", "", "actionchargesstruct", codegentest.StructActionCases, true, dropElementSteps},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			cases := actionChargeCases(t, m.cases())
			hits := map[string]*atomic.Int64{"go-node": {}, "go-typed": {}}
			got := goActionCharges(t, m.prefix, cases, m.document, func(target string, src []byte) []byte {
				out, n := m.mutate(src)
				hits[target].Add(int64(n))
				return out
			})
			// The parts that differ from the golden, or on the Document form from the unmutated parsers.
			var parts []string
			against := m.golden
			if m.document {
				against = "the unmutated Document form"
				want := goActionCharges(t, m.prefix, cases, true, nil)
				for k, v := range want {
					if got[k] != v {
						parts = append(parts, k)
					}
				}
			} else {
				parts = actionChargesGoldenFile(m.golden, "", "").Differences(t, got)
			}
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
// the unit of each element (c.P.Units++) and the count of the elements it pushes at once (c.P.UnitsSized),
// returning the code and the number of counts removed.
func dropElementUnits(src []byte) ([]byte, int) {
	const step, sized = "c.P.Units++\n", "c.P.UnitsSized("
	n := bytes.Count(src, []byte(step)) + bytes.Count(src, []byte(sized))
	out := strings.ReplaceAll(string(src), step, "\n")
	return []byte(strings.ReplaceAll(out, sized, "_ = (")), n
}

// dropElementSteps removes the step that the Document form of generated Go charges for each element a lowered
// action expression makes (c.P.Spend()), returning the code and the number of steps removed.
func dropElementSteps(src []byte) ([]byte, int) {
	const step = "c.P.Spend()\n"
	n := bytes.Count(src, []byte(step))
	return []byte(strings.ReplaceAll(string(src), step, "\n")), n
}
