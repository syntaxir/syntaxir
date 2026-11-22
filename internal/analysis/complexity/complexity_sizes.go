package complexity

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Size degrees of values. The bounds on folds and lambdas take the size of a value from the
// degree, in the size n of the input, that the classifier knows for it: a capture, the value of a
// rule call, an initial value or a list built in the action. A value is **own** when its size is
// at most a constant times one more than the input that its match consumes, so that the values of
// distinct matches, such as the elements of a repetition, add up to at most a constant times n.
// The size of any other value is bounded by composing the forms of growthOf over the rules it
// reads; a rule whose value includes the value of a call on a recursion with it and that is not
// own has no degree that the classifier knows (see condition 4 of spec/runtime/parsing.md).

// unknownDeg is a degree that stands for a size the classifier does not bound by a polynomial.
const unknownDeg = 1000

// addDeg adds two degrees, keeping an unknown degree unknown.
func addDeg(a, b int) int { return min(a+b, unknownDeg) }

// sizeMemo holds the size degrees of the values of rules, computed on demand.
type sizeMemo struct {
	own    map[string]bool            // the rules whose values are own
	look   map[string]map[string]bool // the names captured inside a positive lookahead, by rule
	looked map[string]bool            // the rules whose values can hold a capture in a lookahead
	looks  map[sizeKey]bool           // the rules whose sizes, by mode, can come from such a capture
	shapes map[string]growthShape     // the shapes of the values of rules
	busy   map[string]int             // the computation that bounds each rule being computed
	cur    int                        // the innermost computation, 0 outside any
	next   int                        // the number of computations started
	hit    bool                       // the innermost computation read the shape of a busy rule
}

var (
	// ownShape is the shape of an own value, and of a list of own elements.
	ownShape = func() growthShape {
		el := sized(outsideLin)
		return growthShape{S: outsideLin, N: outsideLin, El: &el}
	}()
	// unknownShape is the shape of a value whose size the classifier does not bound.
	unknownShape = func() growthShape {
		u := lin{{o: unknownDeg}: 1}
		el := sized(u)
		return growthShape{S: u, N: u, El: &el}
	}()
)

// degOf returns the degree in the size of the input of the form x of a value outside a fold: the
// largest power of n in it, or unknownDeg when it depends on the quantities of an accumulator,
// which no fold binds there. A power of Y counts for nothing: outside a fold there is no element
// of a step, and the forms that read one bound what the element adds by the other action expressions.
func degOf(x lin) int {
	d := 0
	for m := range x {
		if m.x != [vars]int8{} {
			return unknownDeg
		}
		d = max(d, int(m.o))
	}
	return d
}

// shapeDegs returns the degrees of the size, the size of the largest element and the number of
// elements of the value of shape x.
func shapeDegs(x growthShape) [3]int { return [3]int{degOf(x.S), degOf(x.m()), degOf(x.N)} }

// degShape returns the shape of a value whose size, largest element and number of elements have
// the degrees d.
func degShape(d [3]int) growthShape {
	el := sized(lin{{o: int16(d[1])}: 1})
	return growthShape{S: lin{{o: int16(d[0])}: 1}, N: lin{{o: int16(d[2])}: 1}, El: &el}
}

// valueShape bounds the value of t in the context tc, outside the steps of a fold.
func (k *classifier) valueShape(t core.ActionExpr, tc actionCtx) growthShape {
	g, _ := k.growthOf(t, nil, tc)
	return g
}

// outsideShape bounds the value of a capture or a lambda parameter t that an action expression reads from
// outside the lambdas whose parameters growthOf follows.
func (k *classifier) outsideShape(t core.ActionExpr, tc actionCtx) growthShape {
	name, param, _ := capRef(t)
	if p, ok := tc.params[name]; ok && (param || tc.lambda) {
		if p.int {
			return growthShape{}
		}
		return p.shape
	}
	return k.captureShape(tc.rule, name)
}

// captureShape bounds the value of the capture name in rule. The operand values that an Operators
// Rule passes to its actions are values of the rule itself.
func (k *classifier) captureShape(rule, name string) growthShape {
	exprs := k.captured(rule, name)
	if len(exprs) == 0 {
		if r := k.byName[rule]; r != nil && isOperators(r.Expr) {
			return k.ruleShape(rule)
		}
		return unknownShape
	}
	var out growthShape
	for i, e := range exprs {
		s := k.exprShape(e, rule)
		if i == 0 {
			out = s
		} else {
			out = out.join(s)
		}
	}
	return out
}

// exprShape bounds the value of a match of n in rule. A repetition is a list with one element for
// each iteration; a call has the value of its rule; any other match is a node, whose text is part
// of the input and whose members are the values of the captures, calls and repetitions in it.
func (k *classifier) exprShape(n core.Node, rule string) growthShape {
	k.control.Poll()
	if k.ownExpr(n, rule, k.ownRules()) {
		return ownShape
	}
	switch x := n.(type) {
	case *core.Capture:
		return k.exprShape(x.Body, rule)
	case *core.Text:
		return k.exprShape(x.Body, rule)
	case *core.Drop:
		return k.exprShape(x.Body, rule)
	case *core.Opt:
		return k.exprShape(x.Body, rule)
	case *core.Choice:
		out := growthShape{S: lin{}, N: lin{}}
		for _, alt := range x.Alts {
			out = out.join(k.exprShape(alt.Body, rule))
		}
		return out
	case *core.Repeat:
		el := k.exprShape(x.Body, rule)
		if x.Max >= 0 {
			return growthShape{S: constLin(x.Max + 1).plus(el.S.times(constLin(x.Max))), N: constLin(x.Max), El: &el}
		}
		return growthShape{S: outsideLin.plus(el.S.times(outsideLin)), N: outsideLin, El: &el}
	}
	if name, ok := k.calledRule(n); ok {
		if k.byName[name] == nil {
			return ownShape
		}
		return k.ruleShape(name)
	}
	out := ownShape
	walkNodes(n, func(y core.Node) {
		if y == n {
			return
		}
		if _, isCall := k.calledRule(y); isCall {
			out = out.join(k.exprShape(y, rule))
			return
		}
		switch y.(type) {
		case *core.Capture, *core.Repeat:
			out = out.join(k.exprShape(y, rule))
		}
	})
	return out
}

// ruleShape bounds the value of a call of the rule name. The rules of a recursion whose values are
// not own are bounded together: what one level of the recursion adds, with the values of the calls
// on the recursion left out, has some degree d, and a value that holds the value of each such call
// at most once is a tree of at most a constant times n levels, which adds up to degree d + 1. A
// value that can hold the value of such a call twice can double over the levels, and has no shape
// that the classifier knows.
func (k *classifier) ruleShape(name string) growthShape {
	if k.ownRules()[name] {
		return ownShape
	}
	m := &k.sizes
	if s, ok := m.shapes[name]; ok {
		return s
	}
	if m.shapes == nil {
		m.shapes, m.busy = map[string]growthShape{}, map[string]int{}
	}
	if c := m.busy[name]; c != 0 {
		// The value of a call on the recursion, which the levels add up. A rule that a computation
		// reaches and that reaches back to a busy rule is on a recursion with it, so in its SCC and
		// busy in the same computation, unless its value is own and it never starts one: a busy
		// rule is only ever read by the innermost computation, which owns the hit.
		if c != m.cur {
			panic(fmt.Sprintf("internal error: the shape of rule %s is read outside the computation of its recursion", name))
		}
		m.hit = true
		return growthShape{}
	}
	members := []string{name}
	if k.recursive[name] {
		members = nil
		for _, r := range k.rules {
			if k.scc[r.Name] == k.scc[name] && !k.ownRules()[r.Name] {
				members = append(members, r.Name)
			}
		}
	}
	m.next++
	savedCur, savedHit := m.cur, m.hit
	m.cur, m.hit = m.next, false
	for _, n := range members {
		m.busy[n] = m.cur
	}
	level := growthShape{S: lin{}, N: lin{}}
	shapes := map[string]growthShape{}
	for _, n := range members {
		shapes[n] = k.levelShape(k.byName[n])
		level = level.join(shapes[n])
	}
	// The hits of this computation are its own: no rule it reads reaches a rule that an outer
	// computation holds busy, so restoring the outer hit loses none of them.
	hit := m.hit
	m.cur, m.hit = savedCur, savedHit
	for _, n := range members {
		delete(m.busy, n)
	}
	for _, n := range members {
		switch {
		case !hit:
			m.shapes[n] = shapes[n]
		case k.recOnce(members):
			d := shapeDegs(level)
			m.shapes[n] = degShape([3]int{addDeg(d[0], 1), addDeg(d[1], 1), addDeg(d[2], 1)})
		default:
			m.shapes[n] = unknownShape
		}
	}
	return m.shapes[name]
}

// levelShape bounds the value of a call of r, with the values of the calls of the rules whose
// shapes are being computed left out.
func (k *classifier) levelShape(r *ast.RuleDef) growthShape {
	if isOperators(r.Expr) {
		out := growthShape{S: lin{}, N: lin{}}
		for _, l := range valueLines(k.nodeOf(r.Expr).(*core.OperatorsRule)) {
			if l.Action != nil {
				out = out.join(k.valueShape(l.Action, actionCtx{rule: r.Name}))
			} else {
				out = out.join(k.exprShape(l.Body, r.Name))
			}
		}
		return out
	}
	if a := k.ruleAction(r.Name); a != nil {
		return k.valueShape(a, actionCtx{rule: r.Name})
	}
	return k.exprShape(k.ruleBody(r.Name), r.Name)
}

// recOnce reports whether the value of each of the rules of a recursion holds the value of each
// call on the recursion at most once: no capture of such a call is read twice, read inside a
// lambda or as the list of a lambda, which adds to each of its elements, or made in a positive
// lookahead, where it can be the value that another capture holds too.
func (k *classifier) recOnce(rules []string) bool {
	for _, name := range rules {
		r := k.byName[name]
		w := &ownWalk{k: k, rule: name, rec: map[string]int{}, ok: true}
		look := k.lookCaptures(name)
		for n := range look {
			if w.recCapture(n) {
				return false
			}
		}
		var actionExprs []core.ActionExpr
		if isOperators(r.Expr) {
			for _, l := range valueLines(k.nodeOf(r.Expr).(*core.OperatorsRule)) {
				actionExprs = append(actionExprs, l.Action)
			}
		} else {
			actionExprs = append(actionExprs, k.ruleAction(name))
		}
		for _, t := range actionExprs {
			if t != nil && !w.recUses(t, false) {
				return false
			}
		}
		for _, n := range w.rec {
			if n > 1 {
				return false
			}
		}
	}
	return true
}

// recUses counts in w.rec the reads of captures that can hold a call on a recursion with the rule
// in t, and reports false for such a read inside a lambda or in the list of a lambda, unless the
// list is a capture of a repetition and the lambdas hold each element at most once.
func (w *ownWalk) recUses(t core.ActionExpr, inLambda bool) bool {
	ok := true
	var visit func(t core.ActionExpr, inLambda bool)
	visit = func(t core.ActionExpr, inLambda bool) {
		if name, param, isRef := capRef(t); isRef {
			if !param && w.recCapture(name) {
				ok = ok && !inLambda
				w.rec["$"+name]++
			}
			return
		}
		switch t := t.(type) {
		case nil:
		case *core.Builtin:
			fi := 1
			if t.Func == "foldl" || t.Func == "foldr" {
				fi = 2
			}
			for i, a := range t.Args {
				if lam, isLam := a.(*core.Lambda); isLam {
					visit(lam.Body, true)
					continue
				}
				if (t.Func == "map" || t.Func == "foldl" || t.Func == "foldr") && i == fi-1 && w.readsRec(a) {
					lam, isLam := lambdaArg(t, fi)
					ok = ok && w.repCapture(a) && isLam && paramsOnce(lam, t.Func != "map")
				}
				visit(a, inLambda)
			}
		default:
			for _, x := range core.ActionExprChildren(t) {
				visit(x, inLambda)
			}
		}
	}
	visit(t, inLambda)
	return ok
}

// ownRules returns the rules whose values are own: the greatest set of rules whose bodies and
// actions build their values only from own parts, computed once.
func (k *classifier) ownRules() map[string]bool {
	m := &k.sizes
	if m.own != nil {
		return m.own
	}
	own := map[string]bool{}
	for _, r := range k.rules {
		own[r.Name] = true
	}
	m.own = own
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if own[r.Name] && !k.ownRule(r, own) {
				own[r.Name] = false
				changed = true
			}
		}
	}
	return own
}

// ownRule reports whether the value of r is own when the rules in own are.
func (k *classifier) ownRule(r *ast.RuleDef, own map[string]bool) bool {
	k.control.Poll()
	if isOperators(r.Expr) {
		for _, l := range valueLines(k.nodeOf(r.Expr).(*core.OperatorsRule)) {
			if !k.ownExpr(l.Body, r.Name, own) || l.Action != nil && !k.ownAction(l.Action, r.Name, own) {
				return false
			}
		}
		return true
	}
	if a := k.ruleAction(r.Name); a != nil {
		return k.ownAction(a, r.Name, own)
	}
	return k.ownExpr(k.ruleBody(r.Name), r.Name, own)
}

// ownExpr reports whether the value of a match of n is own when the rules in own are: no capture
// in a positive lookahead, which stays in effect over input that the match does not consume, is
// part of it, and the rules in it build own values.
func (k *classifier) ownExpr(n core.Node, rule string, own map[string]bool) bool {
	ok := true
	var visit func(n core.Node, look bool)
	visit = func(n core.Node, look bool) {
		walkNodes(n, func(x core.Node) {
			if !ok {
				return
			}
			if name, isCall := k.calledRule(x); isCall {
				// A lookahead discards the node of a rule it calls, so a capture in its body never
				// reaches the value.
				if _, known := own[name]; known {
					ok = own[name]
				}
				return
			}
			switch x := x.(type) {
			case *core.Capture:
				ok = !look
			case *core.Look:
				if !x.Neg && !look {
					visit(x.Body, true)
				}
			case *core.OperatorsRule:
				ok = false
			}
		})
	}
	visit(n, false)
	return ok
}

// lookCaptures returns the names that rule captures inside a positive lookahead.
func (k *classifier) lookCaptures(rule string) map[string]bool {
	m := &k.sizes
	if s, ok := m.look[rule]; ok {
		return s
	}
	if m.look == nil {
		m.look = map[string]map[string]bool{}
	}
	s := map[string]bool{}
	if r := k.byName[rule]; r != nil {
		walkNodes(k.ruleBody(rule), func(x core.Node) {
			if a, ok := x.(*core.Look); ok && !a.Neg {
				walkNodes(a.Body, func(y core.Node) {
					if c, ok := y.(*core.Capture); ok {
						s[c.Name] = true
					}
				})
			}
		})
	}
	m.look[rule] = s
	return s
}

// lookedRules returns the rules whose values can hold a capture made in a positive lookahead,
// directly or through the values of the rules and captures that they read: the least such set,
// computed once.
func (k *classifier) lookedRules() map[string]bool {
	m := &k.sizes
	if m.looked != nil {
		return m.looked
	}
	looked := map[string]bool{}
	m.looked = looked
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if !looked[r.Name] && k.ruleLooks(r) {
				looked[r.Name] = true
				changed = true
			}
		}
	}
	return looked
}

// looks reports whether the size of the value of rule, measured in mode, can come from a capture
// made in a positive lookahead, as lookedRules does but by the size that a copy of the value
// copies: a list built by list(...) has one element for each argument whatever they hold. It is
// the least such assignment, computed once by iterating sizeOf over the rules.
func (k *classifier) looks(rule string, mode sizeMode) bool {
	m := &k.sizes
	if m.looks == nil {
		k.lookedRules()
		m.looks = map[sizeKey]bool{}
		for changed := true; changed; {
			changed = false
			for _, r := range k.rules {
				for _, md := range []sizeMode{textSize, listSize} {
					key := sizeKey{r.Name, md}
					if !m.looks[key] && k.ruleLooksIn(r, md) {
						m.looks[key] = true
						changed = true
					}
				}
			}
		}
	}
	return m.looks[sizeKey{rule, mode}]
}

// ruleLooksIn reports whether the size of the value of r, measured in mode, can come from a
// capture in a positive lookahead, when the sizes in k.sizes.looks can.
func (k *classifier) ruleLooksIn(r *ast.RuleDef, mode sizeMode) bool {
	tc := actionCtx{rule: r.Name}
	switch {
	case isOperators(r.Expr):
		return k.ruleLooks(r)
	case k.ruleAction(r.Name) != nil:
		return k.sizeOf(k.ruleAction(r.Name), tc, mode).look
	default:
		return k.exprSize(k.ruleBody(r.Name), tc, mode, r.Name).look
	}
}

// ruleLooks reports whether the value of r can hold a capture in a positive lookahead, when the
// rules in lookedRules can.
func (k *classifier) ruleLooks(r *ast.RuleDef) bool {
	k.control.Poll()
	if isOperators(r.Expr) {
		for _, l := range valueLines(k.nodeOf(r.Expr).(*core.OperatorsRule)) {
			if k.exprLooks(l.Body, r.Name) || l.Action != nil && k.actionLooks(l.Action, r.Name) {
				return true
			}
		}
		return false
	}
	if a := k.ruleAction(r.Name); a != nil {
		return k.actionLooks(a, r.Name)
	}
	return k.exprLooks(k.ruleBody(r.Name), r.Name)
}

// exprLooks reports whether the value of a match of n in rule can hold a capture in a positive
// lookahead whose size is not bounded by a constant: n has such a capture, or calls a rule whose
// value can hold one.
func (k *classifier) exprLooks(n core.Node, rule string) bool {
	found := false
	walkNodes(n, func(x core.Node) {
		if found {
			return
		}
		if name, isCall := k.calledRule(x); isCall {
			found = k.sizes.looked[name]
			return
		}
		if x, ok := x.(*core.Look); ok && !x.Neg {
			walkNodes(x.Body, func(y core.Node) {
				if y, ok := y.(*core.Capture); ok && !k.boundedOf(y.Body) {
					found = true
				}
			})
		}
	})
	return found
}

// actionLooks reports whether the value of the action expression t in rule can hold a capture in a positive
// lookahead: it reads one, a capture whose value can hold one, or the matched values by index.
func (k *classifier) actionLooks(t core.ActionExpr, rule string) bool {
	found := false
	core.WalkActionExpr(t, func(x core.ActionExpr) {
		if found {
			return
		}
		name, param, ok := capRef(x)
		if !ok || param {
			return
		}
		exprs := k.captured(rule, name)
		if k.lookCaptures(rule)[name] {
			for _, e := range exprs {
				found = found || !k.boundedOf(e)
			}
			if len(exprs) == 0 || found {
				found = true
				return
			}
		}
		for _, e := range exprs {
			found = found || k.exprLooks(e, rule)
		}
	})
	return found
}

// ownWalk checks that an action expression builds an own value. Each part of an own value is at most a constant
// times the input it consumes; a part that holds the value of a call on a recursion with the rule
// may appear once, so that the parts do not double over the levels of the recursion, and a lambda
// reads nothing unbounded from outside it, which it would hold once for each element.
type ownWalk struct {
	k    *classifier
	rule string
	own  map[string]bool
	rec  map[string]int // the uses of the captures that hold a call on a recursion with the rule
	ok   bool
}

// ownLam is the lambda around an action expression in the walk.
type ownLam struct {
	params []string // the parameters of the innermost lambda
	outer  []string // the parameters of the lambdas around it
	acc    string   // the accumulator of the innermost lambda, if it is a fold
	uses   int      // the uses of acc
}

// ownAction reports whether the action expression t in rule builds an own value when the rules in own are.
func (k *classifier) ownAction(t core.ActionExpr, rule string, own map[string]bool) bool {
	w := &ownWalk{k: k, rule: rule, own: own, rec: map[string]int{}, ok: true}
	w.actionExpr(t, nil)
	for _, n := range w.rec {
		if n > 1 {
			return false
		}
	}
	return w.ok
}

// recCapture reports whether the capture name of the rule can hold a call on a recursion with it.
// The operand values of an Operators Rule are values of the rule itself.
func (w *ownWalk) recCapture(name string) bool {
	exprs := w.k.captured(w.rule, name)
	if len(exprs) == 0 {
		return true
	}
	found := false
	for _, e := range exprs {
		walkNodes(e, func(x core.Node) {
			if callee, ok := w.k.calledRule(x); ok && w.k.byName[callee] != nil && w.k.onRecursion(w.rule, callee) {
				found = true
			}
		})
	}
	return found
}

// readsRec reports whether t reads a capture that can hold a call on a recursion with the rule.
func (w *ownWalk) readsRec(t core.ActionExpr) bool {
	found := false
	core.WalkActionExpr(t, func(x core.ActionExpr) {
		if name, param, ok := capRef(x); ok && !param && w.recCapture(name) {
			found = true
		}
	})
	return found
}

func (w *ownWalk) actionExpr(t core.ActionExpr, lam *ownLam) {
	if !w.ok || t == nil {
		return
	}
	if name, param, ok := capRef(t); ok {
		w.captureRef(name, param, lam)
		return
	}
	switch t := t.(type) {
	case *core.Const, *core.Unary:
	case *core.VarRef:
		// The value of a variable is not part of the input the match consumes.
		w.ok = false
	case *core.Cast:
		w.actionExpr(t.X, lam)
	case *core.Member:
		w.actionExpr(t.X, lam)
	case *core.Convert:
		w.actionExpr(t.X, lam)
	case *core.New:
		for _, f := range t.Fields {
			w.actionExpr(f.Value, lam)
		}
	case *core.Tuple:
		for _, x := range t.Elems {
			w.actionExpr(x, lam)
		}
	case *core.Binary:
		switch {
		case t.Op == "+":
			w.actionExpr(t.L, lam)
			w.actionExpr(t.R, lam)
		case t.Op == "[]":
			// An element of a list, taken by an int.
			w.actionExpr(t.L, lam)
		default:
			// An int or a Boolean.
		}
	case *core.Builtin:
		switch t.Func {
		case "len":
		case "text", "list", "concat":
			for _, a := range t.Args {
				w.actionExpr(a, lam)
			}
		case "filter":
			// The kept elements are elements of the list.
			if len(t.Args) > 0 {
				w.actionExpr(t.Args[0], lam)
			}
		case "map", "foldl", "foldr":
			fold := t.Func != "map"
			li, fi := 0, 1
			if fold {
				li, fi = 1, 2
				if len(t.Args) > 0 {
					w.actionExpr(t.Args[0], lam)
				}
			}
			f, isLam := lambdaArg(t, fi)
			if len(t.Args) <= li || !isLam {
				w.ok = false
				return
			}
			// The lambda adds to each element of the list: over the list of a recursion, these
			// additions add up over its levels. The elements of a repetition are distinct matches
			// instead, so a lambda that holds each of them at most once holds the value of each
			// call on the recursion in them at most once.
			if w.readsRec(t.Args[li]) && !(w.repCapture(t.Args[li]) && paramsOnce(f, fold)) {
				w.ok = false
				return
			}
			w.actionExpr(t.Args[li], lam)
			inner := &ownLam{params: f.Params}
			if lam != nil {
				inner.outer = append(append(inner.outer, lam.outer...), lam.params...)
			}
			if fold && len(f.Params) > 0 {
				inner.acc = f.Params[0]
			}
			w.actionExpr(f.Body, inner)
		default:
			w.ok = false
		}
	default:
		w.ok = false
	}
}

// captureRef checks a read of the capture or lambda parameter name inside the lambda lam, if any.
func (w *ownWalk) captureRef(name string, param bool, lam *ownLam) {
	k := w.k
	if lam != nil && param {
		switch {
		case name == lam.acc:
			lam.uses++
			w.ok = lam.uses <= 1
		case containsName(lam.params, name):
		default:
			// A parameter of a lambda around the innermost one, read once for each element.
			w.ok = false
		}
		return
	}
	exprs := k.captured(w.rule, name)
	if lam != nil {
		// A value from outside the lambda is held once for each element.
		for _, e := range exprs {
			w.ok = w.ok && k.boundedOf(e)
		}
		if len(exprs) == 0 {
			w.ok = false
		}
	}
	if k.lookCaptures(w.rule)[name] {
		w.ok = false
	}
	for _, e := range exprs {
		w.ok = w.ok && k.ownExpr(e, w.rule, w.own)
	}
	if len(exprs) == 0 {
		if r := k.byName[w.rule]; r == nil || !isOperators(r.Expr) {
			w.ok = false
			return
		}
		w.ok = w.ok && w.own[w.rule]
	}
	if w.recCapture(name) {
		w.rec["$"+name]++
	}
}

// repCapture reports whether t is a capture of the rule of repetitions only, whose elements are
// distinct matches.
func (w *ownWalk) repCapture(t core.ActionExpr) bool {
	name, param, ok := capRef(t)
	if !ok || param {
		return false
	}
	exprs := w.k.captured(w.rule, name)
	for _, e := range exprs {
		if _, ok := e.(*core.Repeat); !ok {
			return false
		}
	}
	return len(exprs) > 0
}

// paramsOnce reports whether lam and every lambda inside it read each part of each of their
// parameters other than the accumulator of a fold at most once: no member path of a parameter,
// such as x or x.$a, is read twice or read with a path inside it. Distinct members of an element
// of a repetition are disjoint parts of its match. fold says whether lam is the lambda of a fold.
func paramsOnce(lam *core.Lambda, fold bool) bool {
	var params []string
	for i, n := range lam.Params {
		if i > 0 || !fold {
			params = append(params, n)
		}
	}
	var paths []string
	ok := true
	var visit func(t core.ActionExpr)
	visit = func(t core.ActionExpr) {
		if p, isPath := paramPath(t, params); isPath {
			paths = append(paths, p)
			return
		}
		if c, isCall := t.(*core.Builtin); isCall {
			fi := 1
			if c.Func == "foldl" || c.Func == "foldr" {
				fi = 2
			}
			if inner, isLam := lambdaArg(c, fi); isLam {
				ok = ok && paramsOnce(inner, fi == 2)
			}
		}
		for _, x := range core.ActionExprChildren(t) {
			visit(x)
		}
	}
	visit(lam.Body)
	for i, p := range paths {
		for _, q := range paths[i+1:] {
			if p == q || strings.HasPrefix(q, p+".") || strings.HasPrefix(p, q+".") {
				ok = false
			}
		}
	}
	return ok
}

// paramPath returns the member path of t, such as x.$a, when t is one of the parameters names or
// a member of one.
func paramPath(t core.ActionExpr, names []string) (string, bool) {
	if name, param, ok := capRef(t); ok {
		return name, param && containsName(names, name)
	}
	if t, ok := t.(*core.Member); ok {
		p, ok := paramPath(t.X, names)
		return p + "." + t.Name, ok
	}
	return "", false
}

func containsName(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

// foldAccShape returns the shape of the accumulator of the lambda of a fold call, whose degrees
// the step matrix gives from the initial value and the list.
func (k *classifier) foldAccShape(call *core.Builtin, lam *core.Lambda, tc actionCtx) growthShape {
	exp, deg, _, ok := k.foldDegrees(call, lam, tc)
	if !ok {
		return growthShape{}
	}
	if exp {
		return unknownShape
	}
	return degShape(deg)
}

// maxDeg returns the largest of the degrees d.
func maxDeg(d [3]int) int { return max(d[0], d[1], d[2]) }
