package vmlower

import (
	"slices"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// coalesceCopies removes the copies of a value from the register that an instruction writes it to
// into the register of another local: where an instruction writes a register that only the MOVE right
// after it reads, and no jump goes to that MOVE, the instruction writes the MOVE's destination
// itself. A Flow function that gives a value to one local and then to another, as a choice does when
// each alternative's value becomes the choice's, thus keeps it where it ends up.
func coalesceCopies(f *bytecode.Flow) {
	code := f.Code
	reads := make(map[int]int)
	targets := make(map[int]bool)
	for _, in := range code {
		for _, a := range in.Args {
			reads[a]++
		}
		if sh, _ := in.Op.Shape(); sh.Target {
			targets[in.Target] = true
		}
	}
	keep := make([]bool, len(code))
	for i := range code {
		keep[i] = true
		if i == 0 || !keep[i-1] || targets[i] {
			continue
		}
		in, prev := &code[i], &code[i-1]
		if in.Op != bytecode.FlowMove || in.Dst[0] < 0 || reads[in.Args[0]] != 1 {
			continue
		}
		if slices.Contains(prev.Dst, in.Dst[0]) {
			continue
		}
		for d, r := range prev.Dst {
			if r == in.Args[0] {
				prev.Dst[d] = in.Dst[0]
				keep[i] = false
				break
			}
		}
	}
	compact(f, keep)
	renameCopied(f)
}

// renameCopied removes the copy MOVE d = r of a register r that no other instruction reads, where
// several instructions write r and jumps join them at the MOVE, as the alternatives of a choice do:
// each of them writes d instead. It does so only where d is dead after each of them, so every read of
// d sees what it saw before.
func renameCopied(f *bytecode.Flow) {
	for {
		code := f.Code
		reads := map[int]int{}
		for _, in := range code {
			for _, a := range in.Args {
				reads[a]++
			}
		}
		done := true
		for m := range code {
			in := &code[m]
			if in.Op != bytecode.FlowMove {
				continue
			}
			d, r := in.Dst[0], in.Args[0]
			if d < 0 || d == r || reads[r] != 1 || !renamable(code, m, r, d) {
				continue
			}
			for i := range code {
				for k, x := range code[i].Dst {
					if x == r {
						code[i].Dst[k] = d
					}
				}
			}
			keep := make([]bool, len(code))
			for i := range keep {
				keep[i] = i != m
			}
			compact(f, keep)
			done = false
			break
		}
		if done {
			return
		}
	}
}

// renamable reports whether the instructions of code that write r may write d instead, with the MOVE
// d = r at m removed: none of them also writes d, and d is dead after each of them, that is no
// instruction that reads d is reached from one of them before an instruction without a jump that writes
// d, the MOVE among them.
func renamable(code []bytecode.FlowInstr, m, r, d int) bool {
	seen := make([]bool, len(code))
	var work []int
	for i, in := range code {
		if slices.Contains(in.Dst, r) {
			if slices.Contains(in.Dst, d) {
				return false
			}
			work = append(work, successors(code, i)...)
		}
	}
	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		if i == m || i >= len(code) || seen[i] {
			continue
		}
		seen[i] = true
		if slices.Contains(code[i].Args, d) {
			return false
		}
		if sh, _ := code[i].Op.Shape(); slices.Contains(code[i].Dst, d) && !sh.Target {
			continue // d is written here on every path on
		}
		work = append(work, successors(code, i)...)
	}
	return true
}

// successors returns the instructions that may run after the instruction at i.
func successors(code []bytecode.FlowInstr, i int) []int {
	in := &code[i]
	var next []int
	if sh, _ := in.Op.Shape(); sh.Target {
		next = append(next, in.Target)
	}
	if in.Op != bytecode.FlowJump && in.Op != bytecode.FlowReturn {
		next = append(next, i+1)
	}
	return next
}

// compact keeps the instructions of f that keep marks, with each jump retargeted to the instruction
// that its target became, or to the next one kept if its target is removed.
func compact(f *bytecode.Flow, keep []bool) {
	at := make([]int, len(f.Code)+1)
	n := 0
	for i := range f.Code {
		at[i] = n
		if keep[i] {
			n++
		}
	}
	at[len(f.Code)] = n
	out := f.Code[:0]
	for i, in := range f.Code {
		if !keep[i] {
			continue
		}
		if sh, _ := in.Op.Shape(); sh.Target {
			in.Target = at[in.Target]
		}
		out = append(out, in)
	}
	f.Code = out
}
