// Package examinedref is the independent reference of the examined end of a call (see
// spec/runtime/observability.md, Engine Tracing, and spec/runtime/documents.md, Reuse Model).
//
// It evaluates a grammar the way the language specification defines matching: without memo entries,
// contexts, first-character dispatch or generated code. It shares with the parsers only the parsed
// grammar (the AST of the frontend); no lowering, IR, memo or runtime code is used. Its growth of left
// recursion follows spec/language/parser-expressions.md (Left Recursion) and spec/runtime/parsing.md
// (Growths of Left Recursion): only the leaders of the cycles of left calls grow, and they are chosen
// by the definitions in spec/core-ir/attributes.md (facts.go); a call of a leader at a position and
// binding level that is already being evaluated reads the current result of that growth, and the
// growth repeats while a step's result ends farther.
//
// It evaluates the whole language: terminals in both position units (input.go), anchors, sequences,
// choices, repetitions, options, lookaheads, cuts, #recover, #missing with and without token
// insertion, #ignorecase, attributes, inline actions as hidden rules, Operators Rules and calls with
// a binding level (operators.go), predicates and variables, and the action language (values.go):
// the Value Actions whose values predicates read are evaluated by actions.md and types.md, and a
// runtime error of one aborts the parse (Result.ActionFailed).
package examinedref

import (
	"fmt"

	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// Exit is one completed call of a rule: the rule, the binding level (0 for a call without one), the
// start and end positions (End is Pos when the call failed), the examined end and whether it matched.
type Exit struct {
	Rule     string
	Level    int
	Pos, End int
	Examined int
	Matched  bool
	// Growing reports a call that read the current result of a growth of left recursion in progress
	// at its position, rather than evaluating the rule: the seed or a step of the growth.
	Growing bool
}

// Result is the outcome of the start call.
type Result struct {
	Matched  bool
	End      int
	Examined int
	// ActionErrors are the runtime errors of Value Actions in the order the evaluation raised them,
	// each given by the number of exits reported before it. An action runs every time its rule's
	// body matches, also on a path that is abandoned later and inside a lookahead, and its runtime
	// error aborts the parse (spec/runtime/parsing.md, Evaluation; spec/language/actions.md, Types
	// and Errors). The reference goes on with nil for the action's value, so the exits before an
	// error are those of a parse that aborts there.
	ActionErrors []int
}

// ActionFailed reports that a Value Action of the evaluation raised a runtime error.
func (r Result) ActionFailed() bool { return len(r.ActionErrors) > 0 }

// Options select the parse that the reference evaluates.
type Options struct {
	// Unit is the position unit of the positions and of the examined ends.
	Unit input.PositionUnit
	// MissingTokens enables token insertion by #missing literals (a positive WithMissingTokens); the
	// reference does not limit the insertions, so the limit of the parse must not be reached. The
	// parse is one that records no expectations, which fixes the message of an insertion.
	MissingTokens bool
	// NoStartValue is a parse that builds no value of the start rule at its top call, as a stream
	// parse does (spec/runtime/streaming.md, Entry Points): it neither evaluates the rule's action
	// nor converts the value to the rule's node type there, so an error of that action is not one
	// of the parse. Other calls of the start rule build their values. A stream's start rule is on no
	// left-recursion cycle, so the reference refuses the option for a leader.
	NoStartValue bool
}

// UnsupportedError reports a construct that the reference does not evaluate. The oracle fails on it:
// no grammar is left out.
type UnsupportedError struct {
	Construct string
}

func (e *UnsupportedError) Error() string {
	return "examinedref: outside the reference: " + e.Construct
}

// maxSteps bounds the evaluations of one Evaluate call, so that a pathological grammar fails loudly
// instead of running for long.
const maxSteps = 20_000_000

// Evaluate matches input in code points, without token insertion, against the rule start. See
// EvaluateWith.
func Evaluate(g *ast.Grammar, start, in string) (Result, []Exit, error) {
	return EvaluateWith(g, start, in, Options{})
}

// EvaluateWith matches input against the rule start and returns the outcome of that call and every
// exit of every call made, in the order they complete.
func EvaluateWith(g *ast.Grammar, start, in string, o Options) (Result, []Exit, error) {
	rs, err := newRuleSet(g)
	if err != nil {
		return Result{}, nil, err
	}
	facts, err := analyzeRules(rs, o.MissingTokens)
	if err != nil {
		return Result{}, nil, err
	}
	ev := &evaluator{
		rules:        rs.byName,
		leaders:      facts.leaders,
		opts:         o,
		in:           newText(in, o.Unit),
		growths:      map[growthKey]*growth{},
		initial:      map[string]value{},
		nodeTypes:    map[string]bool{},
		structFields: map[string]map[string]ast.TypeExpr{},
		aliases:      map[string]ast.TypeExpr{},
		valuedMemo:   map[ast.Expr]bool{},
		bottomRules:  bottomRules(rs),
	}
	ev.valuedRules = valuedRules(rs, ev.bottomRules)
	for _, d := range g.Types() {
		switch td := d.(type) {
		case *ast.TypeDef:
			if td.NodeValue() != nil {
				ev.nodeTypes[td.Name] = true
				continue
			}
			fields := map[string]ast.TypeExpr{}
			if st, ok := td.Type.(*ast.StructType); ok {
				for _, f := range st.Fields {
					fields[f.Name] = f.Type
				}
			}
			ev.structFields[td.Name] = fields
		case *ast.TypeAlias:
			ev.aliases[td.Name] = td.Type
		}
	}
	for _, v := range g.Vars() {
		val, err := ev.initialValue(v)
		if err != nil {
			return Result{}, nil, fmt.Errorf("examinedref: initial value of %s: %w", v.Name, err)
		}
		ev.initial[v.Name] = val
	}
	if _, ok := ev.rules[start]; !ok {
		return Result{}, nil, fmt.Errorf("examinedref: no rule %q", start)
	}
	if o.NoStartValue && ev.leaders[start] {
		return Result{}, nil, fmt.Errorf("examinedref: rule %s builds no value at its top call but is a leader of left recursion", start)
	}
	ev.top = true
	r := ev.call(start, 0, state{})
	if ev.err != nil {
		return Result{}, nil, ev.err
	}
	return Result{Matched: r.matched, End: r.end, Examined: r.h, ActionErrors: ev.actionErrors}, ev.exits, nil
}

// initialValue is the value a variable starts with (spec/language/predicates.md, Variables): its
// initial value, evaluated before the parse, or the zero value of its type.
func (ev *evaluator) initialValue(v *ast.VarDef) (value, error) {
	if v.Value != nil {
		return (&actionEnv{ev: ev}).eval(v.Value)
	}
	if t, ok := v.Type.(*ast.TypeRef); ok {
		switch t.Name {
		case "int":
			return int64(0), nil
		case "string":
			return "", nil
		case "bool":
			return false, nil
		}
	}
	return nil, nil
}

// state is what an evaluation starts from besides the grammar and the input.
type state struct {
	pos  int
	vars *binding  // the variable bindings visible here, newest first
	caps *capture  // the captures of the current scope, newest first
	look bool      // inside a lookahead or the skip of an Operators Rule: no token is inserted
	fold bool      // inside #ignorecase: literals match ASCII letters in either case
	path *inserted // the insertions on the current path
}

// inserted is an insertion of a #missing site at a position on the current path.
type inserted struct {
	site *ast.Attributed
	pos  int
	next *inserted
}

func (p *inserted) has(site *ast.Attributed, pos int) bool {
	for ; p != nil; p = p.next {
		if p.site == site && p.pos == pos {
			return true
		}
	}
	return false
}

// outcome is the result of an evaluation at a position: whether it matched, its end (the position
// when it failed) and the examined end h, one past the greatest slot that it or a call within it
// examined, and at least its start. cut reports that the evaluation passed a #cut that no enclosing
// choice, option, repetition or lookahead has committed yet: a failure with cut set fails the
// innermost such scope without trying its other alternatives. On a match, vars, caps and path are
// the state after it, and vals are the values that the expression contributes to the sequence around
// it, which are the elements of a sequence when seq is set.
type outcome struct {
	matched bool
	end, h  int
	cut     bool
	vars    *binding
	caps    *capture
	path    *inserted
	vals    []value
	seq     bool
}

// growthKey identifies a growth: the rule, the position and the binding level it is evaluated at.
type growthKey struct {
	rule  string
	pos   int
	level int
}

// growth is the current result of a left-recursive call being evaluated, read by a call of the same
// rule at the same position and level while the growth is in progress.
type growth struct {
	matched  bool
	end      int
	examined int
	vals     []value
	seq      bool
	path     []*inserted // the insertions of the current result
	// reentered reports that the current step read the growth.
	reentered bool
}

type evaluator struct {
	rules     map[string]*rule
	leaders   map[string]bool
	opts      Options
	in        *text
	exits     []Exit
	growths   map[growthKey]*growth
	initial   map[string]value
	nodeTypes map[string]bool
	// structFields are the declared fields of each struct type, and aliases the types that type
	// aliases name.
	structFields map[string]map[string]ast.TypeExpr
	aliases      map[string]ast.TypeExpr
	actionErrors []int
	valuedMemo   map[ast.Expr]bool
	valuedRules  map[string]bool
	bottomRules  map[string]bool
	steps        int
	err          error
	// top is set until the top call of the start rule begins.
	top bool
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

// call evaluates the rule at st.pos with the binding level (0 for a call without one). The call has a
// scope of its own: no captures, the caller's variables, which its own bindings do not change, and no
// #ignorecase. A rule that is not a leader evaluates its body once per call. A leader grows: a call
// that finds a growth of the same leader at the same position and level in progress reads its current
// result: a failure that stands for "no result yet" with examined end pos until the first step
// succeeds, and then the seed of the last step that ended farther, with the examined end of that step.
func (ev *evaluator) call(name string, level int, caller state) outcome {
	r, ok := ev.rules[name]
	if !ok {
		ev.fail(fmt.Errorf("examinedref: rule %q is not defined", name))
		return outcome{end: caller.pos, h: caller.pos}
	}
	pos := caller.pos
	st := state{pos: pos, vars: caller.vars, look: caller.look, path: caller.path}
	top := ev.top
	ev.top = false
	if !ev.leaders[name] {
		body := ev.body(r, level, st)
		if top && ev.opts.NoStartValue {
			return ev.exit(name, level, caller, valueless(st, body))
		}
		return ev.exit(name, level, caller, ev.ruleOutcome(r, st, body))
	}
	key := growthKey{name, pos, level}
	if g, ok := ev.growths[key]; ok {
		g.reentered = true
		o := outcome{matched: g.matched, end: pos, h: g.examined, vars: caller.vars, path: caller.path}
		if g.matched {
			o.end = g.end
			o.vals, o.seq = g.vals, g.seq
			for i := len(g.path) - 1; i >= 0; i-- {
				if !o.path.has(g.path[i].site, g.path[i].pos) {
					o.path = &inserted{site: g.path[i].site, pos: g.path[i].pos, next: o.path}
				}
			}
		}
		o = ev.exit(name, level, caller, o)
		ev.exits[len(ev.exits)-1].Growing = true
		return o
	}
	g := &growth{end: pos, examined: pos}
	ev.growths[key] = g
	total := pos
	for {
		g.reentered = false
		body := ev.body(r, level, st)
		if ev.err != nil {
			break
		}
		total = maxInt(total, body.h)
		if !body.matched || (g.matched && body.end <= g.end) {
			break
		}
		v := ev.ruleOutcome(r, st, body)
		g.matched, g.end, g.examined = true, body.end, body.h
		g.vals, g.seq = v.vals, v.seq
		g.path = nil
		for p := body.path; p != nil && p != st.path; p = p.next {
			g.path = append(g.path, p)
		}
		if !g.reentered {
			// A step that did not read the growth is the same as the one before it, so the next
			// step would end where this one did and the growth stops.
			break
		}
	}
	delete(ev.growths, key)
	o := outcome{matched: g.matched, end: pos, h: total, vars: caller.vars, path: caller.path}
	if g.matched {
		o.end, o.vals, o.seq = g.end, g.vals, g.seq
		for i := len(g.path) - 1; i >= 0; i-- {
			o.path = &inserted{site: g.path[i].site, pos: g.path[i].pos, next: o.path}
		}
	}
	return ev.exit(name, level, caller, o)
}

// body evaluates the body of r in the scope st.
func (ev *evaluator) body(r *rule, level int, st state) outcome {
	if r.ops != nil {
		st.fold = r.fold
		return ev.operators(r, level, st)
	}
	return ev.eval(r.body, st)
}

// ruleOutcome turns the outcome of the body of r at st into the outcome of the call: the caller's
// variables, the path of the body, and the value of the rule (spec/language/parser-expressions.md,
// Values and the Concrete Syntax Tree). A hidden rule without an action contributes what its body
// contributes, as a group written in its place; a rule with an action contributes the action's value;
// any other rule contributes one value.
func (ev *evaluator) ruleOutcome(r *rule, st state, body outcome) outcome {
	o := outcome{matched: body.matched, end: st.pos, h: body.h, path: st.path}
	if !body.matched {
		return o
	}
	o.end, o.path = body.end, body.path
	switch {
	case r.act != nil && r.ops != nil:
		// The action of an Operators Rule reads the folded value as $value.
		o.vals = []value{ev.actionValue(r.act, body.vars, &capture{name: "value", val: body.vals[0]})}
	case r.act != nil:
		o.vals = []value{ev.convert(r, ev.actionValue(r.act, body.vars, body.caps.since(st.caps)))}
	case !r.ownNode:
		o.vals, o.seq = body.vals, body.seq
	default:
		o.vals = []value{ev.ruleValue(r, st, body)}
	}
	return o
}

// valueless is the outcome of a call at st whose body has the outcome body and that builds no value:
// the outcome of ruleOutcome without the value.
func valueless(st state, body outcome) outcome {
	o := outcome{matched: body.matched, end: st.pos, h: body.h, path: st.path}
	if body.matched {
		o.end, o.path = body.end, body.path
	}
	return o
}

// ruleValue is the value of a rule without a user action: a sequence is a tuple node, even of one
// element; a body without a value is the unit; a value that is not a tuple the body creates is
// wrapped in a tuple of one element when the body has a capture outside the elements of repetitions.
func (ev *evaluator) ruleValue(r *rule, st state, body outcome) value {
	if r.ops != nil {
		// The folded value, whose operators are converted to the named node type as they are built.
		return body.vals[0]
	}
	caps := body.caps.since(st.caps)
	var v value
	switch {
	case body.seq:
		v = &node{kind: tupleNode, start: st.pos, end: body.end, elems: body.vals, caps: caps}
	case len(body.vals) == 0:
		v = &node{kind: tupleNode, start: st.pos, end: body.end, caps: caps}
	case caps != nil:
		v = &node{kind: tupleNode, start: st.pos, end: body.end, elems: body.vals, caps: caps}
	default:
		v = body.vals[0]
	}
	return ev.convert(r, v)
}

// convert converts the value of r to the named node type that r declares, if it declares one
// (spec/language/types.md, Named Node Types).
func (ev *evaluator) convert(r *rule, v value) value {
	if n, ok := v.(*node); ok && r.nodeType != "" {
		c := *n
		c.name = r.nodeType
		return &c
	}
	return v
}

// exit records the exit of a call and returns its outcome in the caller's scope: the caller's
// variables, which the bindings of the callee do not change, and the caller's captures.
func (ev *evaluator) exit(name string, level int, caller state, o outcome) outcome {
	o.vars, o.caps, o.cut = caller.vars, caller.caps, false
	if !o.matched {
		o.path = caller.path
	}
	ev.exits = append(ev.exits, Exit{Rule: name, Level: level, Pos: caller.pos, End: o.end, Examined: o.h, Matched: o.matched})
	return o
}

// single is the one value of an expression that has a value: a tuple node of its elements when it is
// a sequence, and otherwise the value it contributes.
func (ev *evaluator) single(o outcome, st state) value {
	if o.seq {
		return &node{kind: tupleNode, start: st.pos, end: o.end, elems: o.vals, caps: o.caps.since(st.caps)}
	}
	if len(o.vals) == 1 {
		return o.vals[0]
	}
	return nil
}

// failed is a failure at st with examined end h.
func failed(st state, h int, cut bool) outcome {
	return outcome{end: st.pos, h: h, cut: cut, vars: st.vars, caps: st.caps, path: st.path}
}

// matched is a match of [st.pos, end) that leaves the state as it was and contributes vals.
func matchedAt(st state, end, h int, vals ...value) outcome {
	return outcome{matched: true, end: end, h: h, vars: st.vars, caps: st.caps, path: st.path, vals: vals}
}

// examine returns the examined end after reading slot i: one past it.
func examine(h, i int) int {
	return maxInt(h, i+1)
}

// eval evaluates an expression at st.pos. A failed evaluation leaves the state as it was.
func (ev *evaluator) eval(e ast.Expr, st state) outcome {
	ev.steps++
	if ev.steps > maxSteps {
		ev.fail(fmt.Errorf("examinedref: more than %d evaluations", maxSteps))
	}
	if ev.err != nil {
		return failed(st, st.pos, false)
	}
	pos := st.pos
	switch e := e.(type) {
	case *ast.Ref:
		level := 0
		if e.Level != "" {
			l, err := ev.levelOf(e.Name, e.Level)
			if err != nil {
				ev.fail(err)
				return failed(st, pos, false)
			}
			level = l
		}
		return ev.call(e.Name, level, st)
	case *ast.Literal:
		return ev.literal(e.Value, e.IgnoreCase || st.fold, st)
	case *ast.CharClass:
		c, next, h, ok := ev.in.char(pos)
		if !ok || inRanges(e.Ranges, c) == e.Negated {
			return failed(st, h, false)
		}
		return matchedAt(st, next, h, ev.stringNode(pos, next))
	case *ast.Any:
		_, next, h, ok := ev.in.char(pos)
		if !ok {
			return failed(st, h, false)
		}
		return matchedAt(st, next, h, ev.stringNode(pos, next))
	case *ast.Top:
		return matchedAt(st, pos, pos)
	case *ast.Bottom:
		return failed(st, pos, false)
	case *ast.EndInput:
		h := examine(pos, pos)
		if pos != ev.in.n() {
			return failed(st, h, false)
		}
		return matchedAt(st, pos, h)
	case *ast.BeginInput:
		// The examined slot is the one before pos, which is below the start.
		if pos != 0 {
			return failed(st, pos, false)
		}
		return matchedAt(st, pos, pos)
	case *ast.BeginLine:
		// Testing for the beginning of a line examines the slot before pos, or the absence of one at 0.
		if pos == 0 || ev.in.lineFeedBefore(pos) {
			return matchedAt(st, pos, pos)
		}
		return failed(st, pos, false)
	case *ast.EndLine:
		// Testing for the end of a line examines slot pos: the end of the input, a line feed or a carriage
		// return there ends a line.
		h := examine(pos, pos)
		if ev.in.lineEndAt(pos) {
			return matchedAt(st, pos, h)
		}
		return failed(st, h, false)
	case *ast.Cut:
		// A cut consumes nothing and examines nothing; it marks the failures that follow it.
		o := matchedAt(st, pos, pos)
		o.cut = true
		return o
	case *ast.Seq:
		h, cur, passed := pos, st, false
		var vals []value
		for _, item := range e.Items {
			o := ev.eval(item, cur)
			h = maxInt(h, o.h)
			passed = passed || o.cut
			if !o.matched {
				return failed(st, h, passed)
			}
			vals = append(vals, o.vals...)
			cur = advance(cur, o)
		}
		o := outcome{matched: true, end: cur.pos, h: h, cut: passed, vars: cur.vars, caps: cur.caps, path: cur.path, vals: vals, seq: true}
		return o
	case *ast.Choice:
		// An alternative that fails after a cut commits the choice: the choice fails without trying the
		// others, and the failure is an ordinary one outside the choice.
		h := pos
		for _, alt := range e.Alts {
			o := ev.eval(alt, st)
			h = maxInt(h, o.h)
			if o.matched {
				o.h, o.cut = h, false
				o.vals, o.seq = ev.element(alt, e, o, st), false
				return o
			}
			if o.cut {
				break
			}
		}
		return failed(st, h, false)
	case *ast.Group:
		return ev.eval(e.Expr, st)
	case *ast.Stream:
		return ev.eval(e.Expr, st)
	case *ast.Atomic:
		o := ev.eval(e.Expr, st)
		if o.matched {
			o.vals, o.seq = []value{ev.stringNode(pos, o.end)}, false
		}
		return o
	case *ast.Discard:
		o := ev.eval(e.Expr, st)
		o.vals, o.seq = nil, false
		return o
	case *ast.Capture:
		o := ev.eval(e.Expr, st)
		if o.matched {
			var v value = &node{kind: tupleNode, start: pos, end: o.end, caps: o.caps.since(st.caps)}
			if ev.valued(e.Expr) {
				v = ev.single(o, st)
			}
			o.caps = &capture{name: e.Name, val: v, next: o.caps}
			o.vals, o.seq = []value{v}, false
		}
		return o
	case *ast.Optional:
		// The optional is a choice between matching and stopping: a failure after a cut fails it.
		o := ev.eval(e.Expr, st)
		valued := ev.valued(e.Expr) || ev.bottom(e.Expr)
		if o.matched {
			o.cut = false
			if valued {
				o.vals = []value{ev.single(o, st)}
			} else {
				o.vals = nil
			}
			o.seq = false
			return o
		}
		if o.cut {
			return failed(st, o.h, false)
		}
		if valued {
			return matchedAt(st, pos, o.h, nil)
		}
		return matchedAt(st, pos, o.h)
	case *ast.And:
		// A lookahead is a boundary for cuts: a cut inside it commits nothing outside. A positive
		// lookahead keeps the bindings and captures of its expression; no token is inserted in it.
		inner := st
		inner.look = true
		o := ev.eval(e.Expr, inner)
		if o.matched {
			return outcome{matched: true, end: pos, h: o.h, vars: o.vars, caps: o.caps, path: st.path}
		}
		return failed(st, o.h, false)
	case *ast.Not:
		inner := st
		inner.look = true
		o := ev.eval(e.Expr, inner)
		if o.matched {
			return failed(st, o.h, false)
		}
		return matchedAt(st, pos, o.h)
	case *ast.Repeat:
		return ev.repeat(e, st)
	case *ast.Recovery:
		return ev.recovery(e, st)
	case *ast.Predicate:
		return ev.predicate(e, st)
	case *ast.Attributed:
		return ev.attributed(e, len(e.Attrs), st)
	case *ast.OperatorsRule:
		ev.fail(fmt.Errorf("examinedref: an Operators Rule outside the whole body of a rule"))
		return failed(st, pos, false)
	}
	ev.fail(&UnsupportedError{Construct: fmt.Sprintf("%T", e)})
	return failed(st, pos, false)
}

// advance is the state after a match.
func advance(st state, o outcome) state {
	st.pos, st.vars, st.caps, st.path = o.end, o.vars, o.caps, o.path
	return st
}

// element is the value that a matched alternative alt gives the choice c: its one value, or nil
// when it has none and another alternative has one; nothing when no alternative has a value.
func (ev *evaluator) element(alt ast.Expr, c *ast.Choice, o outcome, st state) []value {
	if !ev.valued(c) {
		return nil
	}
	if !ev.valued(alt) {
		return []value{nil}
	}
	return []value{ev.single(o, st)}
}

// literal matches the characters of s one at a time; fold matches ASCII letters in either case.
func (ev *evaluator) literal(s string, fold bool, st state) outcome {
	h, i := st.pos, st.pos
	for _, want := range s {
		next, ch, ok := ev.in.matchChar(i, want, fold)
		h = maxInt(h, ch)
		if !ok {
			return failed(st, h, false)
		}
		i = next
	}
	return matchedAt(st, i, h, ev.stringNode(st.pos, i))
}

func sameChar(c, want rune, fold bool) bool {
	if c == want {
		return true
	}
	return fold && lowerASCII(c) == lowerASCII(want) && isASCIILetter(c)
}

func isASCIILetter(c rune) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func lowerASCII(c rune) rune {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func (ev *evaluator) stringNode(start, end int) *node {
	return &node{kind: stringNode, start: start, end: end, str: ev.in.slice(start, end)}
}

// repeat follows the repetition rule of spec/language/parser-expressions.md: it stops at the first
// failure, at the maximum, or at the first iteration that succeeds without consuming input (which
// still counts); the minimum is checked after stopping. An iteration that fails after a cut fails the
// whole repetition instead of stopping. Each iteration is a capture scope of its own.
func (ev *evaluator) repeat(e *ast.Repeat, st state) outcome {
	h, cur, count := st.pos, st, 0
	var elems []value
	for e.Max < 0 || count < e.Max {
		it := cur
		it.caps = nil
		o := ev.eval(e.Expr, it)
		h = maxInt(h, o.h)
		if !o.matched {
			if o.cut {
				return failed(st, h, false)
			}
			break
		}
		count++
		elems = append(elems, ev.iteration(e.Expr, o, it))
		progressed := o.end != cur.pos
		cur.pos, cur.vars, cur.path = o.end, o.vars, o.path
		if !progressed {
			break
		}
	}
	if count < e.Min {
		return failed(st, h, false)
	}
	o := outcome{matched: true, end: cur.pos, h: h, vars: cur.vars, caps: st.caps, path: cur.path}
	o.vals = []value{&node{kind: listNode, start: st.pos, end: cur.pos, elems: elems}}
	return o
}

// iteration is the value of one iteration: the value of the element, the unit when it has none, and
// a tuple of the element's values that holds the captures of the iteration when the element has a
// capture, whether or not this iteration made one (spec/language/parser-expressions.md, Capture).
func (ev *evaluator) iteration(body ast.Expr, o outcome, st state) value {
	caps := o.caps
	switch {
	case o.seq:
		return &node{kind: tupleNode, start: st.pos, end: o.end, elems: o.vals, caps: caps}
	case !ev.valued(body):
		return &node{kind: tupleNode, start: st.pos, end: o.end, caps: caps}
	case scopeCaptures(body):
		return &node{kind: tupleNode, start: st.pos, end: o.end, elems: o.vals, caps: caps}
	}
	return ev.single(o, st)
}

// scopeCaptures reports whether e has a capture of its own scope: one not inside a repetition, which
// has scopes of its own, a called rule or a negative lookahead, whose captures never outlive it.
func scopeCaptures(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Capture:
		return true
	case *ast.Seq:
		for _, it := range e.Items {
			if scopeCaptures(it) {
				return true
			}
		}
	case *ast.Choice:
		for _, alt := range e.Alts {
			if scopeCaptures(alt) {
				return true
			}
		}
	case *ast.Group:
		return scopeCaptures(e.Expr)
	case *ast.Optional:
		return scopeCaptures(e.Expr)
	case *ast.And:
		return scopeCaptures(e.Expr)
	case *ast.Recovery:
		return scopeCaptures(e.Expr)
	case *ast.Stream:
		return scopeCaptures(e.Expr)
	case *ast.Atomic:
		return scopeCaptures(e.Expr)
	case *ast.Discard:
		return scopeCaptures(e.Expr)
	case *ast.Attributed:
		return scopeCaptures(e.Expr)
	}
	return false
}

// recovery follows spec/language/pragmas.md (#recover): when the expression fails, the skip is
// matched from where the expression started, and if it consumes input the recovery succeeds with an
// error value covering it. Both examine what they read.
func (ev *evaluator) recovery(e *ast.Recovery, st state) outcome {
	o := ev.eval(e.Expr, st)
	if o.matched {
		return o
	}
	s := ev.eval(e.Skip, st)
	h := maxInt(o.h, s.h)
	if !s.matched || s.end == st.pos {
		return failed(st, h, o.cut || s.cut)
	}
	r := matchedAt(st, s.end, h)
	r.cut = o.cut || s.cut
	r.path = s.path
	if ev.valued(e.Expr) || ev.bottom(e.Expr) {
		r.vals = []value{&node{kind: errorNode, start: st.pos, end: s.end}}
	}
	return r
}

// predicate runs a Predicate Action: its statements in order, each binding or test failing the
// predicate on an error, and a test also on false. It consumes and examines nothing.
func (ev *evaluator) predicate(p *ast.Predicate, st state) outcome {
	stmts := []ast.ActionExpr{p.Action}
	if b, ok := p.Action.(*ast.Block); ok {
		stmts = b.Statements
	}
	vars := st.vars
	for _, s := range stmts {
		for {
			g, ok := s.(*ast.ActionGroup)
			if !ok {
				break
			}
			s = g.Action
		}
		env := &actionEnv{ev: ev, vars: vars, caps: st.caps}
		if b, ok := s.(*ast.Bind); ok {
			v, err := env.eval(b.Value)
			if err != nil {
				ev.unsupported(err)
				return failed(st, st.pos, false)
			}
			vars = &binding{name: b.Name, val: v, next: vars}
			continue
		}
		v, err := env.eval(s)
		if err != nil {
			ev.unsupported(err)
			return failed(st, st.pos, false)
		}
		if b, ok := v.(bool); ok && !b {
			return failed(st, st.pos, false)
		}
	}
	o := matchedAt(st, st.pos, st.pos)
	o.vars = vars
	return o
}

// unsupported stops the evaluation on an error that is not a run-time action error.
func (ev *evaluator) unsupported(err error) bool {
	if err == errAction {
		return false
	}
	ev.fail(err)
	return true
}

// attributed evaluates an expression with its first n attributes, which apply from the innermost
// outward in the order written: @error and @diagnostic change only diagnostics, #ignorecase makes the
// literals written in the expression case-insensitive, and #missing inserts its literal when the
// expression under it fails, insertion is enabled, the evaluation is outside every lookahead and the
// site has not inserted at the position on the current path.
func (ev *evaluator) attributed(e *ast.Attributed, n int, st state) outcome {
	if n == 0 {
		return ev.eval(e.Expr, st)
	}
	switch e.Attrs[n-1].Name {
	case "ignorecase":
		st.fold = true
		return ev.attributed(e, n-1, st)
	case "missing":
		o := ev.attributed(e, n-1, st)
		if o.matched || !ev.opts.MissingTokens || st.look || st.path.has(e, st.pos) {
			return o
		}
		r := matchedAt(st, st.pos, o.h)
		if ev.valued(e.Expr) || ev.bottom(e.Expr) {
			// The message of the insertion's error, as Node.Text gives it (pragmas.md, #missing): a parse
			// that inserts tokens records no expectations here, so it is the position and "syntax error",
			// with or without an enclosing @error (parsing.md, Syntax Errors).
			line, col := ev.in.lineCol(st.pos)
			r.vals = []value{&node{kind: missingNode, start: st.pos, end: st.pos, expected: missingText(e.Expr),
				message: fmt.Sprintf("%d:%d: syntax error", line, col)}}
		}
		r.path = &inserted{site: e, pos: st.pos, next: st.path}
		return r
	}
	return ev.attributed(e, n-1, st)
}

// missingText is the text that a #missing literal inserts: the value of the literal.
func missingText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Literal:
		return e.Value
	case *ast.Attributed:
		return missingText(e.Expr)
	case *ast.Group:
		return missingText(e.Expr)
	}
	return ""
}

// splitMissing returns the expression under the last #missing of e and that attribute, or nil when e
// has none.
func splitMissing(e *ast.Attributed) (ast.Expr, *ast.Attribute) {
	for i := len(e.Attrs) - 1; i >= 0; i-- {
		if e.Attrs[i].Name == "missing" {
			if i == 0 {
				return e.Expr, e.Attrs[i]
			}
			return &ast.Attributed{Expr: e.Expr, Attrs: e.Attrs[:i]}, e.Attrs[i]
		}
	}
	return nil, nil
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
