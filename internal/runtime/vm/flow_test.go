package vm

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestFlowFixedExpectations checks that the expectation IDs of Flow bodies count the fixed
// expectations of the parser's table as the module format says: a module file stores the IDs, and
// the VM passes them to the parser as they are.
func TestFlowFixedExpectations(t *testing.T) {
	if bytecode.FixedExps != int(program.NumFixedDescs) {
		t.Fatalf("bytecode.FixedExps = %d; the parser has %d fixed expectations", bytecode.FixedExps, program.NumFixedDescs)
	}
}

// runFlowBody runs the hand-built Flow body code on p with the registers regs set first, until it
// returns, and returns the registers.
func runFlowBody(t *testing.T, p *state.Parser, regs []any, code ...bytecode.FlowInstr) []any {
	t.Helper()
	fr := flowRun{f: &bytecode.Flow{Regs: len(regs), Code: code}, regs: regs}
	if (&Program{}).runFlow(p, &fr) {
		t.Fatal("the body stopped at a CALL")
	}
	return fr.regs
}

// TestFlowRecoveredErrorCount checks RECLEN and RESTORE on the recovered errors directly: in a framed
// body they run only around a lookahead, whose end drops the errors recovered inside it as well, so
// a parse cannot tell a RESTORE that keeps them from one that drops them.
func TestFlowRecoveredErrorCount(t *testing.T) {
	p := stateBudgetParser(runtime.Limits{})
	errs := []*value.SyntaxError{{Pos: 0}, {Pos: 1}, {Pos: 2}}
	for _, e := range errs {
		p.PushRecovered(e)
	}
	p.Pos = 5
	regs := runFlowBody(t, p, []any{nil, 1, 1, nil, true},
		bytecode.FlowInstr{Op: bytecode.FlowRecLen, Dst: []int{0}},
		bytecode.FlowInstr{Op: bytecode.FlowRestore, Args: []int{1, 2}},
		bytecode.FlowInstr{Op: bytecode.FlowRecLen, Dst: []int{3}},
		bytecode.FlowInstr{Op: bytecode.FlowReturn, Args: []int{3, 4}},
	)
	if regs[0] != 3 {
		t.Errorf("RECLEN before RESTORE = %v, want 3", regs[0])
	}
	if regs[3] != 1 {
		t.Errorf("RECLEN after RESTORE = %v, want 1", regs[3])
	}
	if p.Pos != 1 {
		t.Errorf("position after RESTORE = %d, want 1", p.Pos)
	}
	if got := p.RecoveredErrors(); len(got) != 1 || got[0] != errs[0] {
		t.Errorf("recovered errors after RESTORE = %v, want the first one", got)
	}
}

// TestFlowKeepCopies checks that KEEP returns the expectations in storage of the parser's own: the
// expectations of a failed recovery body sit in the record that the skip goes on to overwrite, so a
// result that shared the argument's storage would change with it. Keeping the same expectations
// again returns the same storage (state.Parser.Keep).
func TestFlowKeepCopies(t *testing.T) {
	p := stateBudgetParser(runtime.Limits{})
	exps := []state.ExpID{7, 9}
	regs := runFlowBody(t, p, []any{exps, nil, nil, nil, true},
		bytecode.FlowInstr{Op: bytecode.FlowKeep, Dst: []int{1}, Args: []int{0}},
		bytecode.FlowInstr{Op: bytecode.FlowKeep, Dst: []int{2}, Args: []int{0}},
		bytecode.FlowInstr{Op: bytecode.FlowReturn, Args: []int{3, 4}},
	)
	kept, again := regs[1].([]state.ExpID), regs[2].([]state.ExpID)
	exps[0], exps[1] = 1, 2
	if len(kept) != 2 || kept[0] != 7 || kept[1] != 9 {
		t.Errorf("KEEP = %v after the argument changed, want [7 9]", kept)
	}
	if len(again) != 2 || &again[0] != &kept[0] {
		t.Errorf("KEEP of the same expectations again did not return the kept storage")
	}
	if regs := runFlowBody(t, p, []any{[]state.ExpID(nil), "unset", nil, true},
		bytecode.FlowInstr{Op: bytecode.FlowKeep, Dst: []int{1}, Args: []int{0}},
		bytecode.FlowInstr{Op: bytecode.FlowReturn, Args: []int{2, 3}},
	); regs[1].([]state.ExpID) != nil {
		t.Errorf("KEEP of no expectations = %v, want nil", regs[1])
	}
}

// stepLoop returns the body of a loop that moves the position by register 1 at each iteration until
// it reaches register 2, and then returns, with a step at the top of each iteration when step is set and a MOVE of
// another register in its place otherwise: register 0 is the loop's progress measure.
func stepLoop(step bool) []bytecode.FlowInstr {
	top := bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{5}, Args: []int{5}}
	if step {
		top = bytecode.FlowInstr{Op: bytecode.FlowStep, Dst: []int{0}, Args: []int{0}}
	}
	return []bytecode.FlowInstr{
		top,
		{Op: bytecode.FlowAdd, Dst: []int{4}, Args: []int{4, 1}},
		{Op: bytecode.FlowSetPos, Args: []int{4}},
		{Op: bytecode.FlowLt, Dst: []int{3}, Args: []int{4, 2}},
		{Op: bytecode.FlowJumpFalse, Args: []int{3}, Target: 6},
		{Op: bytecode.FlowJump, Target: 0},
		{Op: bytecode.FlowReturn, Args: []int{5, 3}},
	}
}

// runStepLoop runs code, a stepLoop, from position 0 with a move of by and an end of end, returning the
// fatal error of the run or nil; it releases the registers and step positions the run takes.
func runStepLoop(p *state.Parser, code []bytecode.FlowInstr, measure any, by, end int) (err error) {
	mc := machineOf(p)
	mark := mc.regs.mark()
	defer mc.regs.release(mark)
	defer func() {
		if x := recover(); x != nil {
			f, ok := x.(state.Fatal)
			if !ok {
				panic(x)
			}
			err = f.Err
		}
	}()
	p.Pos = 0
	regs := mc.regs.push(6)
	copy(regs, []any{measure, by, end, nil, 0, nil})
	fr := flowRun{f: &bytecode.Flow{Regs: len(regs), Code: code}, regs: regs}
	if (&Program{}).runFlow(p, &fr) {
		panic("the body stopped at a CALL")
	}
	return nil
}

// TestFlowStep checks STEP directly: a loop that advances at each iteration runs to its end, one
// that does not ends with the error of a step without progress at its second step, a measure that
// is no position of a step ends with an error too, and the steps store their positions without
// allocating.
func TestFlowStep(t *testing.T) {
	p := stateBudgetParser(runtime.Limits{})
	steps, moves := stepLoop(true), stepLoop(false)
	if err := runStepLoop(p, steps, -1, 1, 1000); err != nil || p.Pos != 1000 {
		t.Fatalf("advancing loop: %v at %d", err, p.Pos)
	}
	err := runStepLoop(p, steps, -1, 0, 1000)
	if err == nil || err.Error() != "invalid bytecode: STEP at 0: the position 0 is not past 0, the position of the loop's last step" {
		t.Errorf("loop without progress: %v", err)
	}
	if err := runStepLoop(p, steps, "text", 1, 1000); err == nil || err.Error() != "invalid bytecode: STEP at 0: register 0 holds no position of a step" {
		t.Errorf("measure of another type: %v", err)
	}
	// The positions past 255 are not among the ints that an interface holds without allocating.
	with := testing.AllocsPerRun(20, func() { runStepLoop(p, steps, -1, 1, 1000) })
	without := testing.AllocsPerRun(20, func() { runStepLoop(p, moves, -1, 1, 1000) })
	if with != without {
		t.Errorf("the loop allocates %v times with its steps and %v times without them", with, without)
	}
}
