package bytecode

import (
	"fmt"
	"slices"
	"sync"
)

// validateRec checks that every rule on a cycle of calls is marked Rec. Only the calls of such
// rules count against the nesting limit, so a rule that reaches a call of itself without the mark
// would recurse without bound, past the host stack. m must have passed the range checks of
// ValidateModule.
//
// The check runs on a graph that over-approximates the calls a run can make (newCallGraph). A rule
// is on a cycle of calls when its node is on a cycle of the graph: its strongly connected component
// has more than the node itself, or the node leads to itself.
func validateRec(m *Module) error {
	sc := recScratchPool.Get().(*recScratch)
	defer recScratchPool.Put(sc)
	g := newCallGraph(m, sc)
	return g.components(func(v int32, cycle bool) error {
		if m.Rules[v].Rec || !cycle && !slices.Contains(g.succ(v), v) {
			return nil
		}
		return fmt.Errorf("rule %s is on a cycle of calls but is not marked recursive", m.Strings[m.Rules[v].Name])
	})
}

// recScratch holds the buffer of newCallGraph, which loads reuse.
type recScratch struct {
	buf []int32
}

var recScratchPool = sync.Pool{New: func() any { return new(recScratch) }}

// resize returns s with length n, reusing its array when it is large enough. The elements are
// not cleared.
func resize[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// callGraph is the graph of validateRec in compressed form: node i is rule i, and its successors
// are succ(i). work is scratch space for components.
type callGraph struct {
	start  []int32
	edges  []int32
	rindex []int32
	work   []int32
}

// succ returns the successors of node v.
func (g *callGraph) succ(v int32) []int32 { return g.edges[g.start[v]:g.start[v+1]] }

// flowLines calls f with the Flow body of the skip and of each operand and operator line of p, nil
// for one that has none.
func flowLines(p *OperatorsRuleInfo, f func(*Flow)) {
	f(p.SkipFlow)
	for i := range p.Operands {
		f(p.Operands[i].Flow)
	}
	for i := range p.Prefix {
		f(p.Prefix[i].Line.Flow)
	}
	for i := range p.InfixPostfix {
		f(p.InfixPostfix[i].Line.Flow)
	}
}

// newCallGraph returns the graph of validateRec. A rule leads to the rules that the CALL and
// CALLJUMP instructions of its Flow bodies call: its body, its stream body, and the bodies of the
// lines and the skip of its Operators Rule. A Flow body may run any of its instructions, so the
// graph leads from a rule to a rule whenever a run of the one may call the other.
//
// The graph takes one buffer of sc, for the graph and the scratch space of components.
func newCallGraph(m *Module, sc *recScratch) callGraph {
	// flowCalls calls f with the rule of each call of the Flow bodies of rule i.
	flowCalls := func(i int, f func(rule int)) {
		calls := func(fl *Flow) {
			if fl == nil {
				return
			}
			for _, in := range fl.Code {
				if c, ok := in.Imm.(FlowCallOf); ok && (in.Op == FlowCall || in.Op == FlowCallJump) {
					f(c.Rule)
				}
			}
		}
		calls(m.Flows[i])
		calls(m.Streams[i])
		if o := m.Rules[i].OperatorsRule; o >= 0 && o < len(m.OperatorsRules) {
			flowLines(&m.OperatorsRules[o], calls)
		}
	}
	nodes, edges := len(m.Rules), 0
	for i := range m.Rules {
		flowCalls(i, func(int) { edges++ })
	}
	sc.buf = resize(sc.buf, (nodes+1)+nodes+3*nodes+edges)
	buf := sc.buf
	var g callGraph
	g.start, buf = buf[:nodes+1], buf[nodes+1:]
	g.rindex, buf = buf[:nodes], buf[nodes:]
	clear(g.rindex)
	g.work, buf = buf[:3*nodes], buf[3*nodes:]
	g.edges = buf[:0]
	for i := range m.Rules {
		g.start[i] = int32(len(g.edges))
		flowCalls(i, func(rule int) { g.edges = append(g.edges, int32(rule)) })
	}
	g.start[nodes] = int32(len(g.edges))
	return g
}

// components finds the strongly connected components of g and calls found with each node of a
// component when the component is complete, with whether it has more than one node, stopping at
// the first error. It runs Pearce's variant of Tarjan's algorithm without recursion, so that a
// long chain of calls does not deepen the host stack: rindex holds the visit order of a node
// while it is on the stack, lowered to the least order it reaches, and a value above every order
// once its component is complete.
func (g *callGraph) components(found func(v int32, cycle bool) error) error {
	nodes := int32(len(g.rindex))
	// work holds the frames of the visits from its start, three values each (the node, its next
	// edge and its visit order), and the nodes visited and not yet in a component from its end.
	// The two sets of nodes are disjoint, so they fit.
	work := g.work
	top, low := 0, len(work)
	index, comp := int32(1), nodes // 0 is unvisited; components are numbered down from nodes
	visit := func(v int32) {
		g.rindex[v] = index
		work[top], work[top+1], work[top+2] = v, g.start[v], index
		top += 3
		index++
	}
	for root := range nodes {
		if g.rindex[root] != 0 {
			continue
		}
		visit(root)
		for top > 0 {
			f := work[top-3 : top]
			v := f[0]
			if f[1] < g.start[v+1] {
				w := g.edges[f[1]]
				f[1]++
				if g.rindex[w] == 0 {
					visit(w)
				} else {
					g.rindex[v] = min(g.rindex[v], g.rindex[w])
				}
				continue
			}
			top -= 3
			if g.rindex[v] == f[2] {
				// v is the root of its component: the nodes above it on the stack belong to it.
				index--
				cycle := low < len(work) && g.rindex[work[low]] >= g.rindex[v]
				for low < len(work) && g.rindex[work[low]] >= g.rindex[v] {
					w := work[low]
					low++
					g.rindex[w] = comp
					index--
					if err := found(w, true); err != nil {
						return err
					}
				}
				g.rindex[v] = comp
				comp--
				if err := found(v, cycle); err != nil {
					return err
				}
			} else {
				low--
				work[low] = v
			}
			if top > 0 {
				u := work[top-3]
				g.rindex[u] = min(g.rindex[u], g.rindex[v])
			}
		}
	}
	return nil
}
