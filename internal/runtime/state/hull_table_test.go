package state

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestEngineValueHullTable runs the hull table (conform.HullCases) on the engine's composition hull
// (actionHull, printed from Exec IR), through the methods that the backends call to make a tuple and a list.
func TestEngineValueHullTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		prog := &SharedProgram{}
		InitSharedProgram(prog, compiletest.Program(t, "@start\nrule main = \"a\"\n", check.Options{}).Runtime())
		p := &Parser{Prog: prog, Input: NewInput(conform.BuiltinInput, input.Bytes)}
		if controlled {
			p.Control = NewBudgetControl(nil, runtime.Limits{Compute: 1 << 62})
		}
		c := &EvalCtx{P: p, Start: conform.StructRuleStart, End: conform.StructRuleEnd}
		text := p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Terminal: true, Text: "xyz", Start: 2, End: 4})
		vals := map[string]any{
			conform.MemberNil:     nil,
			conform.MemberInt:     1,
			conform.BuiltinString: "ab",
			conform.BuiltinText:   text,
			conform.BuiltinNode:   p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: 2, End: 5, Children: []*value.Node{text, text}}),
			conform.StructLate:    p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Terminal: true, Text: "q", Start: 7, End: 9}),
		}
		// The budget-controls reference build charges nothing, so its units are not checked.
		err := conform.RunHulls("engine", controlled && BudgetControlsEnabled, func(kind string, elems []string) (string, int) {
			var kids []*value.Node
			for _, e := range elems {
				n, err := element(c, kind, vals[e])
				if err != nil {
					return "error: " + err.Error(), 0
				}
				kids = append(kids, n)
			}
			var n *value.Node
			if kind == "tuple" {
				n = c.TupleNode(kids)
			} else {
				n = c.NewList(kids)
			}
			rng := fmt.Sprintf("%d-%d", nodes.D(n).Start, nodes.D(n).End)
			var before uint64
			if controlled {
				before = p.Control.Used
			}
			p.actionHull(c, kind == "tuple", nodes.D(n).Children)
			if controlled {
				return rng, int(p.Control.Used - before)
			}
			return rng, 0
		})
		if err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}
