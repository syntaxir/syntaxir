package print

import (
	"fmt"
	"sort"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// A rule of the Node table (rules), the recognizer table (recRules) or the typed table (trules) has
// a Go body and the functions that body calls. The generator prints them only for the rules that
// something can enter: the start rules, which the generated entry points and Starts reach (for the
// typed table, the start rules whose typed entry point calls the rule's Entry), and the rules a
// printed body calls. A rule that the compiler routes through its value-free twin
// (internal/frontend/check/compile_walk.go, the Ref case) has an original that only value contexts call,
// so a rule referenced only from value-free contexts has an original body that nothing calls, and
// so on transitively.
//
// The program is planned before any table is lowered (the Go backend, package golang): the plan
// takes the calls of the rules from the Core IR of the tables after the Core passes
// (core.Module.Reach), routes each call of a rule through its wrapper or its slot
// (flowplan.CallsWrapper), and starts from the start rules and the typed rules that the typed entry
// points enter (typedEntries, decided before any table is built). The tables are planned with the
// program and lowered leaving out the rules that the plan does not reach (plandata.Unit.Prune), and
// printed once. The rule tables still list every rule, a left-out row with the frame layout of the
// plan (plandata.Plan.Layouts): only the Body, Action,
// Entry and OperatorsRule assignments and the functions of an unreachable rule are left out. What a
// left-out rule would have declared (its literals, class tables and typed helpers) is not declared
// either.
//
// The expectation table of the Node and recognizer tables is numbered before they are printed, from
// the tables with every rule, whose left-out functions are lowered as parts of their own
// (plandata.Program.Numbered), because a left-out rule of them keeps its table row: its IDs are those
// that printing every rule would give. The typed table has no such rows,
// so the expectations of its left-out rules are not in any table.

// ruleKey names what of a rule is entered: its slot in the rule table (the Body that p.Call and
// InvokePlain run), or the wrapper function of a plain rule, which a call of it (ruleCall) uses
// instead and which does not run the Body.
type ruleKey = plandata.Entry

// liveRules is what the generator prints of the plan of the program: the rules that it reaches, which
// a test may change (Hooks.Reach).
type liveRules struct {
	Roots []ruleKey // the start rules of the tables and the entries of the typed table
	Reach map[ruleKey]bool
	plan  *plandata.Program // the plan, whose routes reachFrom follows
}

// reachFrom sets l.Reach to the rules that the roots reach in the plan (plandata.Program.Reached).
func (l *liveRules) reachFrom(roots []ruleKey) {
	l.Reach = l.plan.Reached(roots)
}

// missingBodies returns the rules of the table that are reachable but have no printed Body.
func (l *liveRules) missingBodies(table string, printed map[ruleKey]bool) []ruleKey {
	var miss []ruleKey
	for k := range l.Reach {
		if k.Table == table && !k.Wrapper && !printed[k] {
			miss = append(miss, k)
		}
	}
	sort.Slice(miss, func(i, j int) bool { return miss[i].ID < miss[j].ID })
	return miss
}

// pruned reports whether the second pass leaves out everything of rule r of the current table.
func (g *Generator) pruned(r *common.Rule) bool {
	if g.live == nil || g.keepTableRules && g.Table == "rules" {
		return false
	}
	return !g.live.Reach[ruleKey{Table: g.Table, ID: r.ID, Wrapper: false}] && !g.live.Reach[ruleKey{Table: g.Table, ID: r.ID, Wrapper: true}]
}

// slotReached reports whether the Body of rule r is printed: something enters the rule through its
// slot, and not only through its wrapper.
func (g *Generator) slotReached(r *common.Rule) bool {
	return g.live == nil || g.typed() || g.live.Reach[ruleKey{Table: g.Table, ID: r.ID, Wrapper: false}]
}

// callWrapper reports whether a call of the rule r of the table is printed as a call of its wrapper
// function, which does not run the Body of its table entry, rather than through the entry (ruleCall):
// the route of the plan of the program.
func (g *Generator) callWrapper(table string, r *common.Rule) bool {
	return g.program.Wrapper(table, r.ID)
}

// checkCall checks in the second pass that the callee of a printed call, through its wrapper or its
// slot, was not left out.
func (g *Generator) checkCall(r *common.Rule, wrapper bool) {
	to := ruleKey{Table: g.Table, ID: r.ID, Wrapper: wrapper}
	if g.live != nil && !g.live.Reach[to] && g.reachErr == nil && (g.Table != "rules" || !g.keepTableRules) {
		g.reachErr = fmt.Errorf("internal error: a generated call reaches the rule %s of %s, whose body is left out", r.Name, g.Table)
	}
}

// printedBody records that the Body of rule r of the current table is printed.
func (g *Generator) printedBody(r *common.Rule) {
	if g.bodySet == nil {
		g.bodySet = map[ruleKey]bool{}
	}
	g.bodySet[ruleKey{Table: g.Table, ID: r.ID, Wrapper: false}] = true
}

// finishReach ends a pass: the second checks that no printed call reaches a rule left out and that
// every reachable rule has its Body.
func (g *Generator) finishReach() error {
	if g.keepTableRules && g.convertsNodes {
		return fmt.Errorf("internal error: a typed entry point converts Nodes, but the parser prints no Node rule bodies")
	}
	if g.live == nil {
		return nil
	}
	if g.reachErr != nil {
		return g.reachErr
	}
	for _, table := range []string{"rules", "recRules", "insRules", "insRecRules"} {
		if g.keepTableRules && table == "rules" {
			continue // the Node rules are all kept
		}
		if miss := g.live.missingBodies(table, g.bodySet); len(miss) > 0 {
			return fmt.Errorf("internal error: the reachable rule slot %s[%d] has no Body", table, miss[0].ID)
		}
	}
	return nil
}
