package flow

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// ReadsInput is the law of an operation that reads characters of the input at or after the position. A
// suspended stream (lowerdata.Options.Suspend) must have demanded them with flow.inputDemand before the read.
const ReadsInput = "reads input: the operation reads characters of the input at or after the position, so a suspended stream demands them first (flow.inputDemand)."

// UncoveredReads returns, in evaluation order, the names of the operations with the law ReadsInput that f calls
// without a flow.inputDemand on every path to the call. A demand covers the reads after it on the same path. An
// operation that moves the position (writes prim.Pos), and a Set of the position register, end the coverage,
// because the units that a demand covers were measured from the old position.
//
// The check is a must analysis over the structure of the function. Each statement leaves a coverage: true when
// every path reaching the end of the statement is covered. A break, continue or exit to a label passes its
// coverage to the statement that the label names, which meets the coverage of all the jumps to it (a loop's
// break and continue, a labeled block's exit) with its own end. A loop is checked at the greatest fixpoint of
// its head coverage, so a read in a later iteration needs its own demand. A return ends the function, so its
// coverage is vacuous: no read follows it.
//
// The check covers the presence and position of demands only. That the demanded amount covers the read is the
// lowering's obligation, which the operation's form (a literal, or one character) states.
func UncoveredReads(f *code.Func) []string {
	c := &demandCheck{report: true, readers: map[string]bool{}, movesPos: map[string]bool{}, jumps: map[string]bool{}}
	for _, o := range Ops() {
		if slicesContains(o.Laws, ReadsInput) {
			c.readers[o.Name] = true
		}
		if slicesContains(o.Writes, prim.Pos) {
			c.movesPos[o.Name] = true
		}
	}
	if f.Body != nil {
		c.block(f.Body, false)
	}
	return c.out
}

func slicesContains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// The kinds of a jump, keyed with its label: a break and a continue of a loop, an exit of a labeled block, and a
// return of the function.
const (
	jumpBreak    = "break:"
	jumpContinue = "continue:"
	jumpExit     = "exit:"
	jumpReturn   = "return"
)

type demandCheck struct {
	report   bool
	readers  map[string]bool
	movesPos map[string]bool
	out      []string
	// jumps holds the meet of the coverage of the jumps met so far, by their key (jumpKey). An absent key is
	// vacuously covered: no jump reaches it.
	jumps map[string]bool
}

// jumpKey returns the key of the jump of the kind to the label.
func jumpKey(kind, label string) string { return kind + label }

// meet records a jump with the coverage covered to the key.
func (c *demandCheck) meet(key string, covered bool) {
	c.jumps[key] = c.jumpState(key) && covered
}

// jumpState returns the meet of the jumps recorded to the key, or true if there are none.
func (c *demandCheck) jumpState(key string) bool {
	v, ok := c.jumps[key]
	return !ok || v
}

// take returns the meet of the jumps to the key in jumps and removes them from it, so that the statement the
// key names takes its jumps once.
func take(jumps map[string]bool, key string) bool {
	v, ok := jumps[key]
	if !ok {
		return true
	}
	delete(jumps, key)
	return v
}

// isolate runs f with jumps of its own, and returns them. The jumps that f leaves to targets outside it are then
// merged into the enclosing jumps by absorb; those of a fixpoint iteration are discarded, so that a jump of an
// earlier iteration never weakens the coverage of a later one.
func (c *demandCheck) isolate(f func()) map[string]bool {
	outer := c.jumps
	c.jumps = map[string]bool{}
	f()
	inner := c.jumps
	c.jumps = outer
	return inner
}

// absorb merges the jumps that an isolated statement left to its enclosing statements.
func (c *demandCheck) absorb(inner map[string]bool) {
	for k, v := range inner {
		c.meet(k, v)
	}
}

// calls applies the coverage to the calls of e, in evaluation order: the arguments, then the callee, then the
// call itself, which reads before it moves the position.
func (c *demandCheck) calls(e code.Expr, covered *bool) {
	if e == nil {
		return
	}
	switch e := e.(type) {
	case *code.Unary:
		c.calls(e.X, covered)
	case *code.Binary:
		c.calls(e.X, covered)
		c.calls(e.Y, covered)
	case *code.Convert:
		c.calls(e.X, covered)
	case *code.Call:
		for _, a := range e.Args {
			c.calls(a, covered)
		}
		c.calls(e.Fn, covered)
		switch {
		case e.Callee == OpInputDemand:
			*covered = true
		case c.readers[e.Callee] && !*covered && c.report:
			c.out = append(c.out, e.Callee)
		}
		if c.movesPos[e.Callee] {
			*covered = false
		}
	}
}

// set applies the coverage to a Set: its values are evaluated first, and a Set of the position register moves it.
func (c *demandCheck) set(s *code.Set, covered *bool) {
	if s == nil {
		return
	}
	for _, v := range s.Values {
		c.calls(v, covered)
	}
	for _, d := range s.Dst {
		if d.Name == prim.Pos {
			*covered = false
		}
	}
}

// block returns the coverage at the end of b. A labeled block takes the exits to its label at its end.
func (c *demandCheck) block(b *code.Block, covered bool) bool {
	if b.Label == "" {
		return c.stmts(b, covered)
	}
	var end bool
	inner := c.isolate(func() { end = c.stmts(b, covered) })
	end = take(inner, jumpKey(jumpExit, b.Label)) && end
	c.absorb(inner)
	return end
}

// stmts returns the coverage at the end of the statements of b, given covered at its start.
func (c *demandCheck) stmts(b *code.Block, covered bool) bool {
	for _, st := range b.Stmts {
		switch st := st.(type) {
		case *code.Set:
			c.set(st, &covered)
		case *code.Do:
			c.calls(st.Call, &covered)
		case *code.Let:
			c.calls(st.Value, &covered)
		case *code.Assert:
			c.calls(st.Cond, &covered)
		case *code.Block:
			covered = c.block(st, covered)
		case *code.If:
			c.set(st.Init, &covered)
			c.calls(st.Cond, &covered)
			t := c.block(st.Then, covered)
			e := covered
			if st.Else != nil {
				e = c.block(st.Else, covered)
			}
			covered = t && e
		case *code.Loop:
			covered = c.loop(st, covered)
		case *code.Protect:
			// The cleanup runs however the body ends: at its end, and at each return of the body, which it
			// meets. Its other jumps leave the body as they are.
			var end bool
			inner := c.isolate(func() { end = c.block(st.Body, covered) })
			fin := take(inner, jumpKey(jumpReturn, "")) && end
			c.absorb(inner)
			c.block(st.Finally, fin)
			covered = end
		case *code.Return:
			for _, v := range st.Values {
				c.calls(v, &covered)
			}
			c.meet(jumpKey(jumpReturn, ""), covered)
			covered = true
		case *code.Exit:
			c.meet(jumpKey(jumpExit, st.Label), covered)
			covered = true
		case *code.Break:
			c.meet(jumpKey(jumpBreak, st.Label), covered)
			covered = true
		case *code.Continue:
			c.meet(jumpKey(jumpContinue, st.Label), covered)
			covered = true
		}
	}
	return covered
}

// loop returns the coverage after the loop l, given covered on entry. The head coverage is the greatest fixpoint
// of the entry joined with the coverage that the end of each iteration passes back (after its continues and
// its post statement). The reads are reported by the final pass, at that fixpoint.
func (c *demandCheck) loop(l *code.Loop, covered bool) bool {
	c.set(l.Init, &covered)
	entry := covered
	saved := c.report
	c.report = false
	head := entry
	for {
		var next bool
		c.isolate(func() { _, next = c.iteration(l, head, entry) })
		if next == head {
			break
		}
		head = next
	}
	c.report = saved
	var atCond bool
	inner := c.isolate(func() { atCond, _ = c.iteration(l, head, entry) })
	// The loop leaves by its break, and by the condition failing at the head.
	exit := take(inner, jumpKey(jumpBreak, l.Label)) && (l.Cond == nil || atCond)
	c.absorb(inner)
	return exit
}

// iteration checks one iteration of l from the head coverage head. It returns the coverage after the condition,
// which the body starts from, and the head coverage of the next iteration: the entry coverage joined with the
// end of this one. The reads are reported as the checker reports them.
func (c *demandCheck) iteration(l *code.Loop, head, entry bool) (afterCond, next bool) {
	afterCond = head
	c.calls(l.Cond, &afterCond)
	end := c.block(l.Body, afterCond)
	end = take(c.jumps, jumpKey(jumpContinue, l.Label)) && end
	c.set(l.Post, &end)
	return afterCond, entry && end
}
