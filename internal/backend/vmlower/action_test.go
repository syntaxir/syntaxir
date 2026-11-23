package vmlower

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/runtime/state"
)

// TestActionDeclareKeepsTheFile checks that a local keeps the register file of its first
// declaration: declaring it again in another file is an error instead of a second register that
// its readers would not see.
func TestActionDeclareKeepsTheFile(t *testing.T) {
	c := &actionCompiler{locals: map[string]actionReg{}, prog: &state.ActionProgram{}}
	n, err := c.declare("x", code.Type{Kind: code.KInt})
	if err != nil {
		t.Fatal(err)
	}
	if again, err := c.declare("x", code.Type{Kind: code.KBool}); err != nil || again != n {
		t.Errorf("declaring x again in its file gives %v, %v; want %v", again, err, n)
	}
	if r, err := c.declare("x", code.Type{Kind: code.KHandle, Handle: exec.Datum}); err == nil {
		t.Errorf("declaring the int x as a value gives the register %v, want an error", r)
	}
	if r, err := c.declare("y", code.Type{Kind: code.KHandle, Handle: "Nope"}); err == nil {
		t.Errorf("a local of a type without a register file gets %v, want an error", r)
	}
}

// TestActionEmitFuses checks the instructions the action expression compiler fuses as it emits them: the field of a
// struct and the cover of its value become FIELD, which counts no unit (a step before them stays), and a move just before a return
// becomes the return of its source unless a jump goes to the return, whose other paths need the
// move.
func TestActionEmitFuses(t *testing.T) {
	emit := func(code ...state.ActionInstr) []state.ActionInstr {
		c := &actionCompiler{prog: &state.ActionProgram{}}
		for _, in := range code {
			c.emit(in)
		}
		return c.prog.Code
	}
	step := state.ActionInstr{Op: state.TStep}
	set := state.ActionInstr{Op: state.TSetField, A: 1, C: 2}
	for _, c := range []struct {
		name string
		code []state.ActionInstr
		want []state.ActionInstr
	}{
		{"field", []state.ActionInstr{set, {Op: state.TCover, A: 1, Dst: 3, C: 3}},
			[]state.ActionInstr{{Op: state.TField, Dst: 3, A: 1, C: 2}}},
		{"cover into another register", []state.ActionInstr{step, set, {Op: state.TCover, A: 1, Dst: 3, C: 4}},
			[]state.ActionInstr{step, set, {Op: state.TCover, A: 1, Dst: 3, C: 4}}},
		{"cover of another value", []state.ActionInstr{step, set, {Op: state.TCover, A: 1, B: 5, Dst: 3, C: 3}},
			[]state.ActionInstr{step, set, {Op: state.TCover, A: 1, B: 5, Dst: 3, C: 3}}},
		{"field after a step", []state.ActionInstr{step, set, {Op: state.TCover, A: 1, Dst: 3, C: 3}},
			[]state.ActionInstr{step, {Op: state.TField, Dst: 3, A: 1, C: 2}}},
		{"moved return", []state.ActionInstr{{Op: state.TCap, Dst: 1}, {Op: state.TMoveA, Dst: 2, A: 1}, {Op: state.TRet, A: 2}},
			[]state.ActionInstr{{Op: state.TCap, Dst: 1}, {Op: state.TRet, A: 1}}},
		{"return of another register", []state.ActionInstr{{Op: state.TMoveA, Dst: 2, A: 1}, {Op: state.TRet, A: 3}},
			[]state.ActionInstr{{Op: state.TMoveA, Dst: 2, A: 1}, {Op: state.TRet, A: 3}}},
		{"return a jump goes to", []state.ActionInstr{{Op: state.TJumpF, Dst: 2}, {Op: state.TMoveA, Dst: 2, A: 1}, {Op: state.TRet, A: 2}},
			[]state.ActionInstr{{Op: state.TJumpF, Dst: 2}, {Op: state.TMoveA, Dst: 2, A: 1}, {Op: state.TRet, A: 2}}},
	} {
		if got := emit(c.code...); !slices.Equal(got, c.want) {
			t.Errorf("%s: emitted %v, want %v", c.name, got, c.want)
		}
	}
}
