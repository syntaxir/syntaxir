package examinedref

// This file is the perturbation check of the examined end (record 077, Examined Oracle): a parse is
// repeated on inputs that differ from the original only at positions at or after a change point, and
// the traces of the two runs are compared step by step. Until the first step where they differ, both
// runs made the same calls and read the same input; the first difference shows that a call still
// running read a slot at or after the change point, so each such call must report an examined end
// beyond the change point.
//
// The file imports the standard library only: the tests of generated parsers copy it, with
// highwater.go, into the module of their consumer, so that the generated parsers are checked by this
// same code.

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Event kinds, as instrument.TraceKind names them.
const (
	EventEnter = iota
	EventExit
	EventSkipped
)

// Event is one trace event of an observed parse, with every field that a trace event carries apart
// from the parser state behind it.
type Event struct {
	Kind              int
	Rule              string
	Level, Depth, Pos int
	Lookahead         bool
	Matched           bool
	End               int
	Memo              bool
	Evals             int
	ReusedElements    int
	Examined          int
	SkipReason        string
}

func (e Event) String() string {
	switch e.Kind {
	case EventEnter:
		return fmt.Sprintf("enter %s@%d (level %d, depth %d)", e.Rule, e.Pos, e.Level, e.Depth)
	case EventExit:
		return fmt.Sprintf("exit %s@%d (level %d, depth %d, matched %v, end %d, examined %d, memo %v, evals %d)",
			e.Rule, e.Pos, e.Level, e.Depth, e.Matched, e.End, e.Examined, e.Memo, e.Evals)
	default:
		return fmt.Sprintf("skipped %s@%d (%s)", e.Rule, e.Pos, e.SkipReason)
	}
}

// Variant is a way of changing the input from a change point on.
type Variant int

const (
	// Substitute replaces every unit at or after the change point by another unit.
	Substitute Variant = iota
	// Truncate ends the input at the change point.
	Truncate
	// Extend appends units to the input; its change point is the length of the input, whose end slot
	// becomes a character.
	Extend
)

func (v Variant) String() string {
	switch v {
	case Substitute:
		return "substitute"
	case Truncate:
		return "truncate"
	default:
		return "extend"
	}
}

// Perturbation is an input that agrees with the original on the slots before Change and differs from it
// in every slot from Change on that either input has.
type Perturbation struct {
	Variant Variant
	Change  int
	Input   string
}

// Rotations is the number of substitutions per change point: substitution k replaces each unit by the
// unit k places after it in the alphabet of the perturbation.
const Rotations = 2

// Units splits s into the units of a position unit: bytes, or code points as the parsers decode them
// (an invalid byte is a unit of its own). Each unit keeps its bytes, so that a prefix keeps invalid UTF-8.
func Units(s string, bytes bool) []string {
	var out []string
	for i := 0; i < len(s); {
		size := 1
		if !bytes {
			_, size = utf8.DecodeRuneInString(s[i:])
		}
		out = append(out, s[i:i+size])
		i += size
	}
	return out
}

// alphabet returns the sorted distinct units of the grammar's characters alpha, of the input and NUL,
// which no grammar of the tests names: a unit outside every class.
func alphabet(alpha string, in []string, bytes bool) []string {
	set := map[string]bool{"\x00": true}
	for _, u := range Units(alpha, bytes) {
		set[u] = true
	}
	for _, u := range in {
		set[u] = true
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// Perturbations returns the perturbations of in for the change points ends (the examined ends of the
// exits of a parse of in): for each change point below the length, Rotations substitutions and a
// truncation, and the extensions of the input by single units of the alphabet and by the input itself.
// alpha holds the characters of the grammar (GrammarAlphabet), from which substitutions and extensions
// draw their units. In code points, a perturbation whose new units would join a unit before the change
// point (an incomplete UTF-8 prefix completed by the bytes after it) changes a slot before the change
// point; it is left out and counted in dropped.
func Perturbations(in string, bytes bool, alpha string, ends []int) (out []Perturbation, dropped int) {
	u := Units(in, bytes)
	n := len(u)
	a := alphabet(alpha, u, bytes)
	index := map[string]int{}
	for i, x := range a {
		index[x] = i
	}
	add := func(p Perturbation) {
		if !bytes && !samePrefix(Units(p.Input, false), u, p.Change) {
			dropped++
			return
		}
		out = append(out, p)
	}
	seen := map[int]bool{}
	for _, c := range ends {
		if c < 0 || c >= n || seen[c] {
			continue
		}
		seen[c] = true
		prefix := strings.Join(u[:c], "")
		for k := 1; k <= Rotations && k < len(a); k++ {
			var b strings.Builder
			b.WriteString(prefix)
			for _, x := range u[c:] {
				b.WriteString(a[(index[x]+k)%len(a)])
			}
			add(Perturbation{Substitute, c, b.String()})
		}
		add(Perturbation{Truncate, c, prefix})
	}
	exts := map[string]bool{}
	for _, x := range []string{a[0], a[len(a)/2], a[len(a)-1]} {
		if !exts[x] {
			exts[x] = true
			add(Perturbation{Extend, n, in + x})
		}
	}
	if n > 0 {
		add(Perturbation{Extend, n, in + in})
	}
	return out, dropped
}

// samePrefix reports whether the first c units of got are those of want.
func samePrefix(got, want []string, c int) bool {
	if len(got) < c {
		return false
	}
	for i := 0; i < c; i++ {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Lockstep is the comparison of the trace of a parse with the trace of the parse of a perturbation.
type Lockstep struct {
	// Diverged reports that the traces differ; At is the index of the first event that differs (the
	// length of the shorter trace when one is a prefix of the other).
	Diverged bool
	At       int
	// Running is the number of calls running at the first difference; Checked the number of their
	// exits (in either run) whose examined end was compared with the change point, Exempt the number
	// of running calls left out because a match completed before the call's enter ends beyond the change
	// point in a grammar that reads variables, and Growing the number left out because they are calls of
	// a cycle of left calls inside a growth of that cycle at their position.
	Running, Checked, Exempt, Growing int
	Violations                        []string
}

// pairs returns, for each enter event of tr, the index of its exit, or -1 if the trace ends first (an
// action aborted the parse), and -1 for the other events. It reports an exit that does not close the
// innermost running call.
func pairs(tr []Event) ([]int, error) {
	exit := make([]int, len(tr))
	var stack []int
	for i, e := range tr {
		exit[i] = -1
		switch e.Kind {
		case EventEnter:
			stack = append(stack, i)
		case EventExit:
			if len(stack) == 0 {
				return nil, fmt.Errorf("event %d, %v, closes no call", i, e)
			}
			top := tr[stack[len(stack)-1]]
			if top.Rule != e.Rule || top.Pos != e.Pos || top.Level != e.Level || top.Depth != e.Depth {
				return nil, fmt.Errorf("event %d, %v, does not close the running %v", i, e, top)
			}
			exit[stack[len(stack)-1]] = i
			stack = stack[:len(stack)-1]
		}
	}
	return exit, nil
}

// Dependence holds the facts of a grammar through which a call's result can depend on more than the
// input from its position on, which CheckLockstep must allow for.
type Dependence struct {
	// Variables reports that the grammar reads variables (ReadsVariables).
	Variables bool
	// Cycles numbers the cycles of left calls (LeftCycles): rules with the same number lie on one cycle,
	// and a rule on no cycle has no number.
	Cycles map[string]int
}

// CheckLockstep compares the trace orig of a parse with the trace pert of the parse of an input that
// agrees with the original on the slots before change and differs from it from change on. Up to the
// first event where the traces differ, the two parses made the same calls with the same results, and a
// call whose result is a function of the input from its position on was the same computation in both:
// every such call that is still running at that event (entered and not exited before it, the call whose
// exit differs included) read a slot at or after change in both runs, so its examined end in each run,
// which the run reports at the call's exit, must exceed change.
//
// Two facts of the grammar (dep) make a call's result depend on more than that input. In a grammar that
// reads variables, a call can depend on the value of a variable bound by a match that completed before
// the call began, whose input the call's examined range does not include; a call is then checked only
// when change is at least the largest end of a match completed before its enter, so that no such match
// spans a changed slot. Inside a growth of left recursion, a call of a rule of the growing cycle at the
// growth's position depends on the seed and the step of the growth, and a growth step ends without an
// event: a step that one parse completes and the other does not can be followed by the same events, so
// such a call can be entered in different steps of the two parses. A call is therefore not checked when
// an enclosing running call at its position belongs to the same cycle of left calls (dep.Cycles); the
// enclosing calls, which decided the steps, are checked.
func CheckLockstep(orig, pert []Event, change int, dep Dependence) Lockstep {
	var l Lockstep
	exitO, err := pairs(orig)
	if err != nil {
		l.Violations = append(l.Violations, "original trace: "+err.Error())
		return l
	}
	exitP, err := pairs(pert)
	if err != nil {
		l.Violations = append(l.Violations, "perturbed trace: "+err.Error())
		return l
	}
	k := 0
	for k < len(orig) && k < len(pert) && orig[k] == pert[k] {
		k++
	}
	if k == len(orig) && k == len(pert) {
		return l
	}
	l.Diverged, l.At = true, k
	differs := func(tr []Event) string {
		if k < len(tr) {
			return tr[k].String()
		}
		return "the end of the trace"
	}
	// The running calls and the largest end of a match completed before each enter, from the common
	// prefix.
	var stack []int
	matchedEnd := make([]int, k)
	largest := -1
	for i := 0; i < k; i++ {
		matchedEnd[i] = largest
		switch orig[i].Kind {
		case EventEnter:
			stack = append(stack, i)
		case EventExit:
			stack = stack[:len(stack)-1]
			if orig[i].Matched && orig[i].End > largest {
				largest = orig[i].End
			}
		}
	}
	l.Running = len(stack)
	for n, i := range stack {
		if dep.Variables && change < matchedEnd[i] {
			l.Exempt++
			continue
		}
		if inGrowth(orig, stack[:n], i, dep.Cycles) {
			l.Growing++
			continue
		}
		for _, run := range []struct {
			name string
			tr   []Event
			exit []int
		}{{"original", orig, exitO}, {"perturbed", pert, exitP}} {
			j := run.exit[i]
			if j < 0 {
				continue
			}
			l.Checked++
			if x := run.tr[j]; x.Examined <= change {
				l.Violations = append(l.Violations, fmt.Sprintf(
					"change point %d: the traces first differ at event %d (original %s, perturbed %s), where %s@%d (level %d, depth %d) is running, but its %s run reports examined end %d",
					change, k, differs(orig), differs(pert), x.Rule, x.Pos, x.Level, x.Depth, run.name, x.Examined))
			}
		}
	}
	return l
}

// inGrowth reports whether the call entered at tr[i] is a call of a cycle of left calls inside a growth
// of that cycle at its position: one of the enclosing calls entered at tr[j] for j in outer is at the
// same position and of a rule of the same cycle (cycles, as LeftCycles numbers them).
func inGrowth(tr []Event, outer []int, i int, cycles map[string]int) bool {
	c, ok := cycles[tr[i].Rule]
	if !ok {
		return false
	}
	for _, j := range outer {
		if d, ok := cycles[tr[j].Rule]; ok && d == c && tr[j].Pos == tr[i].Pos {
			return true
		}
	}
	return false
}

// Report sums the perturbation checks of parses.
type Report struct {
	// Parses is the number of original inputs; Perturbed the number of perturbations made, Dropped the
	// number left out (Perturbations) and Refused the number that PerturbAt could not parse at their
	// change point.
	Parses, Perturbed, Dropped, Refused int
	// Identical, Diverged and Outside count the perturbations whose trace is the original's, differs
	// while a call is running, or differs where no call is running (after the start rule returned, as
	// the check of a whole-input parse that the input ends there does).
	Identical, Diverged, Outside int
	// Checked, Exempt and Growing sum those of the comparisons (Lockstep).
	Checked, Exempt, Growing int
	Violations               []string
}

// Add adds the counts and violations of o to r.
func (r *Report) Add(o Report) {
	r.Parses += o.Parses
	r.Perturbed += o.Perturbed
	r.Dropped += o.Dropped
	r.Refused += o.Refused
	r.Identical += o.Identical
	r.Diverged += o.Diverged
	r.Outside += o.Outside
	r.Checked += o.Checked
	r.Exempt += o.Exempt
	r.Growing += o.Growing
	r.Violations = append(r.Violations, o.Violations...)
}

// Perturb checks the examined ends of the parse of in by perturbation. run parses an input and returns
// its trace. The original is parsed twice, and the two traces must agree, since the comparison relies on
// the parse being a function of its input; then each perturbation (Perturbations, for the examined ends
// of the exits of the original) is parsed and compared with the original (CheckLockstep). The
// perturbations are passed to each, if not nil, before they are parsed.
func Perturb(run func(string) []Event, in string, bytes bool, dep Dependence, alpha string, each func(Perturbation)) Report {
	return perturb(func(_ int, s string) ([]Event, bool) { return run(s), true }, false, in, bytes, dep, alpha, each)
}

// PerturbAt is Perturb for parses that depend on the change point too, as the reparse of a Document
// after an edit at it does. run(c, s) parses s for the change point c, or reports that it cannot (the
// perturbation is then counted in Refused); the original is parsed for c, twice, wherever a perturbation
// changes it at c, and compared with the perturbations at c. The examined ends that choose the change
// points are those of run(len(Units(in, bytes)), in).
func PerturbAt(run func(change int, s string) ([]Event, bool), in string, bytes bool, dep Dependence, alpha string, each func(Perturbation)) Report {
	return perturb(run, true, in, bytes, dep, alpha, each)
}

func perturb(run func(int, string) ([]Event, bool), perChange bool, in string, bytes bool, dep Dependence, alpha string, each func(Perturbation)) Report {
	r := Report{Parses: 1}
	original := func(c int) ([]Event, bool, string) {
		orig, ok := run(c, in)
		if !ok {
			return nil, false, ""
		}
		if again, _ := run(c, in); !equalTraces(orig, again) {
			return nil, false, fmt.Sprintf("two parses of the same input for change point %d have different traces (%d and %d events)", c, len(orig), len(again))
		}
		return orig, true, ""
	}
	n := len(Units(in, bytes))
	orig, ok, v := original(n)
	if !ok {
		if v == "" {
			v = fmt.Sprintf("the original %q cannot be parsed", in)
		}
		r.Violations = append(r.Violations, v)
		return r
	}
	var ends []int
	for _, e := range orig {
		if e.Kind == EventExit {
			ends = append(ends, e.Examined)
		}
	}
	type at struct {
		trace []Event
		ok    bool
	}
	origs := map[int]at{n: {orig, true}}
	ps, dropped := Perturbations(in, bytes, alpha, ends)
	r.Dropped = dropped
	for _, p := range ps {
		if each != nil {
			each(p)
		}
		r.Perturbed++
		o := orig
		if perChange {
			a, seen := origs[p.Change]
			if !seen {
				var v string
				a.trace, a.ok, v = original(p.Change)
				if v != "" {
					r.Violations = append(r.Violations, v)
				}
				origs[p.Change] = a
			}
			if !a.ok {
				r.Refused++
				continue
			}
			o = a.trace
		}
		pert, ok := run(p.Change, p.Input)
		if !ok {
			r.Violations = append(r.Violations, fmt.Sprintf("%s of %q at %d (%q): the original can be parsed for the change point, the perturbation cannot", p.Variant, in, p.Change, p.Input))
			continue
		}
		l := CheckLockstep(o, pert, p.Change, dep)
		switch {
		case !l.Diverged:
			r.Identical++
		case l.Running == 0:
			r.Outside++
		default:
			r.Diverged++
		}
		r.Checked += l.Checked
		r.Exempt += l.Exempt
		r.Growing += l.Growing
		for _, v := range l.Violations {
			r.Violations = append(r.Violations, fmt.Sprintf("%s of %q at %d (%q): %s", p.Variant, in, p.Change, p.Input, v))
		}
	}
	return r
}

func equalTraces(a, b []Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
