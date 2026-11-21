package print

import (
	"fmt"
	"sort"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	irbuild "syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A rule of the Node table (rules), the recognizer table (recRules) or the typed table (trules) has
// a Go body and the functions that body calls. The generator prints them only for the rules that
// something can enter: the start rules, which the generated entry points and Starts reach (for the
// typed table, the start rules whose typed entry point calls the rule's Entry), and the rules a
// printed body calls. A rule that the compiler routes through its value-free twin
// (internal/compile/compile_walk.go, the Ref case) has an original that only value contexts call,
// so a rule referenced only from value-free contexts has an original body that nothing calls, and
// so on transitively.
//
// The program is planned before any table is printed (planProgram, flowplan.Planning): the plan takes
// the calls of the rules from the Core IR (core.Module.Reach), routes each call of a rule through its
// wrapper or its slot (flowplan.CallsWrapper), and starts from the start rules and the typed rules that
// the typed entry points enter (typedEntries, decided before printing). GenerateParser then prints
// once and leaves out the rules that the plan does not reach. The rule tables still list every rule,
// a left-out row with the frame layout of the plan (plandata.Plan.Layouts): only the Body, Action,
// Entry and OperatorsRule assignments and the functions of an unreachable rule are left out. What a
// left-out rule would have declared (its literals, class tables and typed helpers) is not declared
// either.
//
// The expectation table of the Node and recognizer tables is numbered before they are printed, from
// the tables lowered with every rule (expectationTable), because a left-out rule of them keeps its
// table row: its IDs are those that printing every rule would give. The typed table has no such rows,
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
	cores *plandata.Cores   // what the program was planned from
	plan  *plandata.Program // the plan, whose routes reachFrom follows
}

// reachFrom sets l.Reach to the rules that the roots reach in the plan (flowplan.Reach).
func (l *liveRules) reachFrom(roots []ruleKey) {
	l.Reach = flowplan.Reach(l.cores, l.plan, roots)
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
	return g.live == nil || g.Table == "trules" || g.live.Reach[ruleKey{Table: g.Table, ID: r.ID, Wrapper: false}]
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

// planProgram plans the program before any table is printed (flowplan.Planning) and keeps the plan
// in g.program: the Node table of prog, the recognizer table of rec if it is not nil and, if typed is
// set, the typed table, which enters the rules that the typed entry points enter (typedRoots). Unless
// keepUnreachable is set, the generator then prints only the rules that the plan reaches (g.live).
func (g *Generator) planProgram(prog, rec *common.Program, typed, keepUnreachable bool) error {
	c := &plandata.Cores{}
	for _, name := range prog.StartRules {
		c.Roots = append(c.Roots, ruleKey{Table: "rules", ID: prog.ByName[name].ID})
		if rec != nil {
			c.Roots = append(c.Roots, ruleKey{Table: "recRules", ID: rec.ByName[name].ID})
		}
	}
	c.Roots = append(c.Roots, g.typedRoots...)
	add := func(table string, pr *common.Program) error {
		m, err := irbuild.CoreFrom(pr.Elab, false)
		if err != nil {
			return err
		}
		t := plandata.CoreTable{Name: table, M: m, Typed: table == "trules", Slots: table == "rules" && g.Document}
		for _, r := range append(append([]*common.Rule(nil), pr.RuleList...), pr.Twins...) {
			_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
			t.Rules = append(t.Rules, plandata.CallRule{Plain: r.Plain, Leader: r.Leader, Operators: operatorsRule})
		}
		c.Tables = append(c.Tables, t)
		return nil
	}
	if err := add("rules", prog); err != nil {
		return err
	}
	if rec != nil {
		if err := add("recRules", rec); err != nil {
			return err
		}
	}
	if typed {
		if err := add("trules", prog); err != nil {
			return err
		}
	}
	p, err := g.Runner.Plan(c)
	if err != nil {
		return err
	}
	g.program = p
	if !keepUnreachable {
		g.live = &liveRules{Roots: p.Roots, Reach: p.Reach, cores: c, plan: p}
	}
	return nil
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
	for _, table := range []string{"rules", "recRules"} {
		if g.keepTableRules && table == "rules" {
			continue // the Node rules are all kept
		}
		if miss := g.live.missingBodies(table, g.bodySet); len(miss) > 0 {
			return fmt.Errorf("internal error: the reachable rule slot %s[%d] has no Body", table, miss[0].ID)
		}
	}
	return nil
}
