package corepass_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestHiddenRulesFollowTheirHolder pins the leader of a cycle of left calls that passes through the
// hidden rule of an inline action. The hidden rules of a rule are declared right after it, so the
// rule that holds an inline action, which lies on every cycle through its hidden rules, leads them.
// The control declares the same rule explicitly before its holder, which makes it the leader: a
// lowering that placed hidden rules before their holder would fail the first case.
func TestHiddenRulesFollowTheirHolder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		leaders   map[string]bool
	}{
		{"an inline action on the cycle", `
rule s = e
rule e = (e "+" n => 1) / n
rule n = [0-9]
`, map[string]bool{"e": true}},
		{"its hidden rule declared before its holder", `
rule s = e
rule _e_1 = e "+" n => 1
rule e = _e_1 / n
rule n = [0-9]
`, map[string]bool{"_e_1": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, err := compiletest.Parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			prog, err := compiletest.Compile(g, check.Options{})
			if err != nil {
				t.Fatal(err)
			}
			m, err := elaborate.CoreFrom(prog, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range m.Rules {
				r.Policy = core.Policy{}
			}
			f := corepass.Compute(m, nil)
			got := map[string]bool{}
			cyclic := 0
			for _, r := range m.Rules {
				x := f.Rules[r.ID]
				if x.Cyclic() {
					cyclic++
				}
				if x.Leader {
					got[r.Name] = true
				}
				if x.Leader != x.LeaderIns {
					t.Errorf("rule %s: leader %v without token insertion, %v with it", r.Name, x.Leader, x.LeaderIns)
				}
			}
			if cyclic != 2 {
				t.Errorf("%d cyclic rules, want 2 (the holder and its hidden rule)", cyclic)
			}
			if len(got) != len(tc.leaders) {
				t.Errorf("leaders %v, want %v", got, tc.leaders)
			}
			for name := range tc.leaders {
				if !got[name] {
					t.Errorf("leaders %v, want %v", got, tc.leaders)
				}
			}
		})
	}
}
