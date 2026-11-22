package libgo

import "testing"

// TestObsOutcomeWide checks that outcomes whose examined range does not fit in a word get numbers that
// tell equal outcomes from others and from the outcomes that fit.
func TestObsOutcomeWide(t *testing.T) {
	const pos = 1 << 41
	p := &Parser{}
	type outcome struct {
		ok             bool
		examined, from int
	}
	var outcomes []outcome
	for _, d := range []int{0, 5, 1<<30 - 1, 1 << 30, 1 << 31, 1 << 40} {
		for _, ok := range []bool{false, true} {
			outcomes = append(outcomes, outcome{ok, pos + d, pos}, outcome{ok, pos, pos - d}, outcome{ok, pos + d, pos - d})
		}
	}
	outcomes = append(outcomes, outcome{true, pos - 1, pos}, outcome{true, pos, pos + 1})
	ids := map[uint64]outcome{}
	for _, o := range outcomes {
		v := p.obsOutcome(o.ok, pos, o.examined, o.from)
		if again := p.obsOutcome(o.ok, pos, o.examined, o.from); again != v {
			t.Errorf("%+v: number %#x, then %#x", o, v, again)
		}
		if prev, dup := ids[v]; dup && prev != o {
			t.Errorf("%+v and %+v: same number %#x", prev, o, v)
		}
		ids[v] = o
		if v == obsNone {
			t.Errorf("%+v: number %#x is not an outcome", o, v)
		}
	}
}
