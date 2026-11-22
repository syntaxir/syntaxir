package examinedref_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
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
// examined ends must exceed the change point, and the exemption of grammars that read variables.
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
	tests := []struct {
		name       string
		orig, pert []examinedref.Event
		change     int
		vars       bool
		diverged   bool
		running    int
		checked    int
		exempt     int
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
			l := examinedref.CheckLockstep(tc.orig, tc.pert, tc.change, tc.vars)
			if l.Diverged != tc.diverged || l.Running != tc.running || l.Checked != tc.checked || l.Exempt != tc.exempt {
				t.Errorf("diverged %v, running %d, checked %d, exempt %d; want %v, %d, %d, %d",
					l.Diverged, l.Running, l.Checked, l.Exempt, tc.diverged, tc.running, tc.checked, tc.exempt)
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
