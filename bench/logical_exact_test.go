package bench

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gchainevent "syntaxir.dev/sxir/internal/bench/parsers/chainevent"
	gchainlogical "syntaxir.dev/sxir/internal/bench/parsers/chainlogical"
	gjsonevent "syntaxir.dev/sxir/internal/bench/parsers/jsonevent"
	gjsonlogical "syntaxir.dev/sxir/internal/bench/parsers/jsonlogical"
	gkeyedevent "syntaxir.dev/sxir/internal/bench/parsers/keyedevent"
	gkeyedlogical "syntaxir.dev/sxir/internal/bench/parsers/keyedlogical"
	gpolicyevent "syntaxir.dev/sxir/internal/bench/parsers/policyevent"
	gpolicylogical "syntaxir.dev/sxir/internal/bench/parsers/policylogical"
	grecoverevent "syntaxir.dev/sxir/internal/bench/parsers/recoverevent"
	grecoverlogical "syntaxir.dev/sxir/internal/bench/parsers/recoverlogical"
	gxmlevent "syntaxir.dev/sxir/internal/bench/parsers/xmlevent"
	gxmllogical "syntaxir.dev/sxir/internal/bench/parsers/xmllogical"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	rt "syntaxir.dev/sxir/pkg/runtime/libgo"
)

// The exactness of logical compute units (design record 076): the generated code, whose kept
// dispatch guards make the effects of the calls they skip, against the per-event reference, which
// evaluates every alternative whose skip would make a call, in the total and in the state of the
// memo; a sweep of compute limits; and the overshoot of a stopped parse against K + B.

type unitsParse func(string, ...sxirruntime.ParseOption) error

type exactCase struct {
	name           string
	inputs         []string
	logical, event [2]unitsParse // Parse, ParseValue
}

func exactCases() []exactCase {
	ps := func(n func(string, ...sxirruntime.ParseOption) error, v func(string, ...sxirruntime.ParseOption) error) [2]unitsParse {
		return [2]unitsParse{n, v}
	}
	return []exactCase{
		{"JSON", exactInputs(inputs.JSONInput(4<<10), []string{`1`, `true`, `null`, `"ab"`, `[1,2]`, `{"a":[1,{"b":null}]}`, `"é\n"`, `[1,]`, `{"a" 1}`, `tru`, ``, ` `, `[[[[`, `"\q"`, `-0.5e+3`, `01`}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gjsonlogical.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gjsonlogical.ParseValue(s, o...)
					return err
				}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gjsonevent.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gjsonevent.ParseValue(s, o...)
					return err
				})},
		{"XML", exactInputs(inputs.XMLInput(4<<10), []string{`<a/>`, `<a b="c">t</a>`, `<?xml version="1.0"?><a><!-- c --><b/></a>`, `<a><![CDATA[x]]></a>`, `<a>&amp;&#65;</a>`, `<a></b>`, `<a`, ``, `<a b=c/>`}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gxmllogical.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gxmllogical.ParseValue(s, o...)
					return err
				}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gxmlevent.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gxmlevent.ParseValue(s, o...)
					return err
				})},
		{"policy", policyInputs(),
			ps(func(s string, o ...sxirruntime.ParseOption) error {
				_, err := gpolicylogical.Parse(s, o...)
				return err
			},
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gpolicylogical.ParseValue(s, o...)
					return err
				}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gpolicyevent.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gpolicyevent.ParseValue(s, o...)
					return err
				})},
		{"chain", chainInputs(),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gchainlogical.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gchainlogical.ParseValue(s, o...)
					return err
				}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gchainevent.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gchainevent.ParseValue(s, o...)
					return err
				})},
		{"keyed", keyedInputs(),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gkeyedlogical.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gkeyedlogical.ParseValue(s, o...)
					return err
				}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := gkeyedevent.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := gkeyedevent.ParseValue(s, o...)
					return err
				})},
		{"recover", recoverInputs(1 << 10),
			ps(func(s string, o ...sxirruntime.ParseOption) error {
				_, err := grecoverlogical.Parse(s, o...)
				return err
			},
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := grecoverlogical.ParseValue(s, o...)
					return err
				}),
			ps(func(s string, o ...sxirruntime.ParseOption) error { _, err := grecoverevent.Parse(s, o...); return err },
				func(s string, o ...sxirruntime.ParseOption) error {
					_, err := grecoverevent.ParseValue(s, o...)
					return err
				})},
	}
}

// recoverInputs returns inputs of bench/grammars/budget_recover.sxir: lines that all recover, lines
// that recover after a long prefix, a mix, valid lines and damaged inputs, each line repeated in a
// ladder of sizes up to 64 KiB, of which those longer than max bytes are left out. Every line that does not match builds a syntax error, and the
// stores of the memo take the entries of recovered errors.
func recoverInputs(max int) []string {
	var in []string
	for _, line := range []string{"ab=cd;", "1;", "a=;", "1234567;", "a;1;b=c;;", ";", "abcdefghijklmnop=", "a=b;c=d"} {
		for _, n := range []int{1, 2, 16, 256, 4096, 65536 / len(line)} {
			if n*len(line) > max {
				continue
			}
			in = append(in, strings.Repeat(line, n))
		}
	}
	return append(in, "", ";;;", "é;", "a=é;\n;", strings.Repeat("é", 300)+";")
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

// chainInputs returns inputs of bench/grammars/logical_chain.sxir: runs of the characters where
// the guards skip calls of outer and inner, among words that the rules match, in a fixed random
// order, before and after the "|", with lookaheads after each "y"; and inputs that fail.
func chainInputs() []string {
	r := rand.New(rand.NewPCG(3, 4))
	tokens := []string{"c", "c", "c", "b", "e", "yc", "a.", "aq.", "a;", "a-!", "a-?", "aqq-?", "aqqq.", "ya-!"}
	part := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(tokens[r.IntN(len(tokens))])
		}
		return b.String()
	}
	var out []string
	for _, n := range []int{1, 10, 200, 3000} {
		out = append(out, part(n), part(n)+"|"+part(n), "|"+part(n))
	}
	return append(out, strings.Repeat("c", 3000)+"|"+strings.Repeat("cb", 1000)+strings.Repeat("a-!", 300),
		"|"+strings.Repeat("b", 3000), strings.Repeat("yc", 500), "", "a", "a-", "ccca-.", "|a-x")
}

// keyedInputs returns inputs of bench/grammars/logical_keyed.sxir: groups that bind k to names of
// different lengths, so that the calls of word, and the calls that guards skip, at the positions of
// a group have keys of different sizes, and the group "stop", where every word fails; and inputs that
// fail.
func keyedInputs() []string {
	r := rand.New(rand.NewPCG(5, 6))
	names := []string{"x", "yy", "zzzzzzzz", "stop", "x"}
	tokens := []string{"c", "c", "b", "a.", "aq.", "a!", "aqq!"}
	var out []string
	for _, n := range []int{1, 10, 400} {
		var b strings.Builder
		for range n {
			b.WriteString("=" + names[r.IntN(len(names))] + ":")
			for range r.IntN(12) {
				b.WriteString(tokens[r.IntN(len(tokens))])
			}
			b.WriteString(";")
		}
		out = append(out, b.String())
	}
	return append(out, "=stop:ccc;", "=stop:a.;", "=x:"+strings.Repeat("c", 2000)+strings.Repeat("a.", 200)+";", "=x:a.", "=:;", "")
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

func countUnits(f unitsParse, in string, unit sxirinput.PositionUnit, opts ...sxirruntime.ParseOption) (uint64, error) {
	var n uint64
	err := f(in, append([]sxirruntime.ParseOption{sxirruntime.WithUnit(unit), sxirruntime.WithComputeReport(&n)}, opts...)...)
	return n, err
}

// memoReport returns the state of the memo that the parse of in by f leaves (rt.SetMemoReport).
func memoReport(f unitsParse, in string, unit sxirinput.PositionUnit, opts ...sxirruntime.ParseOption) string {
	var m string
	rt.SetMemoReport(&m)
	defer rt.SetMemoReport(nil)
	_ = f(in, append([]sxirruntime.ParseOption{sxirruntime.WithUnit(unit)}, opts...)...)
	return m
}

// firstDiff returns the first line where the reports got and want differ.
func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(g), len(w)) {
		var a, b string
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			return fmt.Sprintf("line %d: got %q, reference %q", i+1, a, b)
		}
	}
	return "no difference"
}

var exactUnits = []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints}

// TestLogicalExact checks the totals of the generated code against the per-event reference on
// valid, truncated and damaged inputs, with no limit.
func TestLogicalExact(t *testing.T) {
	unlimited := sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited})
	for _, c := range exactCases() {
		failed, entries := 0, 0
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
					// The memo: the kept guards leave the policy counts, seen marks and entries that the
					// evaluation of the alternatives they skip leaves.
					gm, wm := memoReport(c.logical[api], in, unit, unlimited), memoReport(c.event[api], in, unit, unlimited)
					if gm != wm {
						t.Errorf("%s input %d (%v, api %d): the memo differs from the reference's:\n%s", c.name, i, unit, api, firstDiff(gm, wm))
					}
					entries += strings.Count(gm, "\nentry ")
				}
			}
		}
		t.Logf("%s: %d inputs, %d failing parses, %d memo entries", c.name, len(c.inputs), failed, entries)
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
					total, base := countUnits(c.logical[api], in, unit, sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
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
						used, err := countUnits(c.logical[api], in, unit, sxirruntime.WithLimits(sxirruntime.Limits{Compute: l}))
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
		{"logical", func(s string, o ...sxirruntime.ParseOption) error {
			_, err := gpolicylogical.Parse(s, o...)
			return err
		}},
		{"event", func(s string, o ...sxirruntime.ParseOption) error { _, err := gpolicyevent.Parse(s, o...); return err }},
	} {
		total := func(s string) uint64 {
			n, err := countUnits(p.parse, s, sxirinput.Bytes, sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
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
