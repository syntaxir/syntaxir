package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// composed returns the instructions that the fused instruction in stands for, placed at index at of
// the composed body, with fresh returning a register of the frame that no other instruction uses, or
// nil if in is no fused instruction that the test composes. The targets of the instructions are
// indices of the composed body, one past the last continuing after them, but for those that take the
// target of in, which keep it.
func composed(in bytecode.FlowInstr, at int, fresh func() int) []bytecode.FlowInstr {
	reg := func(r int) int {
		if r < 0 {
			return fresh()
		}
		return r
	}
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
	case bytecode.FlowScanClass, bytecode.FlowScanClassCount, bytecode.FlowScanClassAt:
		// The loop: CONST -1 into the step's register before it; then STEP; MATCHCLASS, leaving it;
		// ADVANCE; for a count, CONST 1 and ADD, then for a walk SCANREACHAT; and JUMP back to the STEP.
		test, site := bytecode.FlowClassTestOf{}, 0
		if x, ok := in.Imm.(bytecode.FlowScanClassOf); ok {
			test, site = x.Test, x.Site
		} else {
			test = in.Imm.(bytecode.FlowClassTestOf)
		}
		ch, size, ok := reg(in.Dst[0]), reg(in.Dst[1]), reg(in.Dst[2])
		var seq []bytecode.FlowInstr
		n := -1
		if in.Op != bytecode.FlowScanClass {
			n = reg(in.Dst[3])
			if from := in.Args[len(in.Args)-1]; from != n {
				seq = append(seq, bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{n}, Args: []int{from}})
			}
		}
		last := fresh()
		seq = append(seq, bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{last}, Imm: -1})
		loop := at + len(seq)
		seq = append(seq,
			bytecode.FlowInstr{Op: bytecode.FlowStep, Dst: []int{last}, Args: []int{last}},
			bytecode.FlowInstr{Op: bytecode.FlowMatchClass, Dst: []int{ch, size, ok}, Imm: test},
			bytecode.FlowInstr{Op: bytecode.FlowAdvance, Args: []int{size}})
		exit := len(seq) - 2
		if n >= 0 {
			one := fresh()
			seq = append(seq,
				bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{one}, Imm: 1},
				bytecode.FlowInstr{Op: bytecode.FlowAdd, Dst: []int{n}, Args: []int{n, one}})
		}
		if site > 0 {
			seq = append(seq, bytecode.FlowInstr{Op: bytecode.FlowScanReachAt, Dst: []int{n}, Args: []int{in.Args[0], n}, Imm: site})
		}
		seq = append(seq, bytecode.FlowInstr{Op: bytecode.FlowJump, Target: loop})
		seq[exit].Target = at + len(seq)
		return seq
	case bytecode.FlowMatchClassNode:
		size, start, text := reg(in.Dst[1]), reg(in.Dst[3]), fresh()
		return []bytecode.FlowInstr{
			{Op: bytecode.FlowMatchClass, Dst: []int{in.Dst[0], size, in.Dst[2]}, Target: in.Target, Imm: in.Imm},
			{Op: bytecode.FlowPos, Dst: []int{start}},
			{Op: bytecode.FlowAdvance, Args: []int{size}},
			{Op: bytecode.FlowText, Dst: []int{text}, Args: []int{start}},
			{Op: bytecode.FlowNewTerminal, Dst: []int{in.Dst[4]}, Args: []int{start, text}},
		}
	case bytecode.FlowMatchLitNode:
		x := in.Imm.(bytecode.FlowLitNodeOf)
		start, text := reg(in.Dst[0]), fresh()
		return []bytecode.FlowInstr{
			{Op: bytecode.FlowPos, Dst: []int{start}},
			{Op: bytecode.FlowMatchLitJump, Dst: []int{in.Dst[1], in.Dst[2]}, Target: in.Target, Imm: x.Lit},
			{Op: bytecode.FlowStr, Dst: []int{text}, Imm: x.Text},
			{Op: bytecode.FlowNewTerminal, Dst: []int{in.Dst[3]}, Args: []int{start, text}},
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
	var retarget []int // the indices in out of the instructions whose targets are instructions of f
	for i, in := range f.Code {
		at[i] = len(out)
		if seq := composed(in, len(out), fresh); seq != nil {
			ops[in.Op]++
			if sh, _ := in.Op.Shape(); sh.Target { // the composed jumps keep the target of in
				for j, c := range seq {
					if sh, _ := c.Op.Shape(); sh.Target {
						retarget = append(retarget, len(out)+j)
					}
				}
			}
			out = append(out, seq...)
			continue
		}
		retarget = append(retarget, len(out))
		out = append(out, in)
	}
	for _, i := range retarget {
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
	for _, op := range []bytecode.FlowOp{
		bytecode.FlowReturnOk, bytecode.FlowReturnFail,
		bytecode.FlowScanClass, bytecode.FlowScanClassCount, bytecode.FlowScanClassAt,
		bytecode.FlowMatchClassNode, bytecode.FlowMatchLitNode,
	} {
		if ops[op] == 0 {
			t.Errorf("no body has %v", op)
		}
	}
}
