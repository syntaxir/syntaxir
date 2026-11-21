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
// must be one that the reference evaluates and that fails there. A parser may make fewer calls than
// the reference, as one that reuses memo entries, resumes the repetitions of a Document or skips calls
// by their first character does, but none that the reference does not make. It returns one line per
// difference.
func CompareCalls(ref []Exit, observed []Observed) []string {
	outs := map[callKey]map[outcomeOf]bool{}
	for _, x := range ref {
		k := callKey{x.Rule, x.Level, x.Pos}
		if outs[k] == nil {
			outs[k] = map[outcomeOf]bool{}
		}
		outs[k][outcomeOf{x.Matched, x.End, x.Examined}] = true
	}
	var diffs []string
	for _, o := range observed {
		k := callKey{o.Rule, o.Level, o.Pos}
		want, ok := outs[k]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("%s: a call the reference does not make", describe(o)))
		case o.Skipped:
			failing := false
			for w := range want {
				failing = failing || !w.matched
			}
			if !failing {
				diffs = append(diffs, fmt.Sprintf("%s: skipped, but the reference matches: %s", describe(o), outcomes(want)))
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
