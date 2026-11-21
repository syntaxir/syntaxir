package nodes

import (
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The functions in this file build and change nodes for the tests of packages outside the engine,
// which cannot reach the fields of a Node. They are functions rather than methods so that they do
// not become part of the public Node API.

// NewNode returns a node of kind k with the range [start, end). text is the string of a
// KindString node, elems the elements of a tuple or list, typ the type name of a KindStruct node,
// fields its fields (or captures), and value the value of a KindValue node.
func NewNode(k sxirvalue.NodeKind, start, end int, text string, elems []*sxirvalue.Node, typ string, fields valueimpl.Fields, value any) *sxirvalue.Node {
	n := Ptr(Data{Start: int32(start), End: int32(end), Text: text, Children: elems, Fields: fields, Box: value})
	d := D(n)
	switch k {
	case sxirvalue.KindString:
		d.Kind, d.Terminal = valueimpl.MatchKind, true
	case sxirvalue.KindTuple:
		d.Kind = valueimpl.SeqKind
	case sxirvalue.KindList:
		d.Kind = valueimpl.ListKind
	case sxirvalue.KindError:
		d.Kind = valueimpl.ErrorKind
	case sxirvalue.KindStruct:
		d.Kind = valueimpl.KindOf(typ, "")
	case sxirvalue.KindTupleValue:
		d.Kind = valueimpl.TupleValueKind
	case sxirvalue.KindListValue:
		d.Kind = valueimpl.ListValueKind
	case sxirvalue.KindValue:
		d.Kind = valueimpl.BoxKind
	}
	return n
}

// SetNodeElems replaces the elements of the tuple or list n, which can make a cycle.
func SetNodeElems(n *sxirvalue.Node, elems []*sxirvalue.Node) { D(n).Children = elems }

// SetNodeText replaces the string of the KindString node n.
func SetNodeText(n *sxirvalue.Node, text string) {
	D(n).Text = text
}

// SetNodeStart replaces the start of the range of n.
func SetNodeStart(n *sxirvalue.Node, start int) { D(n).Start = int32(start) }

// SetNodeField sets the field name of n to v, adding it if absent.
func SetNodeField(n *sxirvalue.Node, name string, v any) {
	valueimpl.SetField(&D(n).Fields, name, v)
}
