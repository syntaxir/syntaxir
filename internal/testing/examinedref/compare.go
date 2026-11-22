package examinedref

import (
	"fmt"
	"sort"

	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// Observed is a call that a parser reported: the exit of a call, with its depth, or a call that the
// parser skipped because it could not match (instrument.TraceSkipped), which has no end or examined
// end of its own.
type Observed struct {
	Exit
	Depth   int
	Skipped bool
}

// Recorder collects the exits and the skipped calls of a parse. Its Trace method is a trace function
// for the parsers' options; Reset empties it between parses.
type Recorder struct {
	Calls []Observed
}

// Trace records an exit or a skipped call and ignores the enter events.
func (r *Recorder) Trace(e instrument.TraceEvent) {
	if o, ok := ObservedOf(e); ok {
		r.Calls = append(r.Calls, o)
	}
}

// Reset forgets the recorded calls.
func (r *Recorder) Reset() { r.Calls = r.Calls[:0] }

// ObservedOf is the call that a trace event reports, or false for an enter event.
func ObservedOf(e instrument.TraceEvent) (Observed, bool) {
	switch e.Kind {
	case instrument.TraceExit:
		return Observed{Exit: Exit{Rule: e.Rule, Level: e.Level, Pos: e.Pos, End: e.End, Examined: e.Examined, Matched: e.Matched}, Depth: e.Depth}, true
	case instrument.TraceSkipped:
		return Observed{Exit: Exit{Rule: e.Rule, Level: e.Level, Pos: e.Pos, End: e.Pos}, Depth: e.Depth, Skipped: true}, true
	}
	return Observed{}, false
}

// callKey identifies the calls of a rule at a position with a binding level.
type callKey struct {
	rule       string
	level, pos int
}

// outcomeOf is what a call returned: whether it matched, its end and its examined end.
type outcomeOf struct {
	matched       bool
	end, examined int
}

// CompareCalls checks the calls a parser reported against the exits of the reference for the same
// grammar, input and options. Every reported exit must be an exit of the reference: a call of the same
// rule at the same position with the same binding level that returned the same match, end and
// examined end. A key may have several reference outcomes: the steps of a growth of left recursion,
// and calls made with different variable values or token insertions on their paths. Every skipped call
// must be one that the reference evaluates and that fails there: a skip stands for evaluating the
// rule, so a failure that only reads the seed of a growth in progress (Exit.Growing) does not
// justify it. A parser may make fewer calls than
// the reference, as one that reuses memo entries, resumes the repetitions of a Document or skips calls
// by their first character does, but none that the reference does not make. It returns one line per
// difference.
func CompareCalls(ref []Exit, observed []Observed) []string {
	outs := map[callKey]map[outcomeOf]bool{}
	evaluatedFails := map[callKey]bool{}
	for _, x := range ref {
		k := callKey{x.Rule, x.Level, x.Pos}
		if outs[k] == nil {
			outs[k] = map[outcomeOf]bool{}
		}
		outs[k][outcomeOf{x.Matched, x.End, x.Examined}] = true
		if !x.Matched && !x.Growing {
			evaluatedFails[k] = true
		}
	}
	var diffs []string
	for _, o := range observed {
		k := callKey{o.Rule, o.Level, o.Pos}
		want, ok := outs[k]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("%s: a call the reference does not make", describe(o)))
		case o.Skipped:
			if !evaluatedFails[k] {
				diffs = append(diffs, fmt.Sprintf("%s: skipped, but no evaluation of the reference fails: %s", describe(o), outcomes(want)))
			}
		case !want[outcomeOf{o.Matched, o.End, o.Examined}]:
			diffs = append(diffs, fmt.Sprintf("%s: reference %s", describe(o), outcomes(want)))
		}
	}
	return diffs
}

// CompareStart checks the exit of the start call, the last reported exit at depth 1, against the
// result of the reference: the match, the end and the examined end must all be the same. It returns
// one line per difference, including a missing exit of the start call.
func CompareStart(res Result, start string, observed []Observed) []string {
	for i := len(observed) - 1; i >= 0; i-- {
		o := observed[i]
		if o.Depth != 1 || o.Skipped {
			continue
		}
		if o.Rule != start || o.Pos != 0 || o.Level != 0 {
			return []string{fmt.Sprintf("start call: %s, want %s at 0", describe(o), start)}
		}
		if o.Matched != res.Matched || o.End != res.End || o.Examined != res.Examined {
			return []string{fmt.Sprintf("start call: %s: reference matched %v, end %d, examined %d",
				describe(o), res.Matched, res.End, res.Examined)}
		}
		return nil
	}
	return []string{fmt.Sprintf("start call: no exit of %s reported", start)}
}

// Actions tells which Value Actions a parse runs: every action, as a parse that builds values does, or
// only those whose values predicates read, as recognition does (spec/glossary.md, recognition).
type Actions bool

const (
	// EveryAction is a parse that runs every Value Action whose rule's body matches.
	EveryAction Actions = true
	// ReadActions is a parse that runs only the Value Actions whose values predicates read.
	ReadActions Actions = false
)

// CompareParse checks one observed parse against the reference's evaluation of its input. A parse
// that completes is checked by CompareCalls and CompareStart. A parse aborts on an error that is not a
// syntax error, which in a parse without limits is the runtime error of a Value Action, and it aborts
// at the first such error it meets. A parse that runs every action meets the reference's first error
// (Result.ActionErrors) and must abort there; one that runs only the actions predicates read may
// abort at any of them, and need not abort. A parse that aborts at an error must have reported only
// exits that the reference reports before it (CompareCalls over that prefix) and, as its last call,
// the reference's last call before it: the call that matched or failed just before the action ran.
// It returns one line per difference.
func CompareParse(res Result, exits []Exit, start string, observed []Observed, aborted bool, actions Actions) []string {
	if !aborted {
		diffs := append(CompareCalls(exits, observed), CompareStart(res, start, observed)...)
		if actions == EveryAction && res.ActionFailed() {
			diffs = append(diffs, fmt.Sprintf("the parse completed, but a Value Action of the reference fails after %d exits", res.ActionErrors[0]))
		}
		return diffs
	}
	if !res.ActionFailed() {
		return []string{"the parse aborted, but no Value Action of the reference fails"}
	}
	points := res.ActionErrors
	if actions == EveryAction {
		points = points[:1]
	}
	var first []string
	for i, k := range points {
		diffs := append(CompareCalls(exits[:k], observed), lastCall(exits[:k], observed)...)
		if len(diffs) == 0 {
			return nil
		}
		if i == 0 {
			first = diffs
		}
	}
	return first
}

// lastCall checks that the last call a parse reported before it aborted is the last exit of the
// reference before the failing action: the same call and, unless the parse skipped it, the same
// outcome.
func lastCall(exits []Exit, observed []Observed) []string {
	if len(exits) == 0 {
		if len(observed) > 0 {
			return []string{fmt.Sprintf("aborted after %s, but the reference's action fails before any exit", describe(observed[len(observed)-1]))}
		}
		return nil
	}
	want := exits[len(exits)-1]
	if len(observed) == 0 {
		return []string{fmt.Sprintf("aborted before any call, but the reference's action fails after %s", describe(Observed{Exit: want}))}
	}
	o := observed[len(observed)-1]
	same := o.Rule == want.Rule && o.Level == want.Level && o.Pos == want.Pos
	if same && !o.Skipped {
		same = o.Matched == want.Matched && o.End == want.End && o.Examined == want.Examined
	}
	if !same {
		return []string{fmt.Sprintf("aborted after %s, but the reference's action fails after %s", describe(o), describe(Observed{Exit: want}))}
	}
	return nil
}

func describe(o Observed) string {
	call := fmt.Sprintf("%s@%d", o.Rule, o.Pos)
	if o.Level != 0 {
		call = fmt.Sprintf("%s:%d@%d", o.Rule, o.Level, o.Pos)
	}
	if o.Skipped {
		return call + " (skipped)"
	}
	return fmt.Sprintf("%s matched %v, end %d, examined %d", call, o.Matched, o.End, o.Examined)
}

func outcomes(m map[outcomeOf]bool) string {
	var list []outcomeOf
	for w := range m {
		list = append(list, w)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.matched != b.matched {
			return !a.matched
		}
		if a.end != b.end {
			return a.end < b.end
		}
		return a.examined < b.examined
	})
	s := ""
	for i, w := range list {
		if i > 0 {
			s += " or "
		}
		s += fmt.Sprintf("(matched %v, end %d, examined %d)", w.matched, w.end, w.examined)
	}
	return s
}
