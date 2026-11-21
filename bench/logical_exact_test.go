package bench

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gjsonevent "syntaxir.dev/sxir/internal/bench/parsers/jsonevent"
	gjsonlogical "syntaxir.dev/sxir/internal/bench/parsers/jsonlogical"
	gjsontransient "syntaxir.dev/sxir/internal/bench/parsers/jsontransient"
	gpolicyevent "syntaxir.dev/sxir/internal/bench/parsers/policyevent"
	gpolicylogical "syntaxir.dev/sxir/internal/bench/parsers/policylogical"
	gxmlevent "syntaxir.dev/sxir/internal/bench/parsers/xmlevent"
	gxmllogical "syntaxir.dev/sxir/internal/bench/parsers/xmllogical"
	gxmltransient "syntaxir.dev/sxir/internal/bench/parsers/xmltransient"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	rt "syntaxir.dev/sxir/pkg/runtime/libgo"
)

// The exactness of the prototype of logical compute units (design record 076): the generated code
// with its static sums (the charges of kept dispatch guards) against the per-event reference, which
// counts each event where it happens; a sweep of compute limits; and the overshoot of a stopped
// parse against K + B.

type unitsParse func(string, ...rt.ParseOption) error

type exactCase struct {
	name           string
	inputs         []string
	logical, event [2]unitsParse // Parse, ParseValue
}

func exactCases() []exactCase {
	ps := func(n func(string, ...rt.ParseOption) error, v func(string, ...rt.ParseOption) error) [2]unitsParse {
		return [2]unitsParse{n, v}
	}
	return []exactCase{
		{"JSON", exactInputs(inputs.JSONInput(4<<10), []string{`1`, `true`, `null`, `"ab"`, `[1,2]`, `{"a":[1,{"b":null}]}`, `"é\n"`, `[1,]`, `{"a" 1}`, `tru`, ``, ` `, `[[[[`, `"\q"`, `-0.5e+3`, `01`}),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gjsonlogical.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gjsonlogical.ParseValue(s, o...); return err }),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gjsonevent.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gjsonevent.ParseValue(s, o...); return err })},
		{"XML", exactInputs(inputs.XMLInput(4<<10), []string{`<a/>`, `<a b="c">t</a>`, `<?xml version="1.0"?><a><!-- c --><b/></a>`, `<a><![CDATA[x]]></a>`, `<a>&amp;&#65;</a>`, `<a></b>`, `<a`, ``, `<a b=c/>`}),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gxmllogical.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gxmllogical.ParseValue(s, o...); return err }),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gxmlevent.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gxmlevent.ParseValue(s, o...); return err })},
		// The measurement variant that keeps guards through transient rules is exact outside
		// Documents, which the prototype refuses.
		{"JSON transient", exactInputs(inputs.JSONInput(4<<10), nil),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gjsontransient.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gjsontransient.ParseValue(s, o...); return err }),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gjsonevent.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gjsonevent.ParseValue(s, o...); return err })},
		{"XML transient", exactInputs(inputs.XMLInput(4<<10), nil),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gxmltransient.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gxmltransient.ParseValue(s, o...); return err }),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gxmlevent.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gxmlevent.ParseValue(s, o...); return err })},
		// The grammar has no types, so both entries are Parse.
		{"policy", policyInputs(),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gpolicylogical.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gpolicylogical.Parse(s, o...); return err }),
			ps(func(s string, o ...rt.ParseOption) error { _, err := gpolicyevent.Parse(s, o...); return err },
				func(s string, o ...rt.ParseOption) error { _, err := gpolicyevent.Parse(s, o...); return err })},
	}
}

// policyInputs returns inputs of bench/grammars/logical_policy.sxir where the guards skip calls of
// the memoized rule word at many positions before and between words, so that the policy counts of
// those skipped calls decide when word becomes eager, and with it how many of its calls are memo
// hits (TestLogicalPolicyCounts shows that they do).
func policyInputs() []string {
	word := "a" + strings.Repeat("q", 30) + "."
	return []string{
		strings.Repeat("c", 2000) + strings.Repeat(word, 200),
		strings.Repeat("c"+word, 300),
		strings.Repeat("cccccccccccccccccccc"+word, 100),
		strings.Repeat(word, 50) + strings.Repeat("c", 3000) + strings.Repeat(word, 50),
		strings.Repeat("c", 500) + "b" + word + "x",
	}
}

// exactInputs returns the small inputs, the document, its prefixes at a few cuts and copies of it with
// one byte replaced at random, which fail at different points and backtrack differently.
func exactInputs(doc string, small []string) []string {
	out := append([]string{doc}, small...)
	r := rand.New(rand.NewPCG(1, 2))
	for _, cut := range []int{1, 7, len(doc) / 3, len(doc) / 2, len(doc) - 1} {
		out = append(out, doc[:cut])
	}
	for range 40 {
		b := []byte(doc)
		b[r.IntN(len(b))] = "{}[]<>\"',: \n=/&;-0a"[r.IntN(20)]
		out = append(out, string(b))
	}
	return out
}

func countUnits(f unitsParse, in string, unit sxirinput.PositionUnit, opts ...rt.ParseOption) (uint64, error) {
	var n uint64
	err := f(in, append([]rt.ParseOption{rt.WithUnit(unit), rt.WithUnitsReport(&n)}, opts...)...)
	return n, err
}

var exactUnits = []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints}

// TestLogicalExact checks the totals of the generated code against the per-event reference on
// valid, truncated and damaged inputs, with no limit.
func TestLogicalExact(t *testing.T) {
	unlimited := rt.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited})
	for _, c := range exactCases() {
		failed := 0
		for i, in := range c.inputs {
			for _, unit := range exactUnits {
				// Parse and ParseValue run separate generated functions and library paths (untyped and
				// typed); the plain evaluation, and so the total, is the same.
				pn, _ := countUnits(c.logical[0], in, unit, unlimited)
				pv, _ := countUnits(c.logical[1], in, unit, unlimited)
				if pn != pv {
					t.Errorf("%s input %d (%v): Parse %d units, ParseValue %d", c.name, i, unit, pn, pv)
				}
				for api := range 2 {
					got, gerr := countUnits(c.logical[api], in, unit, unlimited)
					want, werr := countUnits(c.event[api], in, unit, unlimited)
					if got != want || (gerr == nil) != (werr == nil) {
						t.Errorf("%s input %d (%v, api %d): %d units (err %v), reference %d (err %v)", c.name, i, unit, api, got, gerr, want, werr)
					}
					if gerr != nil {
						failed++
					}
				}
			}
		}
		t.Logf("%s: %d inputs, %d failing parses", c.name, len(c.inputs), failed)
	}
}

// K is the bound of the units between two checks, apart from a fused loop's chunk (B, none here):
// max(4096, the longest literal) in design record 076.
const exactK = 4096

// TestLogicalLimitSweep sets compute limits across the range of each parse's total: a limit at or
// above the total accepts the parse with its outcome, one below it stops the parse with a
// LimitError whose Used exceeds the limit by at most K + B.
func TestLogicalLimitSweep(t *testing.T) {
	var worst uint64
	worstAt := ""
	for _, c := range exactCases() {
		for i, in := range c.inputs {
			for _, unit := range exactUnits {
				for api := range 2 {
					total, base := countUnits(c.logical[api], in, unit, rt.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
					limits := []uint64{total, total + 1, total - 1, 1, 2}
					for k := uint64(1); k < 64; k++ {
						limits = append(limits, total*k/64)
					}
					if total < 400 {
						for l := uint64(1); l <= total+1; l++ {
							limits = append(limits, l)
						}
					}
					for _, l := range limits {
						if l == 0 {
							continue
						}
						used, err := countUnits(c.logical[api], in, unit, rt.WithLimits(sxirruntime.Limits{Compute: l}))
						var le *sxirruntime.LimitError
						stopped := errors.As(err, &le)
						where := fmt.Sprintf("%s input %d (%v, api %d) limit %d of %d", c.name, i, unit, api, l, total)
						switch {
						case l >= total && (stopped || (err == nil) != (base == nil)):
							t.Errorf("%s: outcome changed: %v (without a limit: %v)", where, err, base)
						case l < total && !stopped:
							t.Errorf("%s: not stopped: %v", where, err)
						case stopped && (le.Kind != sxirruntime.LimitCompute || le.Limit != l || le.Used <= l || le.Used != used):
							t.Errorf("%s: wrong LimitError %+v (count %d)", where, le, used)
						case stopped && le.Used-l > worst:
							worst, worstAt = le.Used-l, where
						}
					}
				}
			}
		}
	}
	t.Logf("largest overshoot %d units (%s); K + B = %d", worst, worstAt, exactK)
	if worst > exactK {
		t.Errorf("overshoot %d exceeds K + B = %d", worst, exactK)
	}
}

// TestLogicalPolicyCounts shows that the policy counts of the calls that guards skip change the
// totals of bench/grammars/logical_policy.sxir, so that TestLogicalExact checks that kept guards
// reproduce them: a run of skips before the words delays the point where word becomes eager, and
// the total of the input is more than that of the skips and that of the words.
func TestLogicalPolicyCounts(t *testing.T) {
	w := "a" + strings.Repeat("q", 30) + "."
	skips, words := strings.Repeat("c", 2000), strings.Repeat(w, 200)
	for _, p := range []struct {
		name  string
		parse unitsParse
	}{
		{"logical", func(s string, o ...rt.ParseOption) error { _, err := gpolicylogical.Parse(s, o...); return err }},
		{"event", func(s string, o ...rt.ParseOption) error { _, err := gpolicyevent.Parse(s, o...); return err }},
	} {
		total := func(s string) uint64 {
			n, err := countUnits(p.parse, s, sxirinput.Bytes, rt.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
			if err != nil {
				t.Fatal(p.name, err)
			}
			return n
		}
		both, apart := total(skips+words), total(skips)+total(words)-total("")
		t.Logf("%s: skips then words %d units, apart %d", p.name, both, apart)
		if both <= apart {
			t.Errorf("%s: the skipped calls did not delay the memoization of word: %d units, apart %d", p.name, both, apart)
		}
	}
}
