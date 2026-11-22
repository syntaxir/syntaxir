package libgo

import sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"

// The value primitives of action expressions that only the library has: those of the typed library, and act.made,
// whose context differs from the engine's (action_value.go holds the shared ones).

// tvalItems is the operation seq.items in the typed library.
func tvalItems(x any) ([]any, bool) {
	if x == nil {
		return nil, true
	}
	if n, ok := x.(TVal); ok {
		return n.TKids(), true
	}
	return nil, false
}

// elemSpan is the operation seq.span: a node adds its range, and an int, a string or a bool adds the range
// of the rule, which the node libraries give the box they hold it in.
func (c *TActCtx) elemSpan(it any) (int, int, bool) {
	switch t := it.(type) {
	case TVal:
		s, e := t.TSpan()
		return s, e, true
	case int, string, bool:
		return c.Start, c.End, true
	}
	return 0, 0, false
}

// made is the operation act.made.
func (c *ActCtx) made(n *sxirvalue.Node) { c.P.created = append(c.P.created, n) }

// made is the operation act.made in the typed library.
func (c *TActCtx) made(n TVal) { c.P.Created = append(c.P.Created, n) }

// tnodeOf is the operation node.of in the typed library.
func tnodeOf(x any) (TVal, bool) {
	n := AsTval(x)
	return n, n != nil
}

// tnodeStart and tnodeEnd are the operations node.start and node.end in the typed library.
func tnodeStart(n TVal) any {
	s, _ := n.TSpan()
	return s
}

func tnodeEnd(n TVal) any {
	_, e := n.TSpan()
	return e
}

// tnodeHasText is the operation node.hasText in the typed library.
func tnodeHasText(n TVal) bool {
	_, ok := n.TText()
	return ok
}

// tnodeWidth is the operation node.width in the typed library.
func tnodeWidth(n TVal) any {
	start, end := n.TSpan()
	return end - start
}
