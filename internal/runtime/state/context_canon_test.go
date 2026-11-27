package state

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// canonTree builds the result tree of a growth step over "a+b" at the generation mark: a sum of
// two terms around an empty node between the first term and the operator, with a field naming the
// operator. change alters one part of it while its root keeps its kind and its range.
func canonTree(mark uint32, change string) *value.Node {
	term := valueimpl.KindOf("Term", "term")
	at := func(d nodes.Data) *value.Node {
		d.Mark = mark
		return nodes.Ptr(d)
	}
	text := "b"
	if change == "text" {
		text = "c"
	}
	termKind := term
	if change == "kind" {
		termKind = valueimpl.KindOf("Other", "term")
	}
	children := []*value.Node{
		at(nodes.Data{Kind: term, Start: 0, End: 1, Text: "a", Terminal: true}),
		at(nodes.Data{Kind: valueimpl.MatchKind, Start: 1, End: 1}),
		at(nodes.Data{Kind: termKind, Start: 2, End: 3, Text: text, Terminal: true}),
	}
	switch change {
	case "range":
		nodes.D(children[2]).Start = 1
	case "children":
		children = children[:2]
	case "nil child":
		children[1] = nil
	}
	op := "+"
	if change == "field" {
		op = "-"
	}
	return at(nodes.Data{Kind: valueimpl.KindOf("Sum", "sum"), Start: 0, End: 3, Children: children,
		Fields: valueimpl.Fields{{Name: "op", Value: op}}})
}

// remark sets the generation of the node n of a tree and returns the tree.
func remark(tree, n *value.Node, mark uint32) *value.Node {
	nodes.D(n).Mark = mark
	return tree
}

// A growth step result takes the identity of an earlier result of the same growth and end at the
// same position when their trees are equal by value (spec/runtime/parsing.md, Evaluations of
// Rules), whatever the generations of their nodes: a Document parse holds a result it reused
// without moving it (no edit before it shifted it) with the nodes of an earlier generation, and an
// empty node of a moved result keeps its generation when its parent did not move. A result that
// differs in any part below an equal root gets an identity of its own.
func TestCanonicalSeedComparesByValue(t *testing.T) {
	t.Parallel()
	const gen = 3
	fresh := func() *value.Node { return canonTree(gen, "") }
	equal := map[string]func() *value.Node{
		"fresh":                func() *value.Node { return canonTree(gen, "") },
		"reused without moves": func() *value.Node { return canonTree(1, "") },
		"old empty node": func() *value.Node {
			tree := canonTree(gen, "")
			return remark(tree, nodes.D(tree).Children[1], 1)
		},
		"old root": func() *value.Node {
			tree := canonTree(gen, "")
			return remark(tree, tree, 2)
		},
	}
	different := []string{"text", "kind", "range", "children", "nil child", "field"}
	step := func(n *value.Node) *MemoEntry {
		return &MemoEntry{Ok: true, End: 3, Examined: 4, From: 0, Node: n}
	}
	g := growKey{rule: 1}
	for name, held := range equal {
		p := &Parser{Gen: gen, MemoAll: true}
		first := step(held())
		e, id := p.canonicalSeed(0, 0, g, first)
		if e != first {
			t.Fatalf("%s: the first result was replaced", name)
		}
		later := step(fresh())
		if e, id2 := p.canonicalSeed(0, 0, g, later); e != first || id2 != id {
			t.Errorf("%s: an equal result got identity %d, not %d", name, id2, id)
		}
	}
	for _, change := range different {
		p := &Parser{Gen: gen, MemoAll: true}
		first := step(fresh())
		_, id := p.canonicalSeed(0, 0, g, first)
		later := step(canonTree(gen, change))
		if e, id2 := p.canonicalSeed(0, 0, g, later); e != later || id2 == id {
			t.Errorf("%s: a different result shared identity %d", change, id)
		}
	}
}
