package bytecode_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestFlowUnassigned checks that Flow.Unassigned finds the registers that some path reads before
// writing: a register written on one branch only, and one written only later in a loop, but not one
// written on every path, one read only by unreachable code, or a call's results read after it.
func TestFlowUnassigned(t *testing.T) {
	c := func(dst int, v any) bytecode.FlowInstr {
		return bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{dst}, Imm: v}
	}
	f := &bytecode.Flow{Regs: 8, Code: []bytecode.FlowInstr{
		0:  c(0, true),
		1:  {Op: bytecode.FlowJumpFalse, Args: []int{0}, Target: 3}, // skips the write of r1
		2:  c(1, 1),
		3:  c(2, 2), // r2 on every path
		4:  {Op: bytecode.FlowJumpFalse, Args: []int{0}, Target: 6},
		5:  c(2, 3),
		6:  {Op: bytecode.FlowMove, Dst: []int{3}, Args: []int{1}}, // reads r1: unwritten on the jump from 1
		7:  {Op: bytecode.FlowCall, Dst: []int{4, 5}, Imm: bytecode.FlowCallOf{}},
		8:  {Op: bytecode.FlowJumpFalse, Args: []int{5}, Target: 11},
		9:  {Op: bytecode.FlowMove, Dst: []int{3}, Args: []int{6}}, // reads r6: written only after, in the loop
		10: c(6, 0),
		11: {Op: bytecode.FlowJumpFalse, Args: []int{0}, Target: 8},
		12: {Op: bytecode.FlowReturn, Args: []int{4, 2}},
		13: {Op: bytecode.FlowReturn, Args: []int{7, 7}}, // unreachable
	}}
	if got, want := f.Unassigned(), []int{1, 6}; !slices.Equal(got, want) {
		t.Errorf("Unassigned() = %v, want %v", got, want)
	}
}
