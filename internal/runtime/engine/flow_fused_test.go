package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// composed returns the instructions that the fused instruction in stands for, with fresh returning
// a register of the frame that no other instruction uses, or nil if in is no fused instruction that
// the test composes.
func composed(in bytecode.FlowInstr, fresh func() int) []bytecode.FlowInstr {
	switch in.Op {
	case bytecode.FlowReturnOk:
		yes := fresh()
		return []bytecode.FlowInstr{
			{Op: bytecode.FlowConst, Dst: []int{yes}, Imm: true},
			{Op: bytecode.FlowReturn, Args: []int{in.Args[0], yes}},
		}
	case bytecode.FlowReturnFail:
		nothing, no := fresh(), fresh()
		return []bytecode.FlowInstr{
			{Op: bytecode.FlowConst, Dst: []int{nothing}},
			{Op: bytecode.FlowConst, Dst: []int{no}, Imm: false},
			{Op: bytecode.FlowReturn, Args: []int{nothing, no}},
		}
	}
	return nil
}

// compose replaces each fused instruction of f by the instructions it stands for (composed), each
// jump to it going to the first of them, and adds the operation of each to ops.
func compose(f *bytecode.Flow, ops map[bytecode.FlowOp]int) {
	fresh := func() int { f.Regs++; return f.Regs - 1 }
	at := make([]int, len(f.Code)) // the index of the first instruction that stands for each
	var out []bytecode.FlowInstr
	for i, in := range f.Code {
		at[i] = len(out)
		if seq := composed(in, fresh); seq != nil {
			ops[in.Op]++
			out = append(out, seq...)
			continue
		}
		out = append(out, in)
	}
	for i := range out {
		if sh, _ := out[i].Op.Shape(); sh.Target {
			out[i].Target = at[out[i].Target]
		}
	}
	f.Code = out
}

// TestFusedFlowInstructionsChargeAsComposed checks that each fused instruction does and charges
// exactly what the instructions it stands for do: the testdata grammars parse their samples with the
// bodies the compiler selects and with the same bodies where every fused instruction is replaced by
// its composition, in both execution models, and the trees, the errors and the units used must be
// the same. Every fused instruction the test composes must occur in the bodies.
func TestFusedFlowInstructionsChargeAsComposed(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	ops := map[bytecode.FlowOp]int{}
	all := func(string) bool { return true }
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		samples, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.txt"))
		if err != nil || len(samples) == 0 {
			continue
		}
		_, fused, _ := flowProgramsOf(t, string(src), all, nil)
		_, plain, _ := flowProgramsOf(t, string(src), all, func(_ string, f *bytecode.Flow) { compose(f, ops) })
		for _, sample := range samples {
			text, err := os.ReadFile(sample)
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				ft, fm, fu := parseChargedWith(t, fused, string(text), b)
				pt, pm, pu := parseChargedWith(t, plain, string(text), b)
				if ft != pt || fm != pm || fu != pu {
					t.Errorf("%s %s: fused: %d units, %s; composed: %d units, %s", strings.TrimPrefix(sample, "../../../"), b, fu, fm, pu, pm)
				}
			}
		}
	}
	for _, op := range []bytecode.FlowOp{bytecode.FlowReturnOk, bytecode.FlowReturnFail} {
		if ops[op] == 0 {
			t.Errorf("no body has %v", op)
		}
	}
}
