package flow

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// ReadsInput is the law of an operation that reads characters of the input at or after the position. A
// suspended stream (lowerdata.Options.Suspend) must have demanded them with flow.inputDemand before the read.
const ReadsInput = "reads input: the operation reads characters of the input at or after the position, so a suspended stream demands them first (flow.inputDemand)."

// UncoveredReads returns, in call order, the names of the operations with the law ReadsInput that f calls
// without a flow.inputDemand dominating the call. A demand covers the reads after it in its block, in the
// blocks nested in it and in the loop bodies it dominates. An operation that writes the position (prim.Pos)
// ends the coverage, because the units that a demand covers were measured from the old position. At a join
// the coverage holds only where it holds on every path, and a loop body is checked at the fixpoint of its
// entry state, so that a read in a later iteration needs its own demand.
//
// The check covers the presence and position of demands only. That the demanded amount covers the read is the
// lowering's obligation, which the operation's form (a literal, or one character) states.
func UncoveredReads(f *code.Func) []string {
	c := &demandCheck{report: true, readers: map[string]bool{}, movesPos: map[string]bool{}}
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

type demandCheck struct {
	report   bool
	readers  map[string]bool
	movesPos map[string]bool
	out      []string
}

// calls applies the coverage to the calls of e in preorder.
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
		switch {
		case e.Callee == OpInputDemand:
			*covered = true
		case c.readers[e.Callee] && !*covered && c.report:
			c.out = append(c.out, e.Callee)
		}
		if c.movesPos[e.Callee] {
			*covered = false
		}
		c.calls(e.Fn, covered)
		for _, a := range e.Args {
			c.calls(a, covered)
		}
	}
}

func (c *demandCheck) set(s *code.Set, covered *bool) {
	if s == nil {
		return
	}
	for _, v := range s.Values {
		c.calls(v, covered)
	}
}

// block returns the coverage at the end of b. A statement that leaves the block returns true: it does not
// reach the statements after it, so it does not weaken a join.
func (c *demandCheck) block(b *code.Block, covered bool) bool {
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
			covered = c.block(st.Body, covered)
			c.block(st.Finally, covered)
		case *code.Return:
			for _, v := range st.Values {
				c.calls(v, &covered)
			}
			covered = true
		case *code.Exit, *code.Break, *code.Continue:
			covered = true
		}
	}
	return covered
}

// loop checks the body at the fixpoint of its entry state: the entry coverage, joined with the coverage at the
// end of each iteration. The reads are reported in the final pass only.
func (c *demandCheck) loop(l *code.Loop, covered bool) bool {
	c.set(l.Init, &covered)
	entry := covered
	saved := c.report
	c.report = false
	head := entry
	for {
		h := head
		c.calls(l.Cond, &h)
		end := c.block(l.Body, h)
		c.set(l.Post, &end)
		next := entry && end
		if next == head {
			break
		}
		head = next
	}
	c.report = saved
	h := head
	c.calls(l.Cond, &h)
	c.block(l.Body, h)
	return head
}
