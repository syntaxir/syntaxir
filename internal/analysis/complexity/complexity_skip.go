package complexity

import "syntaxir.dev/sxir/internal/ir/core"

// Reused skips.
//
// The walks of the skip-run sites of a parse evaluate the element of each site a number of times
// linear in the input, whatever their number and order, plus a constant for each walk (Lemma 2 of
// spec/proofs/skip-runs.md). A skip expression whose parts outside its sites, and the elements of
// its sites, each examine at most a constant amount of the input therefore costs a constant for
// each of its evaluations, plus a cost linear in the input for the whole parse, and is not a
// scanning source (see "Skip Runs" in spec/runtime/parsing.md and Lemma 9 of
// spec/proofs/linear-cost.md).

// reusedSkip reports whether the skip expression of a #recover, the node skip, is a reused skip,
// and returns its unbounded repetitions, each of which is then a skip-run site: every unbounded
// repetition in it, outside the elements of others, has no #cut that applies to it
// (corepass.Facts.LooseCut), and neither its element nor the rest of the skip calls a rule, has a
// predicate, a #recover or an Operators Rule, or, in an element, an unbounded repetition. Without
// calls and predicates the other conditions of a skip-run site hold.
func reusedSkip(skip core.Node, looseCut func(core.Node) bool) ([]*core.Repeat, bool) {
	var sites []*core.Repeat
	ok, inside := true, map[core.Node]bool{}
	core.Walk(skip, func(x core.Node) {
		if inside[x] {
			return
		}
		switch x := x.(type) {
		case *core.Repeat:
			if x.Max < 0 {
				sites = append(sites, x)
				ok = ok && constantInput(x.Body) && !looseCut(x.Body)
				core.Walk(x.Body, func(y core.Node) { inside[y] = true })
			}
		default:
			ok = ok && !costly(x)
		}
	})
	return sites, ok
}

// constantInput reports whether every evaluation of n examines at most a constant amount of the
// input: n has no unbounded repetition and nothing that costly reports.
func constantInput(n core.Node) bool {
	ok := true
	core.Walk(n, func(x core.Node) {
		if r, isRep := x.(*core.Repeat); isRep {
			ok = ok && r.Max >= 0
			return
		}
		ok = ok && !costly(x)
	})
	return ok
}

// costly reports whether the node x, without its parts, can cost more than a constant in a skip
// expression: a call, which can examine any amount of the input, a predicate or a binding, whose
// action expressions can build values of any size, a #recover or an Operators Rule.
func costly(x core.Node) bool {
	switch x.(type) {
	case *core.Call, *core.Nest, *core.Pred, *core.BindNode, *core.Recover, *core.OperatorsRule:
		return true
	}
	return false
}

// reused reports whether the node skip is a reused skip.
func (k *classifier) reused(skip core.Node) bool {
	_, ok := reusedSkip(skip, k.looseCutOf)
	return ok
}
