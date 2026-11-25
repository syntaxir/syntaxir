package flowlower

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
)

// tables are the tables of a Core module at the Flow stage (prim.Tables), which every function
// lowered from the module shares as the attributes of its module, with the ID of each class of the
// Core module. The expectations and the classes keep the order of the Core module, so that an
// expectation or a class is the same number in both IRs.
//
// They also number the outlined calls of the module's functions (prim.Outline.Call), which no table
// of the module holds: the lowering of the outlined nodes (lowerOutlined) finds the node of each by its
// number, and the plan of the table the function that runs it.
type tables struct {
	t       *prim.Tables
	classes map[*core.Class]prim.ClassID
	// outlines holds the outlined calls by number, and calls their numbers.
	outlines []outlineCall
	calls    map[outlineKey]int
	// control is the control of the lowering of the table (cancel.New of the context of the step), or nil, which the
	// lowering polls once for each Core node it lowers and each statement it emits.
	control *cancel.Control
}

// outlineCall is an outlined call: the node that a function of its own runs, the capture slots of
// the frame it runs under and whether it builds the node's value.
type outlineCall struct {
	node  core.Node
	slots []core.Slot
	build bool
}

// outlineKey identifies an outlined call by its node, the names of its slots and its building.
type outlineKey struct {
	node  core.Node
	slots string
	build bool
}

// newTables returns the tables of the Core module m.
func newTables(m *core.Module) *tables {
	tb := &tables{t: &prim.Tables{Exps: slices.Clone(m.Exps), Classes: make([]prim.Class, len(m.Classes))},
		classes: make(map[*core.Class]prim.ClassID, len(m.Classes))}
	for i, c := range m.Classes {
		ranges := make([]prim.Range, len(c.Ranges))
		for j, r := range c.Ranges {
			ranges[j] = prim.Range(r)
		}
		tb.t.Classes[i] = prim.Class{Ranges: ranges, Negated: c.Negated, Exp: prim.ExpID(c.Exp)}
		tb.classes[c] = prim.ClassID(i)
	}
	return tb
}

// class returns the ID of the class c, which the Core verifier requires to be a class of the module.
func (tb *tables) class(c *core.Class) prim.ClassID {
	id, ok := tb.classes[c]
	if !ok {
		panic(fmt.Sprintf("flowlower: a class outside the classes of the Core module: %v", *c))
	}
	return id
}

// guard returns the immediate of the guard f.
func (tb *tables) guard(f *core.First) prim.Guard {
	g := prim.Guard{Lit: f.Lit, Rune: f.Rune, Fold: f.Fold, Exp: prim.ExpID(f.Exp), Depth: f.Depth}
	if !f.Lit {
		g.Class = tb.class(f.Class)
	}
	return g
}

// lit returns the immediate of the literal n.
func lit(n *core.Lit) prim.Lit { return prim.Lit{Text: n.Text, Fold: n.Fold, Exp: prim.ExpID(n.Exp)} }

// insertion returns the immediate of the #missing site n.
func insertion(n *core.Missing) prim.Insertion { return prim.Insertion{Site: n.Site, Lit: lit(n.Lit)} }

// shape returns the immediate of the shape s.
func shape(s core.Shape) prim.Shape {
	return prim.Shape{Elems: s.Elems, K: s.K, Mode: prim.SingleMode(s.Mode), Tuple: s.Tuple, Shared: s.Shared}
}

// anchors are the immediates of the anchors and their expectations, the fixed ones of the runtime
// (core.FixedDescs).
var anchors = map[core.AnchorKind]struct {
	a   prim.Anchor
	exp prim.ExpID
}{core.BeginInput: {prim.BeginInput, 1}, core.EndInput: {prim.EndInput, 2}, core.BeginLine: {prim.BeginLine, 3}, core.EndLine: {prim.EndLine, 4}}

// outline returns the number of the outlined call of the node n under a frame with the slots,
// building its value if build: the number of the same call made before, or the next number.
func (tb *tables) outline(n core.Node, slots []core.Slot, build bool) int {
	k := outlineKey{n, strings.Join(core.SlotNames(slots), "\x00"), build}
	if i, ok := tb.calls[k]; ok {
		return i
	}
	if tb.calls == nil {
		tb.calls = map[outlineKey]int{}
	}
	tb.calls[k] = len(tb.outlines)
	tb.outlines = append(tb.outlines, outlineCall{n, slots, build})
	return len(tb.outlines) - 1
}

// terminal returns the terminal expression of the terminal region n (corepass.TerminalLeaves): its
// literals, classes, sequences, choices, options and negative lookaheads, without the Text and Drop
// nodes, which match as their bodies.
func (tb *tables) terminal(n core.Node) prim.Terminal {
	switch n := n.(type) {
	case *core.Lit:
		if n.Fold {
			panic("flowlower: a case-insensitive literal in a terminal region")
		}
		return prim.Terminal{Kind: prim.TerminalLit, Lit: lit(n)}
	case *core.ClassNode:
		return prim.Terminal{Kind: prim.TerminalClass, Class: tb.class(n.Class)}
	case *core.Seq:
		return prim.Terminal{Kind: prim.TerminalSeq, Items: tb.terminals(n.Items)}
	case *core.Choice:
		alts := make([]prim.Terminal, len(n.Alts))
		for i, a := range n.Alts {
			alts[i] = tb.terminal(a.Body)
		}
		return prim.Terminal{Kind: prim.TerminalChoice, Items: alts}
	case *core.Opt:
		return prim.Terminal{Kind: prim.TerminalOpt, Items: []prim.Terminal{tb.terminal(n.Body)}}
	case *core.Look:
		if n.Neg {
			return prim.Terminal{Kind: prim.TerminalNot, Items: []prim.Terminal{tb.terminal(n.Body)}}
		}
	case *core.Drop:
		return tb.terminal(n.Body)
	case *core.Text:
		return tb.terminal(n.Body)
	}
	panic(fmt.Sprintf("flowlower: a %T in a terminal region", n))
}

func (tb *tables) terminals(ns []core.Node) []prim.Terminal {
	out := make([]prim.Terminal, len(ns))
	for i, n := range ns {
		out[i] = tb.terminal(n)
	}
	return out
}
