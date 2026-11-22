package complexity

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// ruleAnalysis holds the rule facts that the classifier reads by rule name, taken from the facts
// of the Core IR module of the rules (sourceFacts). The classifier asks the facts of an expression
// of the module's node (classifier.nodeOf).
type ruleAnalysis struct {
	control *cancel.Control
	// Nullable and Succeeding are the nullable and always-succeeding rules.
	Nullable, Succeeding map[string]bool
	// Leaders are the rules that break left-recursion cycles; Cyclic the rules on a cycle; Cycle
	// numbers the cycle of each cyclic rule (the same number for the rules of one cycle).
	Leaders, Cyclic map[string]bool
	Cycle           map[string]int
	// Positional are the rules whose actions or predicates, or those of a rule they call, read
	// positions.
	Positional map[string]bool
	// Recovering maps each rule whose value can hold an error value of #recover to a rule with such
	// a #recover: the rule itself or one it calls.
	Recovering map[string]string
}

func newRuleAnalysis(control *cancel.Control) *ruleAnalysis {
	return &ruleAnalysis{control: control, Nullable: map[string]bool{}, Succeeding: map[string]bool{},
		Leaders: map[string]bool{}, Cyclic: map[string]bool{}, Cycle: map[string]int{},
		Positional: map[string]bool{}, Recovering: map[string]string{}}
}

// tarjan returns the strongly connected components of the graph of edges, starting from the rules
// in order. A component comes after every component it reaches, so callees come before their
// callers. A non-nil control polls for cancellation.
func tarjan(rules []*ast.RuleDef, edges map[string][]string, control *cancel.Control) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var sccs [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		control.Poll()
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			control.Poll()
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var scc []string
			for {
				control.Poll()
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}
	for _, r := range rules {
		control.Poll()
		if _, seen := index[r.Name]; !seen {
			visit(r.Name)
		}
	}
	return sccs
}
