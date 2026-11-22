package complexity

import (
	"math"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Budget is the bound on the compute units of a parse that the classifier derives for a grammar
// of the class Linear: a parse of n position units costs at most PerUnit * n + Fixed units. The
// units are the logical events of record 076, and n is the length of the input in the position
// unit of the parse. The constants cover both position units: a literal counts its length in
// bytes and a character or class the four bytes of its widest encoding, which is at least what a
// code-point position unit advances.
//
// The bound is derived from the structure of the grammar by the formula of record 079. Its
// constants come from the lemmas of the linear-cost proof and wait for the proof to confirm them,
// so the bound is reported and is not yet applied as the default.
type Budget struct {
	// PerUnit is c, the units for each position unit of the input, and Fixed is k, the units
	// that remain at an empty input.
	PerUnit, Fixed uint64
	// Combinations is K of the classification, which the bound includes.
	Combinations int
}

// Units returns the bound for an input of n position units, and false if it exceeds what a
// counter holds.
func (b Budget) Units(n uint64) (uint64, bool) {
	hi, lo := mulAdd(b.PerUnit, n, b.Fixed)
	return lo, hi == 0
}

func mulAdd(a, b, c uint64) (hi, lo uint64) {
	if a != 0 && b > (math.MaxUint64-c)/a {
		return 1, math.MaxUint64
	}
	return 0, a*b + c
}

// The constants of the derivation (record 079). Each says the lemma of spec/proofs/linear-cost.md
// that it comes from, and is to be confirmed by the proof.
const (
	// budgetMemoEvals is the evaluations of a memoized rule at one position and level, outside
	// growths, for each combination of the variables (Lemma 3: at most 2K).
	budgetMemoEvals = 2
	// budgetCover is the times that the iterations of one repetition cover each position of
	// the input (Lemma 4: the spans of a scanning construct overlap only at their ends, so each
	// position lies in at most two of them).
	budgetCover = 2
	// budgetClassWidth is the position units a character, a class or `.` advances, at most (the
	// widest UTF-8 encoding).
	budgetClassWidth = 4
	// budgetEntry is the units that no position carries: the entry call and the final syntax
	// error (record 076, events).
	budgetEntry = 2
)

// deriveBudget returns the bound of the grammar from the start rule, or nil when it is not
// derived: for a grammar that is not of the class Linear (the other classes depend on what no
// constant of the grammar gives), or when a count passes what a counter holds.
func (k *classifier) deriveBudget(c *Complexity) *Budget {
	if c.Class != ClassLinear {
		return nil
	}
	cycleSize := map[int]uint64{}
	for name, cyc := range k.an.Cycle {
		if k.reach[name] && k.an.Cyclic[name] {
			cycleSize[cyc]++
		}
	}
	sites := map[string]map[string]uint64{} // the call sites of a callee, by caller
	for _, r := range k.rules {
		if !k.reach[r.Name] {
			continue
		}
		astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
			if ref, ok := e.(*ast.Ref); ok && k.byName[ref.Name] != nil {
				if sites[ref.Name] == nil {
					sites[ref.Name] = map[string]uint64{}
				}
				sites[ref.Name][r.Name]++
			}
		})
	}
	over := false
	add := func(a, b uint64) uint64 {
		if a > math.MaxUint64-b {
			over = true
			return math.MaxUint64
		}
		return a + b
	}
	mul := func(a, b uint64) uint64 {
		if a != 0 && b > math.MaxUint64/a {
			over = true
			return math.MaxUint64
		}
		return a * b
	}
	// evals is the number of evaluations of the body of a rule at one position and level, for
	// each combination of the variables. A rule that the memo answers, the start rule and a rule
	// of a left-recursion cycle is evaluated at most budgetMemoEvals times (Lemma 3), and a rule
	// of a cycle also once for each growth step of each rule of the cycle (Lemma 5). Any other rule
	// is evaluated as often as the call sites of its callers evaluate it (Lemma 10).
	evals := map[string]uint64{}
	var evalsOf func(name string, active map[string]bool) uint64
	evalsOf = func(name string, active map[string]bool) uint64 {
		if v, ok := evals[name]; ok {
			return v
		}
		var v uint64
		switch {
		case k.an.Cyclic[name]:
			v = mul(budgetMemoEvals, add(1, cycleSize[k.an.Cycle[name]]))
		case k.memo[name] || name == k.start || active[name]:
			v = budgetMemoEvals
		default:
			active[name] = true
			for caller, n := range sites[name] {
				v = add(v, mul(n, evalsOf(caller, active)))
			}
			delete(active, name)
		}
		evals[name] = v
		return v
	}
	var total uint64
	for _, r := range k.rules {
		if !k.reach[r.Name] {
			continue
		}
		total = add(total, mul(evalsOf(r.Name, map[string]bool{}), k.bodyEvents(r)))
	}
	if over {
		return nil
	}
	per := mul(total, uint64(max(c.Combinations, 1)))
	if over {
		return nil
	}
	return &Budget{PerUnit: per, Fixed: add(per, budgetEntry), Combinations: max(c.Combinations, 1)}
}

// bodyEvents returns the events of one evaluation of the body of r at one position, as the
// units of record 076 count them: a call and the key of a call that the memo answers, the
// advance of a terminal, a syntax error that a terminal or a recovery can build, and a value
// that a capture, a predicate or an action builds. The events inside a repetition happen once
// for each iteration, and the iterations cover each position budgetCover times (Lemma 4), so
// they count budgetCover times, once more for each enclosing repetition.
func (k *classifier) bodyEvents(r *ast.RuleDef) uint64 {
	var total uint64
	sat := func(a, b uint64) uint64 {
		if a > math.MaxUint64-b {
			return math.MaxUint64
		}
		return a + b
	}
	var node func(e ast.Expr) uint64
	node = func(e ast.Expr) uint64 {
		switch x := e.(type) {
		case *ast.Ref:
			if k.byName[x.Name] == nil {
				return 0
			}
			n := uint64(1)
			if k.memo[x.Name] {
				n += uint64(len(k.vars[x.Name]))
			}
			return n
		case *ast.Literal:
			return uint64(len(x.Value)) + 1
		case *ast.CharClass, *ast.Any:
			return budgetClassWidth + 1
		case *ast.Recovery, *ast.Capture, *ast.Predicate, *ast.InlineAction:
			return 1
		}
		return 0
	}
	astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
		total = sat(total, node(e))
		if rep, ok := e.(*ast.Repeat); ok && !(rep.Max == 1) {
			var inner uint64
			astfacts.WalkExpr(rep.Expr, func(y ast.Expr) { inner = sat(inner, node(y)) })
			for range budgetCover - 1 {
				total = sat(total, inner)
			}
		}
	})
	if r.Action != nil {
		total = sat(total, 1)
	}
	return total
}
