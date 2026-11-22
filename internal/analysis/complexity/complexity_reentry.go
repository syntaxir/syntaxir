package complexity

import (
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// --- Re-entry into a growth ---

// A continuation of an alternative of a rule of a left-recursion cycle can reach, before it
// examines input, a left call of a leader of the cycle, as the last alternative a does in
// c = "(" c ")" "y" / "(" c ")" "z" / a with a = c "+" / "n". That call examines nothing that the
// alternative examined.
//
//   - While a growth of the cycle is in progress at the position, the call reads the growing
//     result there and makes no further call.
//   - Otherwise the memo answers it after the growth at the position has ended, or it starts that
//     growth, which happens once for each position and whose steps are counted as the steps of
//     any growth. Each step evaluates the bodies of the cycle at the position again, the
//     alternative included; when every scanning source evaluated at a position that is the same
//     in each step lies inside a memoized call, the steps after the first answer those calls from
//     the memo.
//
// Such a path is left out of the comparison when the alternative evaluates its scanning sources
// only inside memoized calls, the bodies of the cycle evaluate theirs that way at the positions
// that are the same in each step (fixedMemoized), and what follows the call of the leader
// contains no scanning source, so that it examines again no more than a constant amount of the
// input that the alternative examined.

// withoutReentry returns the continuations of the unit without the paths that re-enter its
// cycle, when the conditions above hold, and the continuations of the unit otherwise.
//
// A continuation all of whose paths re-enter the cycle in this way is left out as a whole: it
// examines nothing that the alternative examined before the call, and what follows the call is
// bounded (restBounded), so there is nothing left to compare. Every path of it is accounted for:
// a path that cannot be followed, re-enters nothing or is followed by a part that is not bounded
// is kept as it is.
func (k *classifier) withoutReentry(u *unit) []contSpec {
	if !reentryOn || u.kind != unitAlt || !k.an.Cyclic[u.rule] || len(u.self) != 1 || len(u.self[0].items) != 1 {
		return u.conts
	}
	cycle := k.an.Cycle[u.rule]
	if !k.fixedMemoized(cycle) || !k.memoOnly(k.nodeOf(u.self[0].items[0]), cycle) {
		return u.conts
	}
	out := make([]contSpec, 0, len(u.conts))
	for _, cs := range u.conts {
		var paths []*cont
		for _, p := range cs.paths {
			rests, first, ok := k.leftPaths(p, cycle, "")
			if !ok || len(rests) == 0 || !k.restBounded(rests) {
				paths = append(paths, p)
				continue
			}
			paths = append(paths, first...)
		}
		if len(paths) > 0 {
			cs.paths = paths
			out = append(out, cs)
		}
	}
	return out
}

// reentryOn makes withoutReentry leave out the paths of a continuation that re-enter the cycle
// of the unit. It is on by default; the variable is a test hook, whose off setting compares every
// path of the continuations with the unit.
var reentryOn = true

// fixedMemoized reports whether the bodies of the rules of the cycle evaluate every scanning
// source at a position that is the same in each step of a growth only inside a call that is
// memoized at the call and reads no variable. A position is the same in each step until the body
// has evaluated a left call of a leader of the cycle, which reads the growing result.
func (k *classifier) fixedMemoized(cycle int) bool {
	if v, ok := k.fixed[cycle]; ok {
		return v
	}
	ok := true
	for _, r := range k.rules {
		if ok && k.reach[r.Name] && k.an.Cyclic[r.Name] && k.an.Cycle[r.Name] == cycle {
			k.visitFixed(k.ruleBody(r.Name), cycle, true, &ok)
		}
	}
	if k.fixed == nil {
		k.fixed = map[int]bool{}
	}
	k.fixed[cycle] = ok
	return ok
}

// memoOnly reports whether n evaluates its scanning sources only inside calls that are memoized
// at the call and read no variable, and makes no left call of the cycle.
func (k *classifier) memoOnly(n core.Node, cycle int) bool {
	ok := true
	k.visitFixed(n, cycle, false, &ok)
	return ok
}

// visitFixed clears ok when n evaluates a scanning source outside a call that is memoized at the
// call and reads no variable. With leaders set, a left call of a leader of the cycle ends what it
// checks, and it returns whether n always evaluates one; without it, a left call of the cycle
// clears ok. A right operand (isRightOperand) is checked as the call of its rule it stands for.
func (k *classifier) visitFixed(n core.Node, cycle int, leaders bool, ok *bool) bool {
	k.control.Poll()
	if !*ok {
		return false
	}
	if rule, isCall := k.calledRule(n); isCall {
		r := k.exprOf(n).(*ast.Ref)
		if k.an.Cyclic[rule] && k.an.Cycle[rule] == cycle && !k.outside[r] {
			if !leaders {
				*ok = false
				return false
			}
			return k.an.Leaders[rule]
		}
		if (k.srcExprs[r] || k.ruleSrc[rule] != nil) && !(k.memoAt(r) && len(k.vars[rule]) == 0) {
			*ok = false
		}
		return false
	}
	if k.srcExprs[k.exprOf(n)] {
		*ok = false
		return false
	}
	switch n := n.(type) {
	case *core.Seq:
		for _, it := range n.Items {
			if k.visitFixed(it, cycle, leaders, ok) {
				return true
			}
		}
		return false
	case *core.Choice:
		all := len(n.Alts) > 0
		for _, alt := range n.Alts {
			all = k.visitFixed(alt.Body, cycle, leaders, ok) && all
		}
		return all
	case *core.Repeat:
		if n.Max == 0 {
			return false
		}
		read := k.visitFixed(n.Body, cycle, leaders, ok)
		return read && n.Min > 0
	case *core.Opt:
		k.visitFixed(n.Body, cycle, leaders, ok)
		return false
	case *core.Look:
		k.visitFixed(n.Body, cycle, leaders, ok)
		return false
	case *core.Capture:
		return k.visitFixed(n.Body, cycle, leaders, ok)
	case *core.Drop:
		return k.visitFixed(n.Body, cycle, leaders, ok)
	case *core.Text:
		return k.visitFixed(n.Body, cycle, leaders, ok)
	case *core.Label:
		return k.visitFixed(n.Body, cycle, leaders, ok)
	case *core.Missing:
		return k.visitFixed(n.Body, cycle, leaders, ok)
	case *core.Stream:
		return k.visitFixed(n.Repeat, cycle, leaders, ok)
	case *core.Recover:
		// The skip runs where the body failed, so what the body reads is not certain to be read.
		k.visitFixed(n.Body, cycle, leaders, ok)
		k.visitFixed(n.Skip, cycle, false, ok)
		return false
	case *core.Lit, *core.ClassNode, *core.Any, *core.Pred, *core.BindNode, *core.Cut,
		*core.Succeed, *core.FailNode, *core.Anchor:
		return false
	}
	if k.containsSource(n) {
		*ok = false
	}
	return false
}

// restBounded reports whether the paths contain no scanning source, up to the end of the parse
// through every call of each rule whose body they end, so that they examine a bounded amount of
// input.
func (k *classifier) restBounded(paths []*cont) bool {
	budget := leadBudget
	ended := map[endKey]bool{}
	var visit func(p *cont) bool
	visit = func(p *cont) bool {
		for ; p != nil; p = p.next {
			k.control.Poll()
			if budget--; budget < 0 {
				return false
			}
			for _, it := range p.items {
				if it == nil {
					continue
				}
				n := k.nodeOf(it)
				if a, ok := n.(*core.Anchor); ok && a.Kind == core.EndInput {
					return true
				}
				if isRightOperand(n) || k.containsSource(n) {
					return false
				}
			}
			if p.next != nil {
				continue
			}
			switch p.end {
			case endOps:
				return false
			case endRule:
				key := endKey{p.end, p.rule}
				if ended[key] {
					return true
				}
				ended[key] = true
				for _, s := range k.sites {
					if s.callee == p.rule && !visit(s.cont) {
						return false
					}
				}
			}
			return true
		}
		return true
	}
	for _, p := range paths {
		if !visit(p) {
			return false
		}
	}
	return true
}
