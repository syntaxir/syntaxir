package bytecode_test

import (
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestFlowUnboundedCycle checks that Flow.UnboundedCycle accepts the cycles that charge a step or
// that reuse the elements of a run they leave when it has none left, and finds the others: a jump
// back that charges nothing (the shape of a corrupted module that made a parse run forever), a
// jump to itself, a run whose JUMPFALSE stays in the cycle or tests another register, a run that
// the cycle starts again, and a cycle beside a bounded one.
func TestFlowUnboundedCycle(t *testing.T) {
	const (
		step   = bytecode.FlowStep
		jump   = bytecode.FlowJump
		jfalse = bytecode.FlowJumpFalse
		ret    = bytecode.FlowReturn
	)
	c := func(dst int, v any) bytecode.FlowInstr {
		return bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{dst}, Imm: v}
	}
	j := func(op bytecode.FlowOp, arg, to int) bytecode.FlowInstr {
		in := bytecode.FlowInstr{Op: op, Target: to}
		if op != jump {
			in.Args = []int{arg}
		}
		return in
	}
	start := bytecode.FlowInstr{Op: bytecode.FlowResumeStart, Dst: []int{1}, Imm: bytecode.FlowResumeOf{Site: 1}}
	next := func(ok int) bytecode.FlowInstr {
		return bytecode.FlowInstr{Op: bytecode.FlowResumeNext, Dst: []int{2, ok}, Args: []int{1}}
	}
	end := bytecode.FlowInstr{Op: ret, Args: []int{0, 0}}
	for _, tc := range []struct {
		name string
		code []bytecode.FlowInstr
		want int // the instruction reported, or -1 when the body has no unbounded cycle
	}{
		{"straight", []bytecode.FlowInstr{c(0, true), end}, -1},
		{"step loop", []bytecode.FlowInstr{c(0, true), {Op: step}, j(bytecode.FlowJumpTrue, 0, 1), end}, -1},
		{"step loop by jump", []bytecode.FlowInstr{c(0, true), {Op: step}, j(jfalse, 0, 4), j(jump, 0, 1), end}, -1},
		{"run leaves on its end", []bytecode.FlowInstr{start, next(3), j(jfalse, 3, 4), j(jump, 0, 1), c(0, nil), end}, -1},
		{"run in a step loop", []bytecode.FlowInstr{{Op: step}, start, next(3), j(jfalse, 3, 5), j(jump, 0, 2), j(jump, 0, 0)}, -1},
		{"jump back without a step", []bytecode.FlowInstr{c(0, nil), c(0, false), j(jfalse, 0, 0), end}, 0},
		{"jump to itself", []bytecode.FlowInstr{c(0, false), j(jfalse, 0, 1), end}, 1},
		{"run end stays in the cycle", []bytecode.FlowInstr{start, next(3), j(jfalse, 3, 1), j(jump, 0, 1), end}, 1},
		{"run end tests another register", []bytecode.FlowInstr{start, c(0, false), next(3), j(jfalse, 0, 5), j(jump, 0, 2), c(0, nil), end}, 2},
		{"run started in the cycle", []bytecode.FlowInstr{start, next(3), j(jfalse, 3, 4), j(jump, 0, 0), c(0, nil), end}, 0},
		{"cycle beside a step loop", []bytecode.FlowInstr{c(0, true), {Op: step}, j(jfalse, 0, 1), j(jfalse, 0, 3), end}, 3},
	} {
		f := &bytecode.Flow{Regs: 4, Code: tc.code}
		pc, ok := f.UnboundedCycle()
		if got := map[bool]int{false: -1, true: pc}[ok]; got != tc.want {
			t.Errorf("%s: UnboundedCycle() = %d, %v; want %d", tc.name, pc, ok, tc.want)
		}
	}
}
