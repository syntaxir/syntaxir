package libgo

import (
	"strings"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The value primitives of action expressions (the component values of exec.Parser) that the node library of
// generated parsers and the engine share. The compositions of the operators in action_exec.go decide
// which of them applies; each acts on one kind of value.

// valKind is the operation val.kind: the kind of x. A node, a list, a tuple, a struct and a box are
// valKindOther; a box is opened by the operation val.unbox.
func valKind(x any) int {
	switch x.(type) {
	case nil:
		return valKindNil
	case int:
		return valKindInt
	case string:
		return valKindString
	case bool:
		return valKindBool
	}
	return valKindOther
}

// intCmp is the operation int.cmp: -1, 0 or 1 as the int l is less than, equal to or greater than the int r.
func intCmp(l, r any) int {
	a, b := l.(int), r.(int)
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// intIndex is the operation int.index: the int x and true if it is an index of n elements.
func intIndex(x any, n int) (int, bool) {
	k := x.(int)
	if k < 0 || k >= n {
		return 0, false
	}
	return k, true
}

// valItems is the operation seq.items in the node library.
func valItems(x any) ([]*sxirvalue.Node, bool) {
	switch x := x.(type) {
	case nil:
		return nil, true
	case *sxirvalue.Node:
		if x == nil {
			return nil, true
		}
		return D(x).Children, true
	}
	return nil, false
}

// nodeOf is the operation node.of in the node library: x as a node, list, tuple or struct.
func nodeOf(x any) (*sxirvalue.Node, bool) {
	n, ok := x.(*sxirvalue.Node)
	return n, ok && n != nil
}

// seqKind is the kind of a tuple value or a list value.
func seqKind(tuple bool) *valueimpl.Kind {
	if tuple {
		return valueimpl.TupleValueKind
	}
	return valueimpl.ListValueKind
}

// elemSpan is the operation seq.span: an element of a list or a tuple is a node, boxed if it was a scalar,
// and nil adds no range.
func elemSpan(it *sxirvalue.Node) (int, int, bool) {
	if it == nil {
		return 0, 0, false
	}
	return int(D(it).Start), int(D(it).End), true
}

// setSpan is the operation node.setSpan.
func setSpan(n *sxirvalue.Node, start, end int) { D(n).Start, D(n).End = int32(start), int32(end) }

// strAdd is the operation str.add. Under a budget it charges the bytes of the result and copies them
// in chunks, charging each.
func (p *Parser) strAdd(l, r any) any {
	ls, rs := l.(string), r.(string)
	p.valueUnits(len(ls) + len(rs))
	if !p.countsMemory() {
		return ls + rs
	}
	size := checkedTextSize(len(ls), len(rs))
	p.chargeValueBytes(size)
	var out strings.Builder
	out.Grow(size)
	for _, s := range [...]string{ls, rs} {
		for off := 0; off < len(s); {
			n := min(spendValueChunk, len(s)-off)
			p.spendValue(n)
			out.WriteString(s[off : off+n])
			off += n
		}
	}
	return out.String()
}

// strCmp is the operation str.cmp.
func (p *Parser) strCmp(l, r any) int { return p.compareText(l.(string), r.(string)) }

// strEq is the operation str.eq.
func (p *Parser) strEq(l, r any) bool { return p.TextEqual(l.(string), r.(string)) }
