package examinedref_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// perturbUnitsOf splits s into units of a position unit. It is written apart from examinedref.Units so that
// validPerturbation does not share the code it checks.
func perturbUnitsOf(s string, bytes bool) []string {
	var out []string
	for len(s) > 0 {
		n := 1
		if !bytes {
			_, n = utf8.DecodeRuneInString(s)
		}
		out, s = append(out, s[:n]), s[n:]
	}
	return out
}

// validPerturbation checks that p is what its variant promises: the units of in before the change point are
// kept, and from the change point on every unit differs (substitute), none is left (truncate), or units
// follow the whole input (extend).
func validPerturbation(in string, bytes bool, p examinedref.Perturbation) error {
	u, v := perturbUnitsOf(in, bytes), perturbUnitsOf(p.Input, bytes)
	c := p.Change
	if c < 0 || c > len(u) || len(v) < c {
		return fmt.Errorf("%s at %d of %q: %q has no prefix of %d units", p.Variant, c, in, p.Input, c)
	}
	for i := 0; i < c; i++ {
		if u[i] != v[i] {
			return fmt.Errorf("%s at %d of %q: %q changes unit %d", p.Variant, c, in, p.Input, i)
		}
	}
	switch p.Variant {
	case examinedref.Substitute:
		if c == len(u) || len(v) != len(u) {
			return fmt.Errorf("substitute at %d of %q: %q does not replace the %d units from the change point", c, in, p.Input, len(u)-c)
		}
		for i := c; i < len(u); i++ {
			if u[i] == v[i] {
				return fmt.Errorf("substitute at %d of %q: %q keeps unit %d", c, in, p.Input, i)
			}
		}
	case examinedref.Truncate:
		if c == len(u) || len(v) != c {
			return fmt.Errorf("truncate at %d of %q: %q does not end at the change point", c, in, p.Input)
		}
	case examinedref.Extend:
		if c != len(u) || len(v) == c {
			return fmt.Errorf("extend at %d of %q: %q does not extend the input", c, in, p.Input)
		}
	default:
		return fmt.Errorf("perturbation %v of %q: unknown variant", p, in)
	}
	return nil
}

func lockEnter(rule string, pos, depth int) examinedref.Event {
	return examinedref.Event{Kind: examinedref.EventEnter, Rule: rule, Pos: pos, Depth: depth}
}

func lockExit(rule string, pos, depth int, matched bool, end, examined int) examinedref.Event {
	return examinedref.Event{Kind: examinedref.EventExit, Rule: rule, Pos: pos, Depth: depth, Matched: matched, End: end, Examined: examined}
}

// TestCheckLockstep checks the comparison on hand-made traces: who is running at the first difference, which
// examined ends must exceed the change point, the exemption of grammars that read variables and that of
// the calls of a cycle of left calls inside a growth of the cycle.
func TestCheckLockstep(t *testing.T) {
	t.Parallel()
	// main@0 calls a@0, which matches [0, 2) having examined slot 2, then b@2.
	orig := []examinedref.Event{
		lockEnter("main", 0, 1), lockEnter("a", 0, 2), lockExit("a", 0, 2, true, 2, 3),
		lockEnter("b", 2, 2), lockExit("b", 2, 2, true, 3, 4), lockExit("main", 0, 1, true, 3, 4),
	}
	// b sees another unit at 2 and fails.
	pert := []examinedref.Event{
		lockEnter("main", 0, 1), lockEnter("a", 0, 2), lockExit("a", 0, 2, true, 2, 3),
		lockEnter("b", 2, 2), lockExit("b", 2, 2, false, 2, 3), lockExit("main", 0, 1, false, 0, 3),
	}
	withExamined := func(tr []examinedref.Event, i, examined int) []examinedref.Event {
		out := append([]examinedref.Event(nil), tr...)
		out[i].Examined = examined
		return out
	}
	// a and b are a cycle of left calls, and c is on none. main@0 calls a@0, which grows by b@0 (a call of
	// the cycle at the growth's position), which calls c@0; c sees another unit at 2 and fails.
	cycles := map[string]int{"a": 0, "b": 0}
	grow := []examinedref.Event{
		lockEnter("main", 0, 1), lockEnter("a", 0, 2), lockEnter("b", 0, 3), lockEnter("c", 0, 4),
		lockExit("c", 0, 4, true, 1, 3), lockExit("b", 0, 3, true, 1, 3), lockExit("a", 0, 2, true, 2, 3),
		lockExit("main", 0, 1, true, 2, 3),
	}
	growPert := []examinedref.Event{
		lockEnter("main", 0, 1), lockEnter("a", 0, 2), lockEnter("b", 0, 3), lockEnter("c", 0, 4),
		lockExit("c", 0, 4, false, 0, 3), lockExit("b", 0, 3, false, 0, 3), lockExit("a", 0, 2, false, 0, 3),
		lockExit("main", 0, 1, false, 0, 3),
	}
	tests := []struct {
		name       string
		orig, pert []examinedref.Event
		change     int
		vars       bool
		cycles     map[string]int
		diverged   bool
		running    int
		checked    int
		exempt     int
		growing    int
		violations []string // substrings, one per violation
	}{
		{name: "identical traces", orig: orig, pert: orig, change: 2},
		{name: "every running call examined beyond the change point", orig: orig, pert: pert, change: 2,
			diverged: true, running: 2, checked: 4},
		{name: "a running call that does not examine the change point", orig: withExamined(orig, 4, 2), pert: pert, change: 2,
			diverged: true, running: 2, checked: 4, violations: []string{"b@2 (level 0, depth 2) is running, but its original run reports examined end 2"}},
		{name: "a caller whose examined end is too small in the perturbed run", orig: orig, pert: withExamined(pert, 5, 2), change: 2,
			diverged: true, running: 2, checked: 4, violations: []string{"main@0 (level 0, depth 1) is running, but its perturbed run reports examined end 2"}},
		{name: "a completed call is not blamed", orig: withExamined(orig, 2, 1), pert: withExamined(pert, 2, 1), change: 2,
			diverged: true, running: 2, checked: 4},
		{name: "a match before the enter ends beyond the change point in a grammar with variables",
			orig: withExamined(orig, 4, 1), pert: pert, change: 1, vars: true,
			diverged: true, running: 2, checked: 2, exempt: 1},
		{name: "the same without variables", orig: withExamined(orig, 4, 1), pert: pert, change: 1,
			diverged: true, running: 2, checked: 4, violations: []string{"b@2 (level 0, depth 2) is running, but its original run reports examined end 1"}},
		// A growth step that one parse completes and the other does not ends without an event, so b can be
		// entered in different steps of the two parses and examine less than the change point in one.
		{name: "a call of the cycle inside a growth of the cycle", orig: withExamined(grow, 5, 2), pert: growPert, change: 2,
			cycles: cycles, diverged: true, running: 4, checked: 6, growing: 1},
		{name: "the same without the cycle", orig: withExamined(grow, 5, 2), pert: growPert, change: 2,
			diverged: true, running: 4, checked: 8, violations: []string{"b@0 (level 0, depth 3) is running, but its original run reports examined end 2"}},
		{name: "a leader of the growth whose examined end is too small", orig: withExamined(grow, 6, 2), pert: growPert, change: 2,
			cycles: cycles, diverged: true, running: 4, checked: 6, growing: 1,
			violations: []string{"a@0 (level 0, depth 2) is running, but its original run reports examined end 2"}},
		{name: "a call on no cycle inside a growth", orig: withExamined(grow, 4, 2), pert: growPert, change: 2,
			cycles: cycles, diverged: true, running: 4, checked: 6, growing: 1,
			violations: []string{"c@0 (level 0, depth 4) is running, but its original run reports examined end 2"}},
		{name: "a call of the cycle at another position", change: 2, cycles: cycles, diverged: true, running: 3, checked: 6,
			orig: []examinedref.Event{lockEnter("main", 0, 1), lockEnter("a", 0, 2), lockEnter("b", 1, 3),
				lockExit("b", 1, 3, true, 3, 2), lockExit("a", 0, 2, true, 3, 4), lockExit("main", 0, 1, true, 3, 4)},
			pert: []examinedref.Event{lockEnter("main", 0, 1), lockEnter("a", 0, 2), lockEnter("b", 1, 3),
				lockExit("b", 1, 3, false, 1, 3), lockExit("a", 0, 2, false, 0, 3), lockExit("main", 0, 1, false, 0, 3)},
			violations: []string{"b@1 (level 0, depth 3) is running, but its original run reports examined end 2"}},
		{name: "a difference after the start rule returned", change: 1,
			orig: append(append([]examinedref.Event(nil), orig...), lockEnter("x", 3, 1), lockExit("x", 3, 1, false, 3, 4)), pert: orig,
			diverged: true},
		{name: "a perturbed run that ends inside the calls", orig: orig, pert: pert[:4], change: 2,
			diverged: true, running: 2, checked: 2},
		{name: "an exit that closes another call", orig: orig, pert: []examinedref.Event{lockEnter("main", 0, 1), lockExit("a", 0, 2, true, 1, 1)}, change: 0,
			violations: []string{"does not close the running"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := examinedref.CheckLockstep(tc.orig, tc.pert, tc.change, examinedref.Dependence{Variables: tc.vars, Cycles: tc.cycles})
			if l.Diverged != tc.diverged || l.Running != tc.running || l.Checked != tc.checked || l.Exempt != tc.exempt || l.Growing != tc.growing {
				t.Errorf("diverged %v, running %d, checked %d, exempt %d, growing %d; want %v, %d, %d, %d, %d",
					l.Diverged, l.Running, l.Checked, l.Exempt, l.Growing, tc.diverged, tc.running, tc.checked, tc.exempt, tc.growing)
			}
			if len(l.Violations) != len(tc.violations) {
				t.Fatalf("violations %q, want %d", l.Violations, len(tc.violations))
			}
			for i, v := range tc.violations {
				if !strings.Contains(l.Violations[i], v) {
					t.Errorf("violation %q does not contain %q", l.Violations[i], v)
				}
			}
		})
	}
}

// TestLockstepIndirectGrowth checks the exemption of growths on the engine's parse of a left recursion
// through two rules, which a growth step that only one of two parses completes makes the traces agree
// on in different steps: a and b are numbered as one cycle, and the perturbations of an input that grows
// both rules report no violation with the cycles and some without them.
func TestLockstepIndirectGrowth(t *testing.T) {
	t.Parallel()
	const src = "rule main = a $$\nrule a = b \"1\" / b \"2\" / \"a\"\nrule b = a \"x\" / b \"y\" / \"b\""
	g, err := enginetest.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	cycles, err := examinedref.LeftCycles(g, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cycles["main"]; ok || len(cycles) != 2 || cycles["a"] != cycles["b"] {
		t.Fatalf("cycles %v, want a and b on one cycle", cycles)
	}
	started, err := enginetest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	p, err := enginetest.Compile(started, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range perturbBackends {
		run := func(s string) []examinedref.Event {
			var tr []examinedref.Event
			_, _ = p.OnBackend(b).Parse(s, tracing(&tr, input.Bytes)...)
			return tr
		}
		r := examinedref.Perturb(run, "b1x1y2", true, examinedref.Dependence{Cycles: cycles}, "12abxy", nil)
		if len(r.Violations) > 0 || r.Growing == 0 {
			t.Errorf("%s: growing %d, violations %q", b, r.Growing, r.Violations)
		}
		if r := examinedref.Perturb(run, "b1x1y2", true, examinedref.Dependence{}, "12abxy", nil); len(r.Violations) == 0 {
			t.Errorf("%s: no violation without the cycles: the case does not exercise the exemption", b)
		}
	}
}

// TestPerturbations checks the perturbations of inputs in both units: each keeps the units before its change
// point and changes every unit from it on, the change points are the examined ends below the length, and in
// code points a substitution that would complete a UTF-8 prefix before the change point is dropped.
func TestPerturbations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		bytes    bool
		alpha    string
		ends     []int
		count    map[examinedref.Variant]int
		dropped  int
		changeAt []int // the change points of the substitutions and truncations
	}{
		{in: "abc", bytes: true, alpha: "xy", ends: []int{0, 1, 1, 3, 4},
			count: map[examinedref.Variant]int{examinedref.Substitute: 4, examinedref.Truncate: 2, examinedref.Extend: 4}, changeAt: []int{0, 1}},
		{in: "é😀", alpha: "a", ends: []int{1, 2},
			count: map[examinedref.Variant]int{examinedref.Substitute: 2, examinedref.Truncate: 1, examinedref.Extend: 4}, changeAt: []int{1}},
		{in: "é😀", bytes: true, alpha: "a", ends: []int{1, 2},
			count: map[examinedref.Variant]int{examinedref.Substitute: 4, examinedref.Truncate: 2, examinedref.Extend: 4}, changeAt: []int{1, 2}},
		// The units are \xe2, \x82 and a; the alphabet sorts as \x00, a, \x82, \xac, \xe2, and both
		// substitutions of a (by \x82 and \xac) complete the prefix \xe2\x82 to one character.
		{in: "\xe2\x82a", alpha: "\xac", ends: []int{2},
			count: map[examinedref.Variant]int{examinedref.Truncate: 1, examinedref.Extend: 4}, dropped: 2, changeAt: []int{2}},
		{in: "", alpha: "a", ends: []int{0, 1},
			count: map[examinedref.Variant]int{examinedref.Extend: 2}},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q bytes=%v", tc.in, tc.bytes), func(t *testing.T) {
			ps, dropped := examinedref.Perturbations(tc.in, tc.bytes, tc.alpha, tc.ends)
			count := map[examinedref.Variant]int{}
			changes := map[int]bool{}
			for _, p := range ps {
				count[p.Variant]++
				if p.Variant != examinedref.Extend {
					changes[p.Change] = true
				}
				if err := validPerturbation(tc.in, tc.bytes, p); err != nil {
					t.Error(err)
				}
			}
			if fmt.Sprint(count) != fmt.Sprint(tc.count) || dropped != tc.dropped {
				t.Errorf("perturbations %v, dropped %d; want %v, %d", count, dropped, tc.count, tc.dropped)
			}
			if len(changes) != len(tc.changeAt) {
				t.Errorf("change points %v, want %v", changes, tc.changeAt)
			}
			for _, c := range tc.changeAt {
				if !changes[c] {
					t.Errorf("change points %v, want %v", changes, tc.changeAt)
				}
			}
		})
	}
}

// TestPerturbAt checks the perturbation of parses that depend on the change point, on a parse of a* that
// marks its call with the change point: the original is parsed for each change point, a route that
// cannot parse at a change point is counted, too small examined ends and an original that is not a
// function of its input are reported.
func TestPerturbAt(t *testing.T) {
	t.Parallel()
	// as parses a*: main@0 reads up to the first unit that is not a, or the end of the input.
	as := func(c int, s string, examined func(int) int) []examinedref.Event {
		n := len(s) - len(strings.TrimLeft(s, "a"))
		enter, exit := lockEnter("main", 0, 1), lockExit("main", 0, 1, true, n, examined(n+1))
		enter.Level, exit.Level = c, c
		return []examinedref.Event{enter, exit}
	}
	exact := func(e int) int { return e }
	calls := 0
	tests := []struct {
		name                        string
		run                         func(int, string) ([]examinedref.Event, bool)
		violations                  bool
		refused, identical, outside bool
	}{
		{name: "exact", run: func(c int, s string) ([]examinedref.Event, bool) { return as(c, s, exact), true }, identical: true},
		{name: "refused below the end", run: func(c int, s string) ([]examinedref.Event, bool) { return as(c, s, exact), c == len("abb") }, refused: true, identical: true},
		{name: "examined end too small", run: func(c int, s string) ([]examinedref.Event, bool) {
			return as(c, s, func(int) int { return 1 }), true
		}, violations: true, identical: true},
		{name: "original not a function of its input", run: func(c int, s string) ([]examinedref.Event, bool) {
			calls++
			return as(c+calls, s, exact), true
		}, violations: true},
	}
	for _, tc := range tests {
		r := examinedref.PerturbAt(tc.run, "abb", true, examinedref.Dependence{}, "a", nil)
		if (len(r.Violations) > 0) != tc.violations {
			t.Errorf("%s: violations %q", tc.name, r.Violations)
		}
		if (r.Refused > 0) != tc.refused || (r.Identical > 0) != tc.identical || (r.Outside > 0) != tc.outside {
			t.Errorf("%s: %+v", tc.name, r)
		}
		if !tc.violations && r.Refused+r.Identical+r.Diverged+r.Outside != r.Perturbed {
			t.Errorf("%s: %+v does not account for every perturbation", tc.name, r)
		}
	}
	// Perturb compares every perturbation with the original parsed for the length, whose mark differs.
	r := examinedref.Perturb(func(s string) []examinedref.Event { return as(len(s), s, exact) }, "abb", true, examinedref.Dependence{}, "a", nil)
	if r.Outside == 0 || len(r.Violations) > 0 {
		t.Errorf("Perturb: %+v", r)
	}
}

// TestGrammarFacts checks the alphabet and the variable reads that the perturbations take from a grammar.
func TestGrammarFacts(t *testing.T) {
	t.Parallel()
	g, err := enginetest.ParseGrammar("var n int\nrule main = \"ab\" [x-z] item* $$\nrule item = \"c\" {n > 0}")
	if err != nil {
		t.Fatal(err)
	}
	if a := examinedref.GrammarAlphabet(g); a != "abxzc" {
		t.Errorf("alphabet %q, want %q", a, "abxzc")
	}
	if !examinedref.ReadsVariables(g) {
		t.Error("the grammar reads n")
	}
	g, err = enginetest.ParseGrammar("var n int\nrule main = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	if examinedref.ReadsVariables(g) {
		t.Error("the grammar reads no variable")
	}
}
