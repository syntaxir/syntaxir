package bytecode

import "slices"

// UnboundedCycle returns an instruction of f on a cycle that may repeat without end under a finite
// compute limit, and ok false if there is none. A cycle is bounded when it charges a step (STEP), so
// that a finite compute limit stops it, or when every pass of it reuses an element of a run
// (RESUMENEXT) that the cycle does not start (RESUMESTART): a run holds finitely many elements, and
// a RESUMENEXT bounds a cycle only if the next instruction is a JUMPFALSE that tests its success
// and leaves the cycle when the run has no element left. A loaded module has no unbounded cycle
// (ValidateModule), so a parse of it ends under a finite compute limit.
//
// The check is structural and conservative: it considers the cycles of f that avoid STEP together,
// as the strongly connected components of the body without its STEP instructions. A RESUMENEXT
// bounds a component only if the component has no RESUMESTART and its JUMPFALSE leaves the
// component, which the body can then enter again only through a STEP; every cycle of the component
// must pass through such a RESUMENEXT.
//
// An instruction continues at the next one unless it is JUMP or RETURN, and at its target if its
// operation has one (FlowShape.Target). The operations, registers and targets must be in range.
func (f *Flow) UnboundedCycle() (pc int, ok bool) {
	n := len(f.Code)
	succ := func(pc int, emit func(int)) {
		in := &f.Code[pc]
		if in.Op != FlowJump && in.Op != FlowReturn && pc+1 < n {
			emit(pc + 1)
		}
		if s, _ := in.Op.Shape(); s.Target {
			emit(in.Target)
		}
	}
	in := make([]bool, n) // the instructions of the graph that cycles are looked for in
	for pc := range f.Code {
		in[pc] = f.Code[pc].Op != FlowStep
	}
	for _, comp := range components(n, in, succ) {
		member := make(map[int]bool, len(comp))
		starts := false
		for _, pc := range comp {
			member[pc] = true
			starts = starts || f.Code[pc].Op == FlowResumeStart
		}
		// The component without the RESUMENEXTs that bound it must have no cycle.
		sub := make([]bool, n)
		for _, pc := range comp {
			sub[pc] = starts || !f.boundsByRun(pc, member)
		}
		if rest := components(n, sub, succ); len(rest) > 0 {
			return rest[0][0], true
		}
	}
	return 0, false
}

// boundsByRun reports whether the instruction pc is a RESUMENEXT whose success the next instruction
// tests with a JUMPFALSE that leaves the instructions member.
func (f *Flow) boundsByRun(pc int, member map[int]bool) bool {
	in := &f.Code[pc]
	if in.Op != FlowResumeNext || pc+1 >= len(f.Code) || in.Dst[1] < 0 {
		return false
	}
	next := &f.Code[pc+1]
	return next.Op == FlowJumpFalse && next.Args[0] == in.Dst[1] && !member[next.Target]
}

// components returns the strongly connected components of the graph of the n instructions for which
// in holds, with the edges succ between them, that have a cycle: more than one instruction, or one
// that continues at itself. Each lists its instructions in increasing order of the first, and the
// components come in the order of their least instruction.
func components(n int, in []bool, succ func(int, func(int))) [][]int {
	// Tarjan's algorithm, iterative so that a long body does not deepen the host stack.
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var stack []int
	var comps [][]int
	next := 0
	type frame struct {
		pc   int
		outs []int
		i    int
	}
	outs := func(pc int) []int {
		var s []int
		succ(pc, func(to int) {
			if in[to] {
				s = append(s, to)
			}
		})
		return s
	}
	for root := range n {
		if !in[root] || index[root] >= 0 {
			continue
		}
		index[root], low[root], next = next, next, next+1
		stack, onStack[root] = append(stack, root), true
		calls := []frame{{pc: root, outs: outs(root)}}
		for len(calls) > 0 {
			top := &calls[len(calls)-1]
			if top.i < len(top.outs) {
				to := top.outs[top.i]
				top.i++
				switch {
				case index[to] < 0:
					index[to], low[to], next = next, next, next+1
					stack, onStack[to] = append(stack, to), true
					calls = append(calls, frame{pc: to, outs: outs(to)})
				case onStack[to]:
					low[top.pc] = min(low[top.pc], index[to])
				}
				continue
			}
			pc := top.pc
			calls = calls[:len(calls)-1]
			if len(calls) > 0 {
				parent := calls[len(calls)-1].pc
				low[parent] = min(low[parent], low[pc])
			}
			if low[pc] != index[pc] {
				continue
			}
			var comp []int
			for {
				x := stack[len(stack)-1]
				stack, onStack[x] = stack[:len(stack)-1], false
				comp = append(comp, x)
				if x == pc {
					break
				}
			}
			self := false
			if len(comp) == 1 {
				for _, to := range outs(pc) {
					self = self || to == pc
				}
			}
			if len(comp) > 1 || self {
				comps = append(comps, comp)
			}
		}
	}
	for _, c := range comps {
		slices.Sort(c)
	}
	slices.SortFunc(comps, func(a, b []int) int { return a[0] - b[0] })
	return comps
}
