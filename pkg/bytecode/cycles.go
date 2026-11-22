package bytecode

import "slices"

// UnboundedCycle returns an instruction of f on a cycle that may repeat without end, and ok false if
// there is none. A cycle is bounded when it passes through a step (STEP) that no other instruction of
// the cycle writes the register of: a step requires the position to have advanced past the one its
// register holds, which the previous run of the step wrote, so the position strictly increases from
// one run of the step to the next and the finite input bounds the runs. A cycle is also bounded when
// every pass of it reuses an element of a run (RESUMENEXT) that the cycle does not start
// (RESUMESTART): a run holds finitely many elements, and a RESUMENEXT bounds a cycle only if the next
// instruction is a JUMPFALSE that tests its success and leaves the cycle when the run has no element
// left. A loaded module has no unbounded cycle (ValidateModule), so a run of each of its bodies ends,
// whatever the limits of the parse.
//
// The check is structural and conservative, on the strongly connected components of the body: in a
// component, the steps whose register no other instruction of the component writes, and, if the
// component has no RESUMESTART, the RESUMENEXTs whose JUMPFALSE leaves it, bound every cycle through
// them. A component with none of these is unbounded; otherwise the check goes on in the components
// of what remains without them. An execution that does not end eventually stays in a strongly
// connected set of instructions, which lies in one component at each level of the check and passes
// through none of the instructions that bound it, so it lies in a component with none.
//
// An instruction continues at the next one unless it is JUMP or a return (FlowOp.Ends), and at its
// target if its operation has one (FlowShape.Target). The operations, registers and targets must be in range.
func (f *Flow) UnboundedCycle() (pc int, ok bool) {
	n := len(f.Code)
	succ := func(pc int, emit func(int)) {
		in := &f.Code[pc]
		if !in.Op.Ends() && pc+1 < n {
			emit(pc + 1)
		}
		if s, _ := in.Op.Shape(); s.Target {
			emit(in.Target)
		}
	}
	in := make([]bool, n) // the instructions of the graph that cycles are looked for in
	for pc := range in {
		in[pc] = true
	}
	return f.unboundedIn(in, succ)
}

// unboundedIn returns an instruction on an unbounded cycle among the instructions in, with the edges
// succ between them (UnboundedCycle).
func (f *Flow) unboundedIn(in []bool, succ func(int, func(int))) (pc int, ok bool) {
	n := len(f.Code)
	for _, comp := range components(n, in, succ) {
		member := make(map[int]bool, len(comp))
		starts := false
		writers := map[int]int{} // the number of instructions of the component that write a register
		for _, pc := range comp {
			member[pc] = true
			starts = starts || f.Code[pc].Op == FlowResumeStart
			for _, d := range f.Code[pc].Dst {
				if d >= 0 {
					writers[d]++
				}
			}
		}
		rest := make([]bool, n)
		bounded := false
		for _, pc := range comp {
			cut := f.boundsByProgress(pc, writers) || !starts && f.boundsByRun(pc, member)
			rest[pc] = !cut
			bounded = bounded || cut
		}
		if !bounded {
			return comp[0], true
		}
		if pc, ok := f.unboundedIn(rest, succ); ok {
			return pc, true
		}
	}
	return 0, false
}

// boundsByProgress reports whether the instruction pc is a step that reads and writes one register,
// which no other instruction writes as writers counts them.
func (f *Flow) boundsByProgress(pc int, writers map[int]int) bool {
	in := &f.Code[pc]
	return in.Op == FlowStep && len(in.Args) == 1 && len(in.Dst) == 1 && in.Args[0] == in.Dst[0] &&
		in.Dst[0] >= 0 && writers[in.Dst[0]] == 1
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
