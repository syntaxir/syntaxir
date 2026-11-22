package complexity

import (
	"fmt"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Bounded scans (see "Conditions for Linear Cost" in spec/runtime/parsing.md and Lemma 13 of
// spec/proofs/linear-cost.md).
//
// Every iteration of a repetition consumes characters of its alphabet, so the repetition reaches a
// position q only from a start inside the run of the alphabet that ends at q. The run-length
// analysis bounds how many characters of the alphabet can stand right before an evaluation of the
// repetition, which confines those starts to a window at the beginning of the run. With a constant
// number of evaluations at each start (countedWalk), the iterations at q number a constant, whether
// or not the units around the repetition commit. Memoization alone gives no such bound: it counts
// the evaluations at one position, and a run has many positions.

// runMax is the longest run before a repetition that the analysis tells from a run without a bound.
const runMax = 32

const (
	runNone  = -2         // no value: the path does not arrive
	runClear = -1         // the next character is not in the alphabet, or there is none
	runOpen  = runMax + 1 // no bound on the run
)

// runFlow is a transfer function of the run length: from a run of d characters of the alphabet
// before an expression, the run after it is at most max(a, d+b); a is runNone for no constant part,
// and b is runNone for no part that depends on d.
type runFlow struct{ a, b int }

var (
	runID     = runFlow{runNone, 0}
	runBottom = runFlow{runNone, runNone}
)

func runSat(d int) int {
	if d >= runOpen {
		return runOpen
	}
	return d
}

func (f runFlow) apply(d int) int {
	if d == runNone {
		return f.a
	}
	if f.b == runNone {
		return f.a
	}
	if f.b >= runOpen {
		return runOpen
	}
	return max(f.a, runSat(d+f.b))
}

// then returns f followed by g.
func (f runFlow) then(g runFlow) runFlow {
	out := runFlow{g.a, runNone}
	if f.a != runNone {
		out.a = max(out.a, g.apply(f.a))
	}
	if f.b != runNone && g.b != runNone {
		out.b = runSat(f.b + g.b)
	}
	return out
}

func (f runFlow) join(g runFlow) runFlow { return runFlow{max(f.a, g.a), max(f.b, g.b)} }

// star returns f applied any number of times, none included.
func (f runFlow) star() runFlow {
	out := runID
	for {
		next := out.join(out.then(f))
		if next == out {
			return out
		}
		out = next
	}
}

// runAnalysis is the run-length analysis of one alphabet.
type runAnalysis struct {
	k     *classifier
	x     charset
	rule  map[string]runFlow
	exprs map[core.Node]runFlow
	fixed map[core.Node]runFlow // the transfer functions that depend on no rule
	sure  map[core.Node]bool    // what clears has found
	pre   map[string]int
	at    map[*core.Repeat]int // the longest run before each repetition
}

func (k *classifier) subset(a, b charset) bool {
	return !a.intersects(b.complement())
}

// charFlow is the transfer function of consuming one character of s.
func (r *runAnalysis) charFlow(s charset) runFlow {
	if !s.intersects(r.x) {
		return runFlow{0, runNone}
	}
	if r.k.subset(s, r.x) {
		return runFlow{runNone, 1}
	}
	return runFlow{0, 1}
}

func (r *runAnalysis) flow(n core.Node) runFlow {
	if n == nil {
		return runID
	}
	if f, ok := r.exprs[n]; ok {
		return f
	}
	if f, ok := r.fixed[n]; ok {
		return f
	}
	f := r.compute(n)
	switch n.(type) {
	case *core.Lit, *core.ClassNode, *core.Any, *core.Look:
		r.fixed[n] = f
	default:
		r.exprs[n] = f
	}
	return f
}

// clears reports whether the next character is outside the alphabet where n fails to consume one.
func (r *runAnalysis) clears(n core.Node) bool {
	v, ok := r.sure[n]
	if !ok {
		v = r.k.subset(r.x, r.k.sureOf(n))
		r.sure[n] = v
	}
	return v
}

func (r *runAnalysis) compute(n core.Node) runFlow {
	k := r.k
	k.control.Poll()
	switch n := n.(type) {
	case nil:
		return runID
	case *core.Call:
		return r.rule[k.rules[n.Rule].Name]
	case *core.Nest:
		if isRightOperand(n) {
			return r.rule[k.rules[n.Rule].Name]
		}
		return r.flow(n.Body)
	case *core.Lit:
		f := runID
		for s := n.Text; s != ""; {
			c, size := utf8.DecodeRuneInString(s)
			s = s[size:]
			cs := charOf(c)
			if n.Fold {
				cs = asciiFold(c)
			}
			f = f.then(r.charFlow(cs))
		}
		return f
	case *core.ClassNode:
		return r.charFlow(coreClassChars(n.Class))
	case *core.Any:
		return r.charFlow(allChars)
	case *core.Seq:
		f := runID
		for _, it := range n.Items {
			f = f.then(r.flow(it))
		}
		return f
	case *core.Choice:
		f := runBottom
		for _, alt := range n.Alts {
			f = f.join(r.flow(alt.Body))
		}
		return f
	case *core.Repeat:
		if n.Max == 0 {
			return runID
		}
		body := r.flow(n.Body)
		if n.Max < 0 {
			if r.clears(n.Body) {
				return runFlow{runClear, runNone}
			}
			return body.star()
		}
		if n.Max <= runMax {
			f, step := runID, runID
			for range n.Max {
				step = step.then(body)
				f = f.join(step)
			}
			return f
		}
		return body.star()
	case *core.Opt:
		return runID.join(r.flow(n.Body))
	case *core.Look:
		if n.Neg {
			if r.clears(n.Body) {
				return runFlow{runClear, runNone}
			}
			return runID
		}
		if !k.nullableOf(n.Body) && !k.firstOf(n.Body).intersects(r.x) {
			return runFlow{runClear, runNone}
		}
		return runID
	case *core.Text:
		return r.flow(n.Body)
	case *core.Capture:
		return r.flow(n.Body)
	case *core.Drop:
		return r.flow(n.Body)
	case *core.Label:
		return r.flow(n.Body)
	case *core.Recover:
		return r.flow(n.Body).join(r.flow(n.Skip))
	case *core.Missing:
		// The insertion of the literal consumes nothing.
		return r.flow(n.Body).join(runID)
	case *core.Stream:
		return r.flow(n.Repeat)
	case *core.OperatorsRule:
		return r.operators(n).star()
	case *core.Anchor:
		if n.Kind == core.EndInput {
			return runFlow{runClear, runNone}
		}
		return runID
	case *core.FailNode:
		return runBottom
	case *core.Pred, *core.BindNode, *core.Cut, *core.Succeed:
		// Predicates, anchors, cuts and the top expression consume nothing.
		return runID
	}
	panic(fmt.Sprintf("complexity: run flow of %T", n))
}

// operators is the transfer function of one step of an Operators Rule: its skip, an operand or an
// operator part. Steps follow one another in any order, which over-approximates the order that the
// rule evaluates them in.
func (r *runAnalysis) operators(o *core.OperatorsRule) runFlow {
	f := runID
	for _, l := range o.Lines() {
		f = f.join(r.flow(l.Body))
	}
	return f
}

// solve computes the transfer function of every rule, then the longest run before every rule and
// repetition.
func (r *runAnalysis) solve() {
	k := r.k
	for _, d := range k.rules {
		r.rule[d.Name] = runBottom
	}
	for changed := true; changed; {
		changed = false
		clear(r.exprs)
		for _, d := range k.rules {
			if !k.reach[d.Name] {
				continue
			}
			if f := r.rule[d.Name].join(r.flow(k.ruleBody(d.Name))); f != r.rule[d.Name] {
				r.rule[d.Name] = f
				changed = true
			}
		}
	}
	clear(r.exprs)
	r.pre[k.start] = 0
	for changed := true; changed; {
		changed = false
		for _, d := range k.rules {
			if in, ok := r.pre[d.Name]; ok && k.reach[d.Name] {
				r.visit(k.ruleBody(d.Name), in, &changed)
			}
		}
	}
}

// visit carries the run before n into n, recording it at calls and repetitions.
func (r *runAnalysis) visit(n core.Node, in int, changed *bool) {
	k := r.k
	k.control.Poll()
	if rule, ok := k.calledRule(n); ok {
		if old, ok := r.pre[rule]; !ok || in > old {
			r.pre[rule] = in
			*changed = true
		}
		return
	}
	switch n := n.(type) {
	case *core.Seq:
		for _, it := range n.Items {
			r.visit(it, in, changed)
			in = r.flow(it).apply(in)
		}
	case *core.Choice:
		for _, alt := range n.Alts {
			r.visit(alt.Body, in, changed)
		}
	case *core.Repeat:
		if n.Max == 0 {
			return
		}
		if old, ok := r.at[n]; !ok || in > old {
			r.at[n] = in
			*changed = true
		}
		r.visit(n.Body, r.flow(n.Body).star().apply(in), changed)
	case *core.Opt:
		r.visit(n.Body, in, changed)
	case *core.Look:
		r.visit(n.Body, in, changed)
	case *core.Text:
		r.visit(n.Body, in, changed)
	case *core.Capture:
		r.visit(n.Body, in, changed)
	case *core.Drop:
		r.visit(n.Body, in, changed)
	case *core.Label:
		r.visit(n.Body, in, changed)
	case *core.Missing:
		r.visit(n.Body, in, changed)
	case *core.Recover:
		r.visit(n.Body, in, changed)
		r.visit(n.Skip, in, changed)
	case *core.Stream:
		r.visit(n.Repeat, in, changed)
	case *core.Nest:
		r.visit(n.Body, in, changed)
	case *core.OperatorsRule:
		in = r.operators(n).star().apply(in)
		for _, l := range n.Lines() {
			r.visit(l.Body, in, changed)
		}
	}
}

// sureOf returns what sure returns for n, once for each node: the analysis of each alphabet asks
// again in each pass.
func (k *classifier) sureOf(n core.Node) charset {
	s, ok := k.sureMemo[n]
	if !ok {
		s = k.sure(n)
		k.sureMemo[n] = s
	}
	return s
}

// sure returns characters on which n surely consumes: an evaluation of n where the next character
// is in the set succeeds and consumes it or more, whatever else the input holds. It
// under-approximates. A failure of a part reaches further than the part in two ways, and both end
// the proof: a part that fails after a cut fails the choice, option or repetition around it, and a
// call of a rule on a left-recursion cycle can read the failing seed of a growth. Each rule is
// followed once; a call that returns to a rule being followed answers nothing, which the
// under-approximation allows. Variables reach matching only through predicates, which sure
// answers nothing for and AlwaysSucceeds does not prove, so the answer holds whatever the variables
// that a rule reads hold, as it does for the input.
func (k *classifier) sure(n core.Node) charset {
	k.control.Poll()
	switch n := n.(type) {
	case *core.ClassNode:
		return coreClassChars(n.Class)
	case *core.Any:
		return allChars
	case *core.Lit:
		if utf8.RuneCountInString(n.Text) == 1 {
			return litFirst(n.Text, n.Fold)
		}
	case *core.Capture:
		return k.sure(n.Body)
	case *core.Drop:
		return k.sure(n.Body)
	case *core.Text:
		return k.sure(n.Body)
	case *core.Choice:
		var s charset
		for _, alt := range n.Alts {
			if k.nullableOf(alt.Body) {
				break
			}
			s = s.union(k.sure(alt.Body))
			if k.looseCutOf(alt.Body) {
				break
			}
		}
		return s
	case *core.Repeat:
		if n.Min == 1 && n.Max != 0 {
			return k.sure(n.Body)
		}
	case *core.Call:
		name := k.rules[n.Rule].Name
		if n.Level != 0 || k.an.Cyclic[name] {
			return nil
		}
		s, ok := k.sureRule[name]
		if !ok {
			k.sureRule[name] = nil
			s = k.sure(k.ruleBody(name))
			k.sureRule[name] = s
		}
		return s
	case *core.Seq:
		var away charset // first characters of what the sequence tries before the part that decides
		for i, it := range n.Items {
			switch x := it.(type) {
			case *core.Opt:
				if k.looseCutOf(x.Body) {
					return nil
				}
				away = away.union(k.firstOf(x.Body))
				continue
			case *core.Repeat:
				if x.Min == 0 {
					if k.looseCutOf(x.Body) {
						return nil
					}
					away = away.union(k.firstOf(x.Body))
					continue
				}
			case *core.Look:
				if x.Neg {
					away = away.union(k.firstOf(x.Body))
					if k.nullableOf(x.Body) {
						return nil
					}
					continue
				}
			}
			for _, later := range n.Items[i+1:] {
				if !k.succeedsOf(later) {
					return nil
				}
			}
			s := k.sure(it)
			return intersect(s, away.complement())
		}
	}
	return nil
}

// intersect returns the characters of both sets.
func intersect(a, b charset) charset {
	return a.complement().union(b.complement()).complement()
}

// consumed returns the characters that an evaluation of n can consume, through calls, outside
// lookaheads.
func (k *classifier) consumed(n core.Node, rules map[string]charset) charset {
	var s charset
	var walk func(n core.Node)
	walk = func(n core.Node) {
		if rule, ok := k.calledRule(n); ok {
			s = s.union(rules[rule])
			return
		}
		switch n := n.(type) {
		case *core.Lit:
			for _, c := range n.Text {
				if n.Fold {
					s = s.union(asciiFold(c))
				} else {
					s = s.union(charOf(c))
				}
			}
		case *core.ClassNode:
			s = s.union(coreClassChars(n.Class))
		case *core.Any:
			s = allChars
		case *core.Seq:
			for _, it := range n.Items {
				walk(it)
			}
		case *core.Choice:
			for _, alt := range n.Alts {
				walk(alt.Body)
			}
		case *core.Repeat:
			if n.Max != 0 {
				walk(n.Body)
			}
		case *core.Opt:
			walk(n.Body)
		case *core.Text:
			walk(n.Body)
		case *core.Capture:
			walk(n.Body)
		case *core.Drop:
			walk(n.Body)
		case *core.Label:
			walk(n.Body)
		case *core.Missing:
			walk(n.Body)
		case *core.Recover:
			walk(n.Body)
			walk(n.Skip)
		case *core.Stream:
			walk(n.Repeat)
		case *core.Nest:
			walk(n.Body)
		case *core.OperatorsRule:
			for _, l := range n.Lines() {
				walk(l.Body)
			}
		}
	}
	walk(n)
	return s
}

// computeBoundedScans finds the repetitions that are bounded scans and takes them out of the
// scanning sources that the conditions check. Lemma 13 counts their iterations wherever they are
// evaluated, so a check that takes one for an expression without a scanning source leaves out a
// cost that is counted already.
func (k *classifier) computeBoundedScans() {
	alphabet := map[string]charset{}
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			k.control.Poll()
			if !k.reach[r.Name] {
				continue
			}
			if s := k.consumed(k.ruleBody(r.Name), alphabet).union(alphabet[r.Name]); !s.equal(alphabet[r.Name]) {
				alphabet[r.Name] = s
				changed = true
			}
		}
	}
	k.sureMemo = map[core.Node]charset{}
	k.sureRule = map[string]charset{}
	runs := map[string]*runAnalysis{}
	k.scanRun = map[*core.Repeat]int{}
	for e := range k.srcExprs {
		rep, ok := e.(*ast.Repeat)
		if !ok {
			continue
		}
		n := k.repeatOf(rep)
		if k.again[n] == nil {
			continue
		}
		x := k.consumed(n.Body, alphabet)
		key := fmt.Sprint(x)
		r := runs[key]
		if r == nil {
			r = &runAnalysis{k: k, x: x, rule: map[string]runFlow{}, exprs: map[core.Node]runFlow{},
				fixed: map[core.Node]runFlow{}, sure: map[core.Node]bool{}, pre: map[string]int{}, at: map[*core.Repeat]int{}}
			r.solve()
			runs[key] = r
		}
		if d, ok := r.at[n]; ok {
			k.scanRun[n] = d
		}
	}
	k.regionScan = k.regionScans()
	// counted are the rules with a constant number of evaluations at one position: the memoized
	// rules off left-recursion cycles, by Lemma 3, and the rules that only such evaluations reach
	// at a bounded distance, which countedWalk clears the others from. The rules that are not
	// memoized must not be recursive, so that the counts of their callers settle theirs.
	counted := map[string]bool{}
	for _, r := range k.rules {
		if k.reach[r.Name] && !k.an.Cyclic[r.Name] && (k.memo[r.Name] || !k.recursive[r.Name]) {
			counted[r.Name] = true
		}
	}
	k.rigidRule = map[string]bool{}
	k.scanCounted = map[*core.Repeat]bool{}
	for changed := true; changed; {
		changed = false
		clear(k.scanCounted)
		for _, r := range k.rules {
			if !k.reach[r.Name] {
				continue
			}
			k.countedWalk(k.ruleBody(r.Name), counted[r.Name], counted, &changed)
		}
	}
	for e := range k.srcExprs {
		rep, ok := e.(*ast.Repeat)
		if !ok {
			continue
		}
		if n := k.repeatOf(rep); k.firstBound(n) && k.scanCounted[n] {
			delete(k.srcExprs, rep)
			if a := k.again[n]; a != nil {
				delete(k.srcExprs, k.exprOf(a))
			}
		}
	}
}

// repeatOf returns the repetition node that the repetition e stands for.
func (k *classifier) repeatOf(e *ast.Repeat) *core.Repeat {
	n := k.nodeOf(e)
	rep, ok := n.(*core.Repeat)
	if !ok {
		panic(fmt.Sprintf("complexity: repetition %s stands for %T", k.show(e), n))
	}
	return rep
}

// firstBound reports whether a constant bounds the positions from which the evaluations of rep
// that reach a position start: the run bound, at most runMax characters of its alphabet before it,
// or the region bound of the body of a trivia rule (complexity_regions.go).
func (k *classifier) firstBound(rep *core.Repeat) bool {
	d, ok := k.scanRun[rep]
	return ok && d < runOpen && d != runNone || k.regionScan[rep]
}

// rigid reports whether a constant bounds the positions from which n can match up to any one
// position. A rule takes one answer, found once: only rules that are not recursive have one to find.
func (k *classifier) rigid(n core.Node) bool {
	k.control.Poll()
	switch n := n.(type) {
	case *core.Call:
		name := k.rules[n.Rule].Name
		if k.recursive[name] {
			return false
		}
		v, ok := k.rigidRule[name]
		if !ok {
			v = k.rigid(k.ruleBody(name))
			k.rigidRule[name] = v
		}
		return v
	case *core.Nest:
		return !isRightOperand(n) && k.rigid(n.Body)
	case *core.Seq:
		for _, it := range n.Items {
			if !k.rigid(it) {
				return false
			}
		}
		return true
	case *core.Choice:
		for _, alt := range n.Alts {
			if !k.rigid(alt.Body) {
				return false
			}
		}
		return true
	case *core.Repeat:
		if n.Max >= 0 {
			return k.rigid(n.Body)
		}
		// A repetition that no path reaches (runNone) is rigid as well: it has no evaluations.
		d, ok := k.scanRun[n]
		return ok && d < runOpen || k.regionScan[n]
	case *core.Opt:
		return k.rigid(n.Body)
	case *core.Text:
		return k.rigid(n.Body)
	case *core.Capture:
		return k.rigid(n.Body)
	case *core.Drop:
		return k.rigid(n.Body)
	case *core.Label, *core.Recover, *core.Missing, *core.Stream, *core.OperatorsRule:
		return false
	}
	return true
}

// countedWalk walks n with ok saying that an evaluation of n at a position is one of a constant
// number there, marks the repetitions for which that holds, and clears the rules called where it
// does not hold.
func (k *classifier) countedWalk(n core.Node, ok bool, counted map[string]bool, changed *bool) {
	k.control.Poll()
	if rule, isCall := k.calledRule(n); isCall {
		if !ok && counted[rule] && !k.memo[rule] {
			counted[rule] = false
			*changed = true
		}
		return
	}
	switch n := n.(type) {
	case *core.Seq:
		for _, it := range n.Items {
			k.countedWalk(it, ok, counted, changed)
			ok = ok && k.rigid(it)
		}
	case *core.Choice:
		for _, alt := range n.Alts {
			k.countedWalk(alt.Body, ok, counted, changed)
		}
	case *core.Repeat:
		if n.Max == 0 {
			return
		}
		if ok && n.Max < 0 {
			k.scanCounted[n] = true
		}
		k.countedWalk(n.Body, ok && k.rigid(n), counted, changed)
	case *core.Opt:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Look:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Text:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Capture:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Drop:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Label:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Missing:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.Recover:
		k.countedWalk(n.Body, ok, counted, changed)
		k.countedWalk(n.Skip, ok, counted, changed)
	case *core.Stream:
		k.countedWalk(n.Repeat, ok, counted, changed)
	case *core.Nest:
		k.countedWalk(n.Body, ok, counted, changed)
	case *core.OperatorsRule:
		for _, l := range n.Lines() {
			k.countedWalk(l.Body, false, counted, changed)
		}
	}
}
