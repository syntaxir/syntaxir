package libgo

import (
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The built-in table (conform.BuiltinCases) on the compositions length, text, items and convert
// of the node and the typed library (ActionLength, ActionText, ActionItems and ActionConvert,
// printed from Exec IR), through the methods that a generated parser calls.

// nodeBuiltinFixture returns the context of a node parser over conform.BuiltinInput, with a budget
// control if controlled, and the values of the fixture.
func nodeBuiltinFixture(controlled bool) (*ActCtx, map[string]any) {
	g := NewGrammar(100)
	for _, typ := range []string{conform.BuiltinType, conform.BuiltinOther} {
		g.NodeTypes[typ] = NewNodeTypeKinds(typ)
	}
	p := &Parser{g: g, unit: sxirinput.Bytes, bs: conform.BuiltinInput}
	if controlled {
		conformUnits(p, 0)
	}
	c := &ActCtx{P: p}
	text := nodeOfData(&Data{Kind: MatchKind, Terminal: true, Text: "xyz", Start: 2, End: 4})
	typed, _ := g.NodeTypes[conform.BuiltinOther].convert(text)
	return c, map[string]any{
		conform.BuiltinTyped:  typed,
		conform.BuiltinBox:    nodeOfData(&Data{Kind: valueimpl.BoxKind, Box: 1}),
		conform.MemberNil:     nil,
		conform.MemberInt:     1,
		conform.BuiltinString: "ab",
		conform.BuiltinBool:   true,
		conform.BuiltinText:   text,
		conform.BuiltinNode:   nodeOfData(&Data{Kind: SeqKind, Start: 2, End: 5, Children: []*sxirvalue.Node{text, text}}),
		conform.BuiltinPair:   c.fxList(1, 2),
		conform.MemberList:    c.fxList(),
		conform.BuiltinFunc:   func(x any) any { return x },
	}
}

// typedBuiltinFixture returns the context of a typed parser over conform.BuiltinInput and the values of
// the fixture.
func typedBuiltinFixture(controlled bool) (*TActCtx, map[string]any) {
	p := &Parser{unit: sxirinput.Bytes, bs: conform.BuiltinInput}
	if controlled {
		conformUnits(p, 0)
	}
	c := &TActCtx{P: &TParser{Parser: p}}
	text := &TMatch{TNode: TNode[string]{Span: sxirinput.Span{Start: 2, End: 4}, Value: "xyz"}}
	return c, map[string]any{
		conform.BuiltinTyped:  &TMatch{TNode: text.TNode, Nt: builtinOtherNt},
		conform.BuiltinBox:    1,
		conform.MemberNil:     nil,
		conform.MemberInt:     1,
		conform.BuiltinString: "ab",
		conform.BuiltinBool:   true,
		conform.BuiltinText:   text,
		conform.BuiltinNode:   &TSeq{Typ: "tuple", Span: sxirinput.Span{Start: 2, End: 5}, Kids: []any{text, text}},
		conform.BuiltinPair:   &TSeq{Typ: TypeListValue, Kids: []any{1, 2}},
		conform.MemberList:    &TSeq{Typ: TypeListValue},
		conform.BuiltinFunc:   func(x any) any { return x },
	}
}

// builtinTypeNt and builtinOtherNt are the indexes of the named node types of the fixture in the typed
// library.
var (
	builtinTypeNt  = TRegisterNodeType(conform.BuiltinType, nil)
	builtinOtherNt = TRegisterNodeType(conform.BuiltinOther, nil)
)

func TestValueBuiltinTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		c, vals := nodeBuiltinFixture(controlled)
		if err := conform.RunBuiltins("node", func(call, value string) string {
			v := vals[value]
			return memberRead(func() any {
				switch call {
				case conform.CallLen:
					return fxValue(c.P.ActionLength(v))
				case conform.CallText:
					return fxValue(c.P.ActionText(v))
				case conform.CallConvert:
					return fxValue(c.P.ActionConvert(c.P.g.NodeTypes[conform.BuiltinType], v))
				}
				return c.listNode(c.fxItems(conform.BuiltinFn(call), v))
			}, memberCanon)
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
		tc, tvals := typedBuiltinFixture(controlled)
		if err := conform.RunBuiltins("typed", func(call, value string) string {
			v := tvals[value]
			return memberRead(func() any {
				switch call {
				case conform.CallLen:
					return fxValue(tc.P.ActionLength(v))
				case conform.CallText:
					return fxValue(tc.P.ActionText(v))
				case conform.CallConvert:
					return fxValue(tc.P.ActionConvert(builtinTypeNt, v))
				}
				return &TSeq{Typ: TypeListValue, Kids: tc.fxItems(conform.BuiltinFn(call), v)}
			}, tmemberCanon)
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}

// TestValueLengthUnits runs the length table (conform.RunLengths) on the composition length of the node
// and the typed library under logical compute units: len of a string counts its bytes in either unit.
func TestValueLengthUnits(t *testing.T) {
	unit := func(codePoints bool) sxirinput.PositionUnit {
		if codePoints {
			return sxirinput.CodePoints
		}
		return sxirinput.Bytes
	}
	for _, typed := range []bool{false, true} {
		target := "node"
		if typed {
			target = "typed"
		}
		if err := conform.RunLengths(target, func(s string, codePoints bool) (int, uint64) {
			p := &Parser{unit: unit(codePoints)}
			conformUnits(p, 0)
			var v, e any
			if typed {
				v, e = (&TParser{Parser: p}).ActionLength(s)
			} else {
				v, e = p.ActionLength(s)
			}
			if e != nil {
				t.Fatalf("%s: len(%q) fails: %v", target, s, e)
			}
			return v.(int), uint64(p.UnitsNow())
		}); err != nil {
			t.Error(err)
		}
	}
}
