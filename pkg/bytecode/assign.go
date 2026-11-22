package bytecode

import "slices"

// Unassigned returns, in increasing order, the registers of f that an instruction may read before
// any instruction writes them: on some path from the first instruction to a reachable instruction
// that reads the register as an argument, no instruction names it as a destination. A body without
// such a register reads only values that its own run wrote, so a frame of registers need not be
// cleared before the body runs. A loaded module has none (ValidateModule); the bytecode lowering
// writes the zero value of a local at the start of the body only where some path reads it unwritten.
//
// An instruction continues at the next one unless it is JUMP or RETURN, and at its target if its
// operation has one (FlowShape.Target). The operations, registers and targets must be in range.
func (f *Flow) Unassigned() []int {
	n := len(f.Code)
	if n == 0 {
		return nil
	}
	words := (f.Regs + 63) / 64
	// in holds, for each reached instruction, the registers written on every path to it found so
	// far. The first path that reaches an instruction sets its set; each later one intersects it.
	in := make([]uint64, n*words)
	reached := make([]bool, n)
	queued := make([]bool, n)
	reached[0], queued[0] = true, true
	work := []int{0}
	out := make([]uint64, words)
	flow := func(to int) {
		set := in[to*words : (to+1)*words]
		changed := false
		if !reached[to] {
			reached[to], changed = true, true
			copy(set, out)
		} else {
			for i := range set {
				if v := set[i] & out[i]; v != set[i] {
					set[i], changed = v, true
				}
			}
		}
		if changed && !queued[to] {
			queued[to] = true
			work = append(work, to)
		}
	}
	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
		queued[pc] = false
		ins := &f.Code[pc]
		copy(out, in[pc*words:(pc+1)*words])
		for _, d := range ins.Dst {
			if d >= 0 {
				out[d/64] |= 1 << (d % 64)
			}
		}
		if !ins.Op.Ends() && pc+1 < n {
			flow(pc + 1)
		}
		if s, _ := ins.Op.Shape(); s.Target {
			flow(ins.Target)
		}
	}
	var bad []int
	for pc := range f.Code {
		if !reached[pc] {
			continue
		}
		set := in[pc*words : (pc+1)*words]
		for _, a := range f.Code[pc].Args {
			if set[a/64]&(1<<(a%64)) == 0 && !slices.Contains(bad, a) {
				bad = append(bad, a)
			}
		}
	}
	slices.Sort(bad)
	return bad
}
