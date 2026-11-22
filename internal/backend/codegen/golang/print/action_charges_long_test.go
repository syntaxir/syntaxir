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

// TestActionChargeGoldensDetectBrokenCharges checks that the action expression charge goldens notice generated Go that
// charges other than the library for a lowered action expression. Each mutant edits the generated parsers of a case set
// and must occur in them, and the Go entries of its golden must change:
//   - a member read of a capture passes the bare capture name instead of the name cut from the member
//     name, so that the controlled comparison with the names of the captures can take its constant path
//     for the same string data;
//   - the elements that a tuple, a list, a concat or a struct literal makes charge no step.
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
		mutate               func([]byte) ([]byte, int)
	}{
		{"a capture member passes the bare name", "testdata/action_charges_member.golden", "actionchargesmember", codegentest.MemberActionCases,
			func(src []byte) ([]byte, int) {
				return cutName.ReplaceAll(src, []byte(`"$1"`)), len(cutName.FindAllIndex(src, -1))
			}},
		{"struct fields charge no step", "testdata/action_charges_struct.golden", "actionchargesstruct", codegentest.StructActionCases, dropElementSteps},
		{"pushed elements charge no step", "testdata/action_charges_push.golden", "actionchargespush", codegentest.PushActionCases, dropElementSteps},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			hits := map[string]*atomic.Int64{"go-node": {}, "go-typed": {}}
			got := goActionCharges(t, m.prefix, actionChargeCases(t, m.cases()), func(target string, src []byte) []byte {
				out, n := m.mutate(src)
				hits[target].Add(int64(n))
				return out
			})
			golden := actionChargesGoldenFile(m.golden, "", "")
			differ := map[string]int{}
			for _, part := range golden.Differences(t, got) {
				// The target is the last word of the part's name, with -recognize for the node parser's Recognize.
				target := part[strings.LastIndex(part, " ")+1:]
				target = strings.TrimSuffix(target, "-recognize")
				differ[target]++
				if differ[target] <= 3 {
					t.Logf("%s differs from %s", part, m.golden)
				}
			}
			// The mutant must apply to, and change the charges of, the node and the typed parser alike: a
			// pattern that only one of them prints would leave the other's goldens unchecked.
			for _, target := range []string{"go-node", "go-typed"} {
				switch n := hits[target].Load(); {
				case n == 0:
					t.Errorf("the mutant %q matches no generated %s parser", m.name, target)
				case differ[target] == 0:
					t.Errorf("the mutant %q (%d edits) changes no %s entry of %s", m.name, n, target, m.golden)
				}
			}
		})
	}
}

// dropElementSteps removes the step that generated Go charges for each element a lowered action expression makes
// (c.P.Spend()), returning the code and the number of steps removed.
func dropElementSteps(src []byte) ([]byte, int) {
	const step = "c.P.Spend()\n"
	n := bytes.Count(src, []byte(step))
	return []byte(strings.ReplaceAll(string(src), step, "\n")), n
}
