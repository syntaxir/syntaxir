package conform

// CtxBrokenOps returns the broken versions of the operations of the dependency memo that its cases
// must tell from the correct ones, as OprBrokenOps does for the Operators Rule family.
func CtxBrokenOps() []OprBroken {
	var bs []OprBroken
	add := func(name, op string, edit func(call func(string, ...any) []any, a []any) []any) {
		bs = append(bs, OprBroken{Name: name, Op: op, Edit: edit})
	}
	ignore := func(op string) {
		add(op+" is ignored", op, func(call func(string, ...any) []any, a []any) []any { return nil })
	}
	add("ctx.allows ignores the present set", "ctx.allows", func(call func(string, ...any) []any, a []any) []any {
		return []any{true}
	})
	add("ctx.allows is negated", "ctx.allows", func(call func(string, ...any) []any, a []any) []any {
		return []any{!call("ctx.allows", a...)[0].(bool)}
	})
	add("ctx.active is negated", "ctx.active", func(call func(string, ...any) []any, a []any) []any {
		return []any{!call("ctx.active", a...)[0].(bool)}
	})
	add("ctx.ok is negated", "ctx.ok", func(call func(string, ...any) []any, a []any) []any {
		return []any{!call("ctx.ok", a...)[0].(bool)}
	})
	add("ctx.found is always false", "ctx.found", func(call func(string, ...any) []any, a []any) []any {
		return []any{false}
	})
	add("ctx.pop always stores in the memo", "ctx.pop", func(call func(string, ...any) []any, a []any) []any {
		call("ctx.pop", a...)
		return []any{call("ctx.storeMemo")[0]}
	})
	add("ctx.storeCtx is ctx.storeMemo", "ctx.storeCtx", func(call func(string, ...any) []any, a []any) []any {
		return call("ctx.storeMemo")
	})
	add("ctx.end is one further", "ctx.end", func(call func(string, ...any) []any, a []any) []any {
		return []any{call("ctx.end", a...)[0].(int64) + 1}
	})
	add("ctx.far is the start", "ctx.far", func(call func(string, ...any) []any, a []any) []any {
		return []any{int64(0)}
	})
	add("ctx.expected is empty", "ctx.expected", func(call func(string, ...any) []any, a []any) []any {
		return call("exp.empty")
	})
	add("ctx.errs is none", "ctx.errs", func(call func(string, ...any) []any, a []any) []any {
		return []any{nil}
	})
	add("grow.ctx is negated", "grow.ctx", func(call func(string, ...any) []any, a []any) []any {
		return []any{!call("grow.ctx", a...)[0].(bool)}
	})
	add("grow.longer ignores the seed", "grow.longer", func(call func(string, ...any) []any, a []any) []any {
		return []any{a[1].(bool)}
	})
	add("grow.adopt drops the errors", "grow.adopt", func(call func(string, ...any) []any, a []any) []any {
		return call("grow.adopt", a[0], a[1], nil)
	})
	add("grow.next always evaluates the body", "grow.next", func(call func(string, ...any) []any, a []any) []any {
		call("grow.next", a...)
		return []any{true}
	})
	add("grow.next never evaluates the body", "grow.next", func(call func(string, ...any) []any, a []any) []any {
		call("grow.next", a...)
		return []any{false}
	})
	add("grow.step takes every step as matched", "grow.step", func(call func(string, ...any) []any, a []any) []any {
		return call("grow.step", a[0], a[1], true)
	})
	add("grow.step takes every step as failed", "grow.step", func(call func(string, ...any) []any, a []any) []any {
		return call("grow.step", a[0], a[1], false)
	})
	add("grow.step drops the value", "grow.step", func(call func(string, ...any) []any, a []any) []any {
		return call("grow.step", a[0], nil, a[2])
	})
	// grow.end returns the growth's last seed: one that answers with the result of an earlier growth ends at
	// another position and has another value.
	var lastEnd any
	add("grow.end answers with the result of an earlier growth", "grow.end", func(call func(string, ...any) []any, a []any) []any {
		out := call("grow.end", a...)
		if lastEnd != nil {
			out[0] = lastEnd
		}
		lastEnd = out[0]
		return out
	})
	// Identity is compared for the results of a lookup, not for the result of grow.end: a lookup that
	// answers with the entry of an earlier lookup is told from the correct one.
	var last any
	add("ctx.lookup answers with the entry of an earlier lookup", "ctx.lookup", func(call func(string, ...any) []any, a []any) []any {
		out := call("ctx.lookup", a...)
		if call("ctx.found", out[0])[0].(bool) {
			if last != nil {
				out[0] = last
			}
			last = out[0]
		}
		return out
	})
	ignore("grow.step")
	ignore("grow.adopt")
	ignore("ctx.keep")
	ignore("ctx.done")
	ignore("memo.setPresent")
	return bs
}
