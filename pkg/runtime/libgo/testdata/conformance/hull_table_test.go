package libgo

import (
	"fmt"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The hull table (conform.HullCases) on the composition hull of the node and the typed library (actionHull,
// printed from Exec IR), through the methods that a generated parser calls to make a tuple and a list.

func TestValueHullTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		// The library's ActionHull counts no unit: the code of an action counts the elements of a hull
		// itself (HULL), which the action charge goldens check. So the table's steps, which a target whose
		// hull charges them checks, are not checked here, and under limit controls the library must count
		// nothing.
		c, vals := nodeBuiltinFixture(controlled)
		vals[conform.StructLate] = nodeOfData(&Data{Kind: MatchKind, Terminal: true, Text: "q", Start: 7, End: 9})
		c.start, c.end = conform.StructRuleStart, conform.StructRuleEnd
		if err := conform.RunHulls("node", false, func(kind string, elems []string) (string, int) {
			var n *sxirvalue.Node
			if kind == "tuple" {
				var args []any
				for _, e := range elems {
					args = append(args, vals[e])
				}
				n = c.fxTuple(args...).(*sxirvalue.Node)
			} else {
				base := len(c.P.KidStack)
				for _, e := range elems {
					c.fxPush("list", vals[e])
				}
				items := slices.Clone(c.P.KidStack[base:])
				c.P.DropKids(base)
				n = c.NewList(items)
			}
			rng := fmt.Sprintf("%d-%d", D(n).Start, D(n).End)
			var before uint64
			if controlled {
				before = c.P.UnitsNow()
			}
			c.P.ActionHull(c, kind == "tuple", D(n).Children)
			if units := c.P.UnitsNow() - before; controlled && units != 0 {
				t.Errorf("node: %s(%v): counted %d units, want 0", kind, elems, units)
			}
			return rng, 0
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
		tc, tvals := typedBuiltinFixture(controlled)
		tvals[conform.StructLate] = &TMatch{TNode: TNode[string]{Span: sxirinput.Span{Start: 7, End: 9}, Value: "q"}}
		tc.Start, tc.End = conform.StructRuleStart, conform.StructRuleEnd
		if err := conform.RunHulls("typed", false, func(kind string, elems []string) (string, int) {
			var args []any
			for _, e := range elems {
				args = append(args, tvals[e])
			}
			var n *TSeq
			if kind == "tuple" {
				n = tc.fxTuple(args...).(*TSeq)
			} else {
				n = tc.NewList(args).(*TSeq)
			}
			rng := fmt.Sprintf("%d-%d", n.Start, n.End)
			var before uint64
			if controlled {
				before = tc.P.UnitsNow()
			}
			tc.P.ActionHull(tc, kind == "tuple", n.Kids)
			if units := tc.P.UnitsNow() - before; controlled && units != 0 {
				t.Errorf("typed: %s(%v): counted %d units, want 0", kind, elems, units)
			}
			return rng, 0
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}
