// Package examinedref is the independent reference of the examined end of a call (see
// spec/runtime/observability.md, Examined, and spec/runtime/documents.md, Reuse Model).
//
// It evaluates a grammar the way the language specification defines matching: without memo entries,
// contexts, first-character dispatch, binding levels or generated code. It shares with the parsers
// only the parsed grammar (the AST of the frontend); no lowering, IR, memo or runtime code is used.
// Its growth of left recursion follows spec/language/parser-expressions.md (Left Recursion) and
// spec/runtime/parsing.md (Growths of Left Recursion): only the leaders of the cycles of left calls
// grow, and they are chosen by the definitions in spec/core-ir/attributes.md (facts.go); a call of a
// leader at a position that is already being evaluated reads the current result of that growth, and
// the growth repeats while a step's result ends farther.
//
// The fragment it evaluates is the matching core: rule references without a binding level, literals
// without ignore-case, character classes, `.`, all four anchors (`$$`, `^^`, `$` and `^`), sequences, choices,
// groups, repetitions, optionals and the two lookaheads, with value-only wrappers (actions, captures, discards, #atomic,
// #stream) transparent. Other constructs make Evaluate return an *UnsupportedError; a grammar with
// one is outside the oracle and is named by the caller.
package examinedref

import (
	"fmt"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Exit is one completed call of a rule: the rule, the binding level (always 0 in the fragment), the
// start and end positions (End is Pos when the call failed), the examined end and whether it matched.
type Exit struct {
	Rule     string
	Level    int
	Pos, End int
	Examined int
	Matched  bool
}

// Result is the outcome of the start call.
type Result struct {
	Matched  bool
	End      int
	Examined int
}

// UnsupportedError reports a construct that the reference does not evaluate.
type UnsupportedError struct {
	Construct string
}

func (e *UnsupportedError) Error() string {
	return "examinedref: construct outside the reference fragment: " + e.Construct
}

// maxSteps bounds the evaluations of one Evaluate call, so that a pathological grammar fails loudly
// instead of running for long.
const maxSteps = 20_000_000

// Evaluate matches input (as code points, the position unit of the fragment) against the rule start
// and returns the outcome of that call and every exit of every call made, in the order they complete.
func Evaluate(g *ast.Grammar, start, input string) (Result, []Exit, error) {
	defs := g.Rules()
	facts, err := analyzeRules(defs)
	if err != nil {
		return Result{}, nil, err
	}
	ev := &evaluator{rules: map[string]*ast.RuleDef{}, leaders: facts.leaders, in: []rune(input), growths: map[growthKey]*growth{}}
	for _, r := range defs {
		ev.rules[r.Name] = r
	}
	if _, ok := ev.rules[start]; !ok {
		return Result{}, nil, fmt.Errorf("examinedref: no rule %q", start)
	}
	r := ev.call(start, 0)
	if ev.err != nil {
		return Result{}, nil, ev.err
	}
	return Result{Matched: r.matched, End: r.end, Examined: r.h}, ev.exits, nil
}

// outcome is the result of an evaluation at a position: whether it matched, its end (the position when
// it failed) and the examined end h, one past the greatest slot that it or a call within it examined,
// and at least its start.
type outcome struct {
	matched bool
	end, h  int
}

// growthKey identifies a growth: the rule and the position it is evaluated at. The fragment has no
// binding levels, so the level part of the key is always the same.
type growthKey struct {
	rule string
	pos  int
}

// growth is the current result of a left-recursive call being evaluated, read by a call of the same
// rule at the same position while the growth is in progress.
type growth struct {
	matched  bool
	end      int
	examined int
	// reentered reports that the current step read the growth.
	reentered bool
}

type evaluator struct {
	rules   map[string]*ast.RuleDef
	leaders map[string]bool
	in      []rune
	exits   []Exit
	growths map[growthKey]*growth
	steps   int
	err     error
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (ev *evaluator) fail(err error) {
	if ev.err == nil {
		ev.err = err
	}
}

// call evaluates the rule at pos. A rule that is not a leader (see analyzeRules) evaluates its body
// once per call. A leader grows: a call that finds a growth of the same leader at the same position in
// progress reads its current result: a failure that stands for "no result yet" with examined end pos
// until the first step succeeds, and then the seed of the last step that ended farther, with the
// examined end of that step.
func (ev *evaluator) call(name string, pos int) outcome {
	rule, ok := ev.rules[name]
	if !ok {
		ev.fail(fmt.Errorf("examinedref: rule %q is not defined", name))
		return outcome{end: pos, h: pos}
	}
	if !ev.leaders[name] {
		body := ev.eval(rule.Expr, pos)
		o := outcome{matched: body.matched, end: pos, h: body.h}
		if body.matched {
			o.end = body.end
		}
		ev.exit(name, pos, o)
		return o
	}
	key := growthKey{name, pos}
	if g, ok := ev.growths[key]; ok {
		g.reentered = true
		o := outcome{matched: g.matched, end: pos, h: g.examined}
		if g.matched {
			o.end = g.end
		}
		ev.exit(name, pos, o)
		return o
	}
	g := &growth{end: pos, examined: pos}
	ev.growths[key] = g
	total := pos
	for {
		g.reentered = false
		body := ev.eval(rule.Expr, pos)
		if ev.err != nil {
			break
		}
		total = maxInt(total, body.h)
		if !body.matched || (g.matched && body.end <= g.end) {
			break
		}
		g.matched, g.end, g.examined = true, body.end, body.h
		if !g.reentered {
			// A step that did not read the growth is the same as the one before it, so the next
			// step would end where this one did and the growth stops.
			break
		}
	}
	delete(ev.growths, key)
	o := outcome{matched: g.matched, end: pos, h: total}
	if g.matched {
		o.end = g.end
	}
	ev.exit(name, pos, o)
	return o
}

func (ev *evaluator) exit(name string, pos int, o outcome) {
	ev.exits = append(ev.exits, Exit{Rule: name, Level: 0, Pos: pos, End: o.end, Examined: o.h, Matched: o.matched})
}

// examine returns the examined end after reading slot i: one past it.
func examine(h, i int) int {
	return maxInt(h, i+1)
}

// eval evaluates an expression at pos. A failed evaluation leaves the position as it was.
func (ev *evaluator) eval(e ast.Expr, pos int) outcome {
	ev.steps++
	if ev.steps > maxSteps {
		ev.fail(fmt.Errorf("examinedref: more than %d evaluations", maxSteps))
	}
	if ev.err != nil {
		return outcome{end: pos, h: pos}
	}
	n := len(ev.in)
	ok := func(end, h int) outcome { return outcome{matched: true, end: end, h: h} }
	bad := func(h int) outcome { return outcome{end: pos, h: h} }
	switch e := e.(type) {
	case *ast.Ref:
		if e.Level != "" {
			break
		}
		return ev.call(e.Name, pos)
	case *ast.Literal:
		if e.IgnoreCase {
			break
		}
		h, i := pos, pos
		for _, c := range e.Value {
			// Reading slot i examines it, whether the character matches or is rejected; the end of
			// the input is slot n.
			h = examine(h, i)
			if i >= n || ev.in[i] != c {
				return bad(h)
			}
			i++
		}
		return ok(i, h)
	case *ast.CharClass:
		h := examine(pos, pos)
		if pos >= n {
			return bad(h)
		}
		if inRanges(e.Ranges, ev.in[pos]) == e.Negated {
			return bad(h)
		}
		return ok(pos+1, h)
	case *ast.Any:
		h := examine(pos, pos)
		if pos >= n {
			return bad(h)
		}
		return ok(pos+1, h)
	case *ast.EndInput:
		h := examine(pos, pos)
		if pos != n {
			return bad(h)
		}
		return ok(pos, h)
	case *ast.BeginInput:
		// The examined slot is the one before pos, which is below the start.
		if pos != 0 {
			return bad(pos)
		}
		return ok(pos, pos)
	case *ast.BeginLine:
		// Testing for the beginning of a line examines the slot before pos, or the absence of one at 0.
		h := pos
		if pos == 0 || ev.in[pos-1] == '\n' {
			return ok(pos, h)
		}
		return bad(h)
	case *ast.EndLine:
		// Testing for the end of a line examines slot pos: the end of the input, a line feed or a carriage
		// return there ends a line.
		h := examine(pos, pos)
		if pos == n || ev.in[pos] == '\n' || ev.in[pos] == '\r' {
			return ok(pos, h)
		}
		return bad(h)
	case *ast.Seq:
		h, cur := pos, pos
		for _, item := range e.Items {
			o := ev.eval(item, cur)
			h = maxInt(h, o.h)
			if !o.matched {
				return bad(h)
			}
			cur = o.end
		}
		return ok(cur, h)
	case *ast.Choice:
		h := pos
		for _, alt := range e.Alts {
			o := ev.eval(alt, pos)
			h = maxInt(h, o.h)
			if o.matched {
				return ok(o.end, h)
			}
		}
		return bad(h)
	case *ast.Group:
		return ev.eval(e.Expr, pos)
	case *ast.InlineAction:
		return ev.eval(e.Expr, pos)
	case *ast.Atomic:
		return ev.eval(e.Expr, pos)
	case *ast.Discard:
		return ev.eval(e.Expr, pos)
	case *ast.Capture:
		return ev.eval(e.Expr, pos)
	case *ast.Stream:
		return ev.eval(e.Expr, pos)
	case *ast.Optional:
		o := ev.eval(e.Expr, pos)
		if o.matched {
			return ok(o.end, o.h)
		}
		return ok(pos, o.h)
	case *ast.And:
		o := ev.eval(e.Expr, pos)
		if o.matched {
			return ok(pos, o.h)
		}
		return bad(o.h)
	case *ast.Not:
		o := ev.eval(e.Expr, pos)
		if o.matched {
			return bad(o.h)
		}
		return ok(pos, o.h)
	case *ast.Repeat:
		return ev.repeat(e, pos)
	default:
		ev.fail(&UnsupportedError{Construct: fmt.Sprintf("%T", e)})
		return bad(pos)
	}
	ev.fail(&UnsupportedError{Construct: describe(e)})
	return outcome{end: pos, h: pos}
}

// repeat follows the repetition rule of spec/language/parser-expressions.md: it stops at the first
// failure, at the maximum, or at the first iteration that succeeds without consuming input (which
// still counts); the minimum is checked after stopping.
func (ev *evaluator) repeat(e *ast.Repeat, pos int) outcome {
	h, cur, count := pos, pos, 0
	for e.Max < 0 || count < e.Max {
		o := ev.eval(e.Expr, cur)
		h = maxInt(h, o.h)
		if !o.matched {
			break
		}
		count++
		progressed := o.end != cur
		cur = o.end
		if !progressed {
			break
		}
	}
	if count < e.Min {
		return outcome{end: pos, h: h}
	}
	return outcome{matched: true, end: cur, h: h}
}

// inRanges reports whether c is in one of the ranges.
func inRanges(ranges []ast.CharRange, c rune) bool {
	for _, r := range ranges {
		if r.Lo <= c && c <= r.Hi {
			return true
		}
	}
	return false
}

// describe names an expression outside the fragment for the error.
func describe(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ref:
		return fmt.Sprintf("call of %s with a binding level", e.Name)
	case *ast.Literal:
		return fmt.Sprintf("literal %q with ignore-case", e.Value)
	default:
		return fmt.Sprintf("%T", e)
	}
}
