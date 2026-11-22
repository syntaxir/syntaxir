package libgo

import sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"

// The tables call the compositions that generated parsers print from Exec IR (the Action functions of
// action_exec.go) through these helpers, which compose them as the lowered action expressions do: a list or a tuple
// pushes its elements on KidStack and ends with ActionHull, a struct sets its fields and covers their
// ranges, and an iteration reads the items and steps through them with ActionItemAt. A failure that a
// composition returns is raised as a panic, which memberRead reports.

// fxRaise raises the failure f of a composition.
func fxRaise(f any) {
	if f != nil {
		panic(f)
	}
}

// fxValue returns v, raising the failure f of the composition that made it.
func fxValue[T any](v T, f any) T {
	fxRaise(f)
	return v
}

func (c *ActCtx) fxPush(fn string, v any) { fxRaise(c.P.ActionPush(c, fn, v)) }

func (c *ActCtx) fxList(args ...any) any {
	base := len(c.P.KidStack)
	for _, a := range args {
		c.fxPush("list", a)
	}
	return c.listNode(c.P.Kids(base))
}

func (c *ActCtx) fxTuple(args ...any) any {
	base := len(c.P.KidStack)
	for _, a := range args {
		c.fxPush("tuple", a)
	}
	return c.P.ActionHull(c, true, c.P.Kids(base))
}

func (c *ActCtx) fxStruct(typ string, kv ...any) any {
	n := c.ActionStruct(typ, len(kv)/2)
	first := true
	for i := 0; i < len(kv); i += 2 {
		v := kv[i+1]
		c.ActionSetField(n, kv[i].(string), v)
		first = c.P.ActionCover(n, v, first)
	}
	c.P.ActionMade(c, n, first)
	return n
}

func (c *ActCtx) fxMember(x any, name string) any {
	kind, field := memberKind(name)
	return fxValue(c.P.ActionMember(c, kind, x, name, field))
}

func (c *ActCtx) fxItems(fn string, v any) []*sxirvalue.Node { return fxValue(c.P.ActionItems(fn, v)) }

func (c *ActCtx) fxPushItems(v any) { fxRaise(c.P.ActionPushItems(c, "concat", v)) }

func (c *ActCtx) fxFold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := c.fxItems(fn, list)
	for i := range items {
		acc = f(acc, c.P.ActionItemAt(items, i, right))
	}
	return acc
}

func (c *ActCtx) fxMap(list any, f func(item any) any) any {
	base := len(c.P.KidStack)
	items := c.fxItems("map", list)
	for i := range items {
		c.fxPush("map", f(c.P.ActionItemAt(items, i, false)))
	}
	return c.listNode(c.P.Kids(base))
}

func (c *ActCtx) fxFilter(list any, f func(item any) any) any {
	base := len(c.P.KidStack)
	items := c.fxItems("filter", list)
	for i := range items {
		if fxValue(c.P.ActionFilterKeep(f(c.P.ActionItemAt(items, i, false)))) {
			c.P.KidStack = append(c.P.KidStack, items[i])
		}
	}
	return c.listNode(c.P.Kids(base))
}

func (c *TActCtx) fxPush(fn string, v any) { fxRaise(c.P.ActionPush(c, fn, v)) }

func (c *TActCtx) fxTuple(args ...any) any {
	base := len(c.P.KidStack)
	for _, a := range args {
		c.fxPush("tuple", a)
	}
	return c.P.ActionHull(c, true, c.P.Kids(base))
}

func (c *TActCtx) fxMember(x any, name string) any {
	kind, field := memberKind(name)
	return fxValue(c.P.ActionMember(c, kind, x, name, field))
}

func (c *TActCtx) fxItems(fn string, v any) []any { return fxValue(c.P.ActionItems(fn, v)) }

func (c *TActCtx) fxPushItems(v any) { fxRaise(c.P.ActionPushItems(c, "concat", v)) }

func (c *TActCtx) fxFold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := c.fxItems(fn, list)
	for i := range items {
		acc = f(acc, c.P.ActionItemAt(items, i, right))
	}
	return acc
}

func (c *TActCtx) fxMap(list any, f func(item any) any) any {
	base := len(c.P.KidStack)
	items := c.fxItems("map", list)
	for i := range items {
		c.fxPush("map", f(c.P.ActionItemAt(items, i, false)))
	}
	return c.listNode(c.P.Kids(base))
}

func (c *TActCtx) fxFilter(list any, f func(item any) any) any {
	base := len(c.P.KidStack)
	items := c.fxItems("filter", list)
	for i := range items {
		if fxValue(c.P.ActionFilterKeep(f(c.P.ActionItemAt(items, i, false)))) {
			c.P.KidStack = append(c.P.KidStack, items[i])
		}
	}
	return c.listNode(c.P.Kids(base))
}
