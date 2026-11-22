//go:build !sxir_budget_controls_reference

package engine

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/document"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// Compute units are abstract units of the specification (spec/runtime/parsing.md#cost): for a
// grammar, a position unit and an input, every backend charges the same units in the same
// reservations. The tests of this file compare, per input, the units that a parse reserves in all
// (the smallest compute limit under which it does not fail on the limit) and what it reports
// (Used/Requested) at limits below that, across the backends of the engine: the closure backend,
// the recursive and iterative bytecode machines, and both machines with every rule that has a Flow
// body running it. The generated parsers join them in the long tier (charge_agreement_long_test.go).

// chargeRun parses an input under the compute limit and reports the outcome: "ok" for a parse that
// does not fail on the compute limit (a syntax error included), "used/requested" for one that does.
type chargeRun func(in string, limit uint64) string

// chargeBackend is a parser under comparison.
type chargeBackend struct {
	name string
	run  chargeRun
}

// chargeMaxLimit bounds the search for the units of a parse.
const chargeMaxLimit = 1 << 40

// chargeTotal returns the units that run reserves for in: the smallest limit under which it is ok,
// or 0 if no limit up to chargeMaxLimit is.
func chargeTotal(run chargeRun, in string) uint64 {
	hi := uint64(64)
	for run(in, hi) != "ok" {
		if hi >= chargeMaxLimit {
			return 0
		}
		hi *= 4
	}
	lo := uint64(0) // run fails under lo, or lo is 0
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if run(in, mid) == "ok" {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// chargeLimits returns the limits below total at which reports are compared: every limit for a
// small total, and otherwise a spread of them with the first and the last few.
func chargeLimits(total uint64) []uint64 {
	if total <= 1 {
		return nil
	}
	if total <= 400 {
		out := make([]uint64, 0, total-1)
		for l := uint64(1); l < total; l++ {
			out = append(out, l)
		}
		return out
	}
	set := map[uint64]bool{}
	for l := uint64(1); l <= 8; l++ {
		set[l] = true
		set[total-l] = true
	}
	for k := uint64(1); k < 32; k++ {
		set[total*k/32] = true
	}
	out := make([]uint64, 0, len(set))
	for l := range set {
		if l >= 1 && l < total {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

// limitReportOf reports err as chargeRun does.
func limitReportOf(err error) string {
	var le *sxirruntime.LimitError
	if errors.As(err, &le) && le.Kind == sxirruntime.LimitCompute {
		return fmt.Sprintf("%d/%d", le.Used, le.Requested)
	}
	return "ok"
}

// engineChargeBackends returns the backends of the engine for the grammar src: the closure
// backend and both bytecode machines.
func engineChargeBackends(t testing.TB, src string, unit input.PositionUnit) []chargeBackend {
	t.Helper()
	return engineChargeBackendsTraced(t, src, unit, false)
}

// engineChargeBackendsTraced is engineChargeBackends, with every parse traced if traced holds.
func engineChargeBackendsTraced(t testing.TB, src string, unit input.PositionUnit, traced bool) []chargeBackend {
	t.Helper()
	old := mustCompile(t, src)
	on := func(prog *Program, b Backend) chargeRun {
		return func(in string, limit uint64) string {
			o := ParseOptions{Backend: b, Unit: unit, Expectations: true, Limits: sxirruntime.Limits{Compute: limit}}
			if traced {
				o.Trace = func(TraceEvent) {}
			}
			_, err := prog.ParseWith("", in, o)
			return limitReportOf(err)
		}
	}
	return []chargeBackend{
		{"closure", on(old, Closure)},
		{"bytecode", on(old, Bytecode)},
		{"bytecode-iterative", on(old, BytecodeIterative)},
	}
}

// chargeDivergence is a backend that disagrees with the reference, and how.
type chargeDivergence struct{ backend, msg string }

// chargeDivergences compares the backends on the inputs: the first backend is the reference. It
// returns one divergence for each backend and input whose units differ, or whose report differs at
// a limit below the reference's units (the first such limit).
func chargeDivergences(backends []chargeBackend, inputs []string) []chargeDivergence {
	var out []chargeDivergence
	for i, in := range inputs {
		ref := backends[0]
		want := chargeTotal(ref.run, in)
		limits := chargeLimits(want)
		wantAt := make([]string, len(limits))
		for k, l := range limits {
			wantAt[k] = ref.run(in, l)
		}
		for _, b := range backends[1:] {
			if got := chargeTotal(b.run, in); got != want {
				out = append(out, chargeDivergence{b.name, fmt.Sprintf("input %d (%d bytes): %s %d, %s %d", i, len(in), b.name, got, ref.name, want)})
				continue
			}
			for k, l := range limits {
				if got := b.run(in, l); got != wantAt[k] {
					out = append(out, chargeDivergence{b.name, fmt.Sprintf("input %d (%d bytes): limit %d: %s %s, %s %s", i, len(in), l, b.name, got, ref.name, wantAt[k])})
					break
				}
			}
		}
	}
	return out
}

// chargeCase is a grammar with its inputs.
type chargeCase struct {
	name, grammar string
	inputs        []string
}

// chargeCases are small grammars that each exercise a kind of charge: matching, repetition,
// choice, guards that skip alternatives, cut, gathered values, actions, Operators Rules, left recursion,
// recovery and lookahead.
var chargeCases = []chargeCase{
	{"literal", "rule main = \"abcd\"+", []string{"abcdabcd", "abcdab", "x", ""}},
	{"class repetition", "rule main = [a-z]*", []string{"abc", "", "abc1"}},
	{"class sequence", "rule main = [a-z]#repeat(5,) / [a-z] [a-z] [a-z]", []string{"abc", "abcdefg", "ab"}},
	{"bounded scans", "rule main = w \"-\" w $$\nrule w = [a-z]#repeat(2,4) .#repeat(0,2) #atomic", []string{"abc-de", "abcdefg-ab", "a-b", "ab-"}},
	{"guard skips", "rule main = (\"p\" [0-9] / \"q\" / \"r\" \"!\" / [s-u]+ / [0-9])* $$", []string{"p1qr!st9", "qqx", "r", ""}},
	{"guard skips through calls", "rule main = (kw / a / b / c / d)* $$\nrule kw = \"if\" / \"do\"\nrule a = \"x\" [0-9]\nrule b = \"y\"\nrule c = \"z\" \"!\"\nrule d = [0-9]+", []string{"dox1yz!12if", "yyw", "z", ""}},
	{"choice of sequences", "rule main = \"a\" \"b\" $$ / \"a\" \"c\" $$", []string{"ab", "ac", "ad"}},
	{"cut in repetition", "rule main = ([a-z] #cut [,])+ / [a-z,]*", []string{"a,b,c", "a,b,c,", "a,bc"}},
	{"repeat or class", "rule main = [a-z]#repeat(5,) / [a-z]*", []string{"abc", "abcdefgh", "abc1"}},
	{"rules", "rule main = item (\",\" item)* $$\nrule item = word / num\nrule word = [a-z]+\nrule num = [0-9]+", []string{"ab,12,c", "ab,,", "1"}},
	{"captures", "rule main = k:key \"=\" v:val $$\nrule key = [a-z]+\nrule val = [0-9]+ / key", []string{"a=1", "abc=def", "a="}},
	{"lookahead", "rule main = (!\"x\" .)* &\"x\" \"x\" $$", []string{"abcx", "abc"}},
	{"list action", "@start\nrule main int = \"x\" => len(filter(list(1, 2, 3, 4, 5, 6, 7), (e) => e > 2))", []string{"x", "y"}},
	{"text action", "rule main string = w:[a-z]+ => text($w) + text($w)", []string{"abc", "1"}},
	{"operators", `rule main = #operators {
    operand [0-9]
    level {
        infix left "+"
        infix left "*"
    }
    level {
        prefix "-"
    }
}`, []string{"1+2*3", "-1+-2", "1+", ""}},
	{"left recursion", "rule main = expr $$\nrule expr = expr \"+\" term / term\nrule term = [0-9]+", []string{"1+2+3", "1+", "12"}},
	{"recovery", `rule main = ("a" @error("need a") #recover(skip=.))* $$`, []string{"aab", "bbb", "a"}},
	{"recover skip", "rule main = s $$\nrule s = item s \"!\" / item s / item\nrule item = (\"a\" \";\") #recover(skip=[^;]* \";\")", []string{"a;a;", "a;x;a;!", "x"}},
}

// TestChargeAgreementEngine compares the units of the engine's backends on the charge cases, in both
// position units.
func TestChargeAgreementEngine(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range chargeCases {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				for _, d := range chargeDivergences(engineChargeBackends(t, c.grammar, unit), c.inputs) {
					reportChargeDivergence(t, c.name, d.backend, d.msg)
				}
			})
		}
	}
}

// reportChargeDivergence reports that backend disagrees with the reference on the case name, as
// msg says. The backends do not yet charge the same units: a divergence of a pair of case and backend
// in knownChargeDivergences is only logged, and any other fails the test. With SXIR_CHARGE_INVENTORY set
// every divergence is only logged, so that a run lists them all.
func reportChargeDivergence(t *testing.T, name, backend, msg string) {
	t.Helper()
	if os.Getenv("SXIR_CHARGE_INVENTORY") != "" || knownChargeDivergences[name+"|"+backend] {
		t.Logf("DIVERGENCE %s: %s: %s", name, backend, msg)
		return
	}
	t.Errorf("%s: %s: %s", name, backend, msg)
}

// documentCharges returns, for the backend b, the units that the first parse of a Document of in
// counts under explicit limits.
func documentCharges(t *testing.T, prog *Program, b Backend, unit input.PositionUnit, in string) uint64 {
	t.Helper()
	run := func(in string, limit uint64) string {
		// The limit applies to the parse, not to the construction of the Document, whose preparation of
		// the text is charged apart and may cost more than the parse counts now that the work of the
		// Document counts under no limit.
		d, err := prog.NewDocumentWith("", in, ParseOptions{Backend: b, Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		o := d.ParseDefaults()
		o.Limits = sxirruntime.Limits{Compute: limit}
		_, err = d.ParseWith(o)
		return limitReportOf(err)
	}
	return chargeTotal(run, in)
}

// TestChargeAgreementDocument compares, on the charge cases, the units of the first parse of a
// Document and of a parse after an edit across the backends. The count of a Document parse against
// the default limit is compared with a parse from scratch in the document package
// (TestChargeDocumentDefaultCount), which reads it at the end of the parse.
func TestChargeAgreementDocument(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range chargeCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			prog := compileBare(t, c.grammar)
			unit := input.CodePoints
			for i, in := range c.inputs {
				var explicit []uint64
				for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
					explicit = append(explicit, documentCharges(t, prog, b, unit, in))
				}
				if explicit[1] != explicit[0] || explicit[2] != explicit[0] {
					reportChargeDivergence(t, c.name, "document", fmt.Sprintf("input %d: Document first parse under explicit limits: closure %d, bytecode %d, iterative %d", i, explicit[0], explicit[1], explicit[2]))
				}
				var edited []uint64
				for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
					edited = append(edited, chargeTotal(func(in string, limit uint64) string {
						d, err := prog.NewDocumentWith("", in, ParseOptions{Backend: b, Unit: unit})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := d.Parse(); err != nil && limitReportOf(err) != "ok" {
							t.Fatal(err)
						}
						editMiddle(t, d)
						o := d.ParseDefaults()
						o.Limits = sxirruntime.Limits{Compute: limit}
						_, err = d.ParseWith(o)
						return limitReportOf(err)
					}, in))
				}
				if edited[1] != edited[0] || edited[2] != edited[0] {
					reportChargeDivergence(t, c.name, "document edit", fmt.Sprintf("input %d: Document parse after an edit: closure %d, bytecode %d, iterative %d", i, edited[0], edited[1], edited[2]))
				}
			}
		})
	}
}

// editMiddle replaces the code point in the middle of the text of d by itself, or inserts "a" into an
// empty text.
func editMiddle(t *testing.T, d *document.Document) {
	t.Helper()
	text := []rune(d.Text())
	at := len(text) / 2
	repl := "a"
	if at < len(text) {
		repl = string(text[at])
		if err := d.Edit(at, at+1, repl); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := d.Edit(at, at, repl); err != nil {
		t.Fatal(err)
	}
}
