package bytecode

// RestoreAcrossCommit returns the first instruction of f, in instruction order, that may restore
// the parse to a state saved before an EMITSTREAM that ran since: a RESET, SETPOS, RESTORE or
// RESETMISSING that reads, on some path, a register that held a mark, a position, a number of
// recovered errors or a number of insertions (MARK, POS, RECLEN or MISSINGLEN, or a MOVE, MARKPOS,
// ADD or SUB of one) when an EMITSTREAM ran. ok is false if there is none. EMITSTREAM commits the
// input and the recovered errors before the position, so nothing after it may go back before it; a
// loaded module has no such instruction (ValidateModule), on any path, whether a choice, a loop or
// a recovery takes it.
//
// An instruction continues at the next one unless it is JUMP or RETURN, and at its target if its
// operation has one (FlowShape.Target). The operations, registers and targets must be in range.
func (f *Flow) RestoreAcrossCommit() (pc int, ok bool) {
	n := len(f.Code)
	emits := false
	for i := range f.Code {
		if f.Code[i].Op == FlowEmitStream {
			emits = true
			break
		}
	}
	if !emits {
		return 0, false
	}
	words := (f.Regs + 63) / 64
	// For each reached instruction, the registers that may hold a saved state (saved), and those
	// that may hold one saved before an EMITSTREAM that ran since (stale), on some path to it.
	saved := make([]uint64, n*words)
	stale := make([]uint64, n*words)
	reached := make([]bool, n)
	queued := make([]bool, n)
	reached[0], queued[0] = true, true
	work := []int{0}
	sv, st := make([]uint64, words), make([]uint64, words)
	has := func(set []uint64, r int) bool { return set[r/64]&(1<<(r%64)) != 0 }
	put := func(set []uint64, r int, v bool) {
		if v {
			set[r/64] |= 1 << (r % 64)
		} else {
			set[r/64] &^= 1 << (r % 64)
		}
	}
	flow := func(to int) {
		s, t := saved[to*words:(to+1)*words], stale[to*words:(to+1)*words]
		changed := !reached[to]
		reached[to] = true
		for i := range s {
			if v := s[i] | sv[i]; v != s[i] {
				s[i], changed = v, true
			}
			if v := t[i] | st[i]; v != t[i] {
				t[i], changed = v, true
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
		in := &f.Code[pc]
		copy(sv, saved[pc*words:(pc+1)*words])
		copy(st, stale[pc*words:(pc+1)*words])
		switch in.Op {
		case FlowEmitStream:
			for i := range st {
				st[i] |= sv[i]
			}
		case FlowMark, FlowPos, FlowRecLen, FlowMissingLen:
			if d := in.Dst[0]; d >= 0 {
				put(sv, d, true)
				put(st, d, false)
			}
		case FlowMove, FlowMarkPos, FlowAdd, FlowSub:
			s, t := false, false
			for _, a := range in.Args {
				s, t = s || has(sv, a), t || has(st, a)
			}
			if d := in.Dst[0]; d >= 0 {
				put(sv, d, s)
				put(st, d, t)
			}
		default:
			for _, d := range in.Dst {
				if d >= 0 {
					put(sv, d, false)
					put(st, d, false)
				}
			}
		}
		if !in.Op.Ends() && pc+1 < n {
			flow(pc + 1)
		}
		if s, _ := in.Op.Shape(); s.Target {
			flow(in.Target)
		}
	}
	for pc := range f.Code {
		in := &f.Code[pc]
		if !reached[pc] {
			continue
		}
		switch in.Op {
		case FlowReset, FlowSetPos, FlowRestore, FlowResetMissing:
			t := stale[pc*words : (pc+1)*words]
			for _, a := range in.Args {
				if has(t, a) {
					return pc, true
				}
			}
		}
	}
	return 0, false
}
