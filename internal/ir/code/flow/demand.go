package flow

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// ReadsInput is the law of an operation that reads characters of the input at or after the position. A
// suspended stream (lowerdata.Options.Suspend) must have demanded them with flow.inputDemand before the read.
const ReadsInput = "reads input: the operation reads characters of the input at or after the position, so a suspended stream demands them first (flow.inputDemand)."

// UncoveredReads returns, in evaluation order, the names of the operations with the law ReadsInput that f calls
// without a flow.inputDemand that covers their input on every path to the call. A demand covers the reads after
// it on the same path, up to its amount (see amount). An operation that moves the position (writes prim.Pos), and
// a Set of the position register, end the coverage, because the input a demand covers was measured from the old
// position.
//
// The check is a must analysis over the structure of the function. Each statement leaves a coverage: the amount
// that every path reaching the end of the statement demands. A break, continue or exit to a label passes its
// coverage to the statement that the label names, which meets the coverage of all the jumps to it (a loop's
// break, continue and exit, a labeled block's exit) with its own end. A loop is checked at the greatest fixpoint
// of its head coverage, so a read in a later iteration needs its own demand. A return ends the function, so its
// coverage is vacuous: no read follows it.
func UncoveredReads(f *code.Func) []string {
	c := &demandCheck{report: true, readers: map[string]bool{}, movesPos: map[string]bool{}, jumps: map[string]amount{}}
	for _, o := range Ops() {
		if slicesContains(o.Laws, ReadsInput) {
			c.readers[o.Name] = true
		}
		if slicesContains(o.Writes, prim.Pos) {
			c.movesPos[o.Name] = true
		}
	}
	if f.Body != nil {
		c.block(f.Body, none)
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

// VerifyDemands returns an error naming the reads of f without a covering demand, if f is a function of a
// suspended stream (Attrs.Suspend) and UncoveredReads finds any. It verifies a function after its passes, when
// its forms are finished.
func VerifyDemands(f *code.Func) error {
	if a := AttrsOf(f); a == nil || !a.Suspend {
		return nil
	}
	if reads := UncoveredReads(f); len(reads) > 0 {
		return fmt.Errorf("flow: function %s reads its input with no dominating demand (flow.inputDemand): %s", f.Name, strings.Join(reads, " "))
	}
	return nil
}

// amount is the input past the position that a demand covers, or that a read needs, in two measures: the
// characters and the bytes. A demand of a literal covers its text, and a demand of a character covers one
// complete character. A read is covered on a path when the path's coverage has at least the amount the read
// needs in both measures, so that the check holds in each position unit (prim.Demand).
type amount struct{ runes, bytes int }

var (
	// none is the coverage after a move of the position, and at the start of a function.
	none = amount{}
	// top is the coverage of a path that no read follows: the vacuous coverage of a path that ends in a
	// return, or of a jump to a target that no jump reaches.
	top = amount{math.MaxInt, math.MaxInt}
	// oneChar is one complete character of the input: what a demand of a character covers, and what a read
	// of one character needs. A UTF-8 character takes at most utf8.UTFMax bytes.
	oneChar = amount{1, utf8.UTFMax}
)

// litAmount returns the amount of the text of a literal.
func litAmount(text string) amount { return amount{utf8.RuneCountInString(text), len(text)} }

// min returns the amount that both a and b cover.
func (a amount) min(b amount) amount {
	return amount{min(a.runes, b.runes), min(a.bytes, b.bytes)}
}

// max returns the amount that either a or b covers.
func (a amount) max(b amount) amount {
	return amount{max(a.runes, b.runes), max(a.bytes, b.bytes)}
}

// covers reports whether a covers the amount need.
func (a amount) covers(need amount) bool { return a.runes >= need.runes && a.bytes >= need.bytes }

// demanded returns the amount that the flow.inputDemand call c covers: the text of its literal, or one character.
func demanded(c *code.Call) amount {
	if d, ok := c.Imm.(prim.Demand); ok && !d.Char {
		return litAmount(d.Lit.Text)
	}
	return oneChar
}

// needed returns the amount that the read c needs: the text of its literal, or one character.
func needed(c *code.Call) amount {
	if l, ok := c.Imm.(prim.Lit); ok {
		return litAmount(l.Text)
	}
	return oneChar
}

// The kinds of a jump, keyed with its label: a break, a continue and an exit of a labeled statement, and a
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
	jumps map[string]amount
}

// jumpKey returns the key of the jump of the kind to the label.
func jumpKey(kind, label string) string { return kind + label }

// meet records a jump with the coverage a to the key.
func (c *demandCheck) meet(key string, a amount) {
	c.jumps[key] = c.jumpState(key).min(a)
}

// jumpState returns the meet of the jumps recorded to the key, or top if there are none.
func (c *demandCheck) jumpState(key string) amount {
	if v, ok := c.jumps[key]; ok {
		return v
	}
	return top
}

// take returns the meet of the jumps to the key in jumps and removes them from it, so that the statement the
// key names takes its jumps once.
func take(jumps map[string]amount, key string) amount {
	v, ok := jumps[key]
	if !ok {
		return top
	}
	delete(jumps, key)
	return v
}

// isolate runs f with jumps of its own, and returns them. The jumps that f leaves to targets outside it are then
// merged into the enclosing jumps by absorb; those of a fixpoint iteration are discarded, so that a jump of an
// earlier iteration never weakens the coverage of a later one.
func (c *demandCheck) isolate(f func()) map[string]amount {
	outer := c.jumps
	c.jumps = map[string]amount{}
	f()
	inner := c.jumps
	c.jumps = outer
	return inner
}

// absorb merges the jumps that an isolated statement left to its enclosing statements.
func (c *demandCheck) absorb(inner map[string]amount) {
	for k, v := range inner {
		c.meet(k, v)
	}
}

// calls applies the coverage to the calls of e, in evaluation order: the arguments, then the callee, then the
// call itself, which reads before it moves the position.
func (c *demandCheck) calls(e code.Expr, have *amount) {
	if e == nil {
		return
	}
	switch e := e.(type) {
	case *code.Unary:
		c.calls(e.X, have)
	case *code.Binary:
		c.calls(e.X, have)
		c.calls(e.Y, have)
	case *code.Convert:
		c.calls(e.X, have)
	case *code.Call:
		for _, a := range e.Args {
			c.calls(a, have)
		}
		c.calls(e.Fn, have)
		switch {
		case e.Callee == OpInputDemand:
			*have = have.max(demanded(e))
		case c.readers[e.Callee] && !have.covers(needed(e)) && c.report:
			c.out = append(c.out, e.Callee)
		}
		if c.movesPos[e.Callee] {
			*have = none
		}
	}
}

// set applies the coverage to a Set: its values are evaluated first, and a Set of the position register moves it.
func (c *demandCheck) set(s *code.Set, have *amount) {
	if s == nil {
		return
	}
	for _, v := range s.Values {
		c.calls(v, have)
	}
	for _, d := range s.Dst {
		if d.Name == prim.Pos {
			*have = none
		}
	}
}

// block returns the coverage at the end of b, given have at its start. A labeled block takes the exits to its
// label at its end.
func (c *demandCheck) block(b *code.Block, have amount) amount {
	if b.Label == "" {
		return c.stmts(b, have)
	}
	var end amount
	inner := c.isolate(func() { end = c.stmts(b, have) })
	end = take(inner, jumpKey(jumpExit, b.Label)).min(end)
	c.absorb(inner)
	return end
}

// stmts returns the coverage at the end of the statements of b, given have at its start.
func (c *demandCheck) stmts(b *code.Block, have amount) amount {
	for _, st := range b.Stmts {
		switch st := st.(type) {
		case *code.Set:
			c.set(st, &have)
		case *code.Do:
			c.calls(st.Call, &have)
		case *code.Let:
			c.calls(st.Value, &have)
		case *code.Assert:
			c.calls(st.Cond, &have)
		case *code.Block:
			have = c.block(st, have)
		case *code.If:
			c.set(st.Init, &have)
			c.calls(st.Cond, &have)
			t := c.block(st.Then, have)
			e := have
			if st.Else != nil {
				e = c.block(st.Else, have)
			}
			have = t.min(e)
		case *code.Loop:
			have = c.loop(st, have)
		case *code.Protect:
			// The cleanup runs however the body ends: at its end, and at each return of the body, which it
			// meets. Its other jumps leave the body as they are.
			var end amount
			inner := c.isolate(func() { end = c.block(st.Body, have) })
			fin := take(inner, jumpKey(jumpReturn, "")).min(end)
			c.absorb(inner)
			c.block(st.Finally, fin)
			have = end
		case *code.Return:
			for _, v := range st.Values {
				c.calls(v, &have)
			}
			c.meet(jumpKey(jumpReturn, ""), have)
			have = top
		case *code.Exit:
			c.meet(jumpKey(jumpExit, st.Label), have)
			have = top
		case *code.Break:
			c.meet(jumpKey(jumpBreak, st.Label), have)
			have = top
		case *code.Continue:
			c.meet(jumpKey(jumpContinue, st.Label), have)
			have = top
		}
	}
	return have
}

// loop returns the coverage after the loop l, given have on entry. The head coverage is the greatest fixpoint of
// the entry joined with the coverage that the end of each iteration passes back (after its continues and its
// post statement). The reads are reported by the final pass, at that fixpoint.
func (c *demandCheck) loop(l *code.Loop, have amount) amount {
	c.set(l.Init, &have)
	entry := have
	saved := c.report
	c.report = false
	head := entry
	for {
		var next amount
		c.isolate(func() { _, next = c.iteration(l, head, entry) })
		if next == head {
			break
		}
		head = next
	}
	c.report = saved
	var atCond amount
	inner := c.isolate(func() { atCond, _ = c.iteration(l, head, entry) })
	// The loop leaves by its break or its exit, and by the condition failing at the head.
	exit := take(inner, jumpKey(jumpBreak, l.Label)).min(take(inner, jumpKey(jumpExit, l.Label)))
	if l.Cond != nil {
		exit = exit.min(atCond)
	}
	c.absorb(inner)
	return exit
}

// iteration checks one iteration of l from the head coverage head. It returns the coverage after the condition,
// which the body starts from, and the head coverage of the next iteration: the entry coverage joined with the
// end of this one. The reads are reported as the checker reports them.
func (c *demandCheck) iteration(l *code.Loop, head, entry amount) (afterCond, next amount) {
	afterCond = head
	c.calls(l.Cond, &afterCond)
	end := c.block(l.Body, afterCond)
	end = take(c.jumps, jumpKey(jumpContinue, l.Label)).min(end)
	c.set(l.Post, &end)
	return afterCond, entry.min(end)
}
