package print

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A generated parser prints only the rules that a parse can enter: the start rules, which parseRule
// names and parse and recognize run, and the rules that the printed bodies call: the reach of the
// plan of the program (plandata.Program.Reach). The Core IR can also have calls that the printed
// code leaves out, after an expression that never matches; print-ts then returns the rules that the
// printed calls reach (printedReach) and the driver compiles again with them as the reach of the plan.
// A rule left out has no constant, body, action, plain function or table row, and what only it would
// have declared (helpers, literals, expectations) is not declared either. A rule that returns a tree
// has a value-free twin that every call from a value-free context uses, so the original of a rule
// that only such contexts reference is left out, and so are the originals that only it calls.
//
// Rule IDs and the numbering of memoized rules (Seen) are those of the compiled program, so the
// remaining rules keep their values. The printer checks that no printed call reaches a rule left out.

// tsLive holds, for each table ("rules", "recRules", "insRules" or "insRecRules"), the ways in which
// the rules are entered.
type tsLive struct {
	reach map[string][]core.Entry
}

// printedReach returns the rules that a parse can enter through the calls that the generated code
// prints (checkCall), from the entry points of the parser p: every start rule of the Node table and of
// the insertion table, which parseRule can name, and the start rule that parse and recognize run, in
// each table. Printing leaves out the calls that Core IR has at points that the lowered code never
// reaches, such as after a lookahead that always fails.
func (g *Generator) printedReach(p *Parser) *tsLive {
	l, _ := reachFrom(p, g.Document, func(table string, p *common.Program) (*core.Module, error) {
		m := &core.Module{Calls: make([][]int, len(p.RuleList)+len(p.Twins))}
		for from, to := range g.printed[table] {
			m.Calls[from] = to
		}
		m.Rules = make([]*core.Rule, len(m.Calls))
		return m, nil
	})
	return l
}

// reachFrom computes the reach of the entry points of printedReach over the call graph of the module that module
// returns for each table.
func reachFrom(parser *Parser, document bool, module func(table string, p *common.Program) (*core.Module, error)) (*tsLive, error) {
	l := &tsLive{reach: map[string][]core.Entry{}}
	start := parser.Start
	for _, t := range parser.tables() {
		table, p := t.table, t.prog
		m, err := module(table, p)
		if err != nil {
			return nil, err
		}
		var roots []core.Root
		if table == "rules" || table == "insRules" {
			for _, name := range p.StartRules {
				roots = append(roots, core.Root{Rule: p.ByName[name].ID, Entry: core.EnterSlot})
			}
		}
		roots = append(roots, core.Root{Rule: p.ByName[start.Name].ID, Entry: core.EnterSlot})
		rules := append(append([]*common.Rule(nil), p.RuleList...), p.Twins...)
		l.reach[table] = m.Reach(roots, func(id int) core.Entry {
			if callsPlain(rules[id], document) {
				return core.EnterWrapper
			}
			return core.EnterSlot
		})
	}
	return l, nil
}

// liveOf returns the rules that the plan p reaches, in the ways it reaches them, for each of its tables.
func liveOf(p *plandata.Program) *tsLive {
	l := &tsLive{reach: map[string][]core.Entry{}}
	for _, t := range p.Tables {
		entries := make([]core.Entry, len(t.Names))
		for id := range entries {
			if p.Reach[plandata.Entry{Table: t.Table, ID: id}] {
				entries[id] |= core.EnterSlot
			}
			if p.Reach[plandata.Entry{Table: t.Table, ID: id, Wrapper: true}] {
				entries[id] |= core.EnterWrapper
			}
		}
		l.reach[t.Table] = entries
	}
	return l
}

// entries returns the reach of l as the reach of a plan (plandata.Program.Reach).
func (l *tsLive) entries() map[plandata.Entry]bool {
	r := map[plandata.Entry]bool{}
	for table, entries := range l.reach {
		for id, e := range entries {
			if e&core.EnterSlot != 0 {
				r[plandata.Entry{Table: table, ID: id}] = true
			}
			if e&core.EnterWrapper != 0 {
				r[plandata.Entry{Table: table, ID: id, Wrapper: true}] = true
			}
		}
	}
	return r
}

// equal reports whether l and o enter the same rules in the same ways.
func (l *tsLive) equal(o *tsLive) bool {
	if len(l.reach) != len(o.reach) {
		return false
	}
	for table, r := range l.reach {
		if !slices.Equal(r, o.reach[table]) {
			return false
		}
	}
	return true
}

// callsPlain reports whether a call of the rule r is printed as a call of its plain function
// (tsPrimPrinter.call) rather than through its constant. A parser with a Document calls every rule
// through its constant, since a Document parse memoizes plain rules too.
func callsPlain(r *common.Rule, document bool) bool {
	_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
	return !document && r.Plain && !operatorsRule
}

// pruned reports whether the rule r of the current table is left out.
func (g *Generator) pruned(r *common.Rule) bool {
	return g.live != nil && g.live.reach[g.Table][r.ID] == 0
}

// slotReached reports whether something enters the rule r through its constant (and so its body
// assignment), and not only through its plain function.
func (g *Generator) slotReached(r *common.Rule) bool {
	return g.live == nil || g.live.reach[g.Table][r.ID]&core.EnterSlot != 0
}

// tableRow reports whether the constant of the rule r is declared: something reads it, which is
// the entry through its slot, or the plain function of a rule with a value (it finishes the value
// with the rule). A rule without a value that only its plain function enters needs no constant.
func (g *Generator) tableRow(r *common.Rule) bool {
	return g.slotReached(r) || !r.Novalue
}

// checkCall checks that the callee of a printed call was not left out.
func (g *Generator) checkCall(r *common.Rule) {
	if g.Cur != nil {
		calls := g.printed[g.Table]
		if calls == nil {
			calls = map[int][]int{}
			g.printed[g.Table] = calls
		}
		if !slices.Contains(calls[g.Cur.ID], r.ID) {
			calls[g.Cur.ID] = append(calls[g.Cur.ID], r.ID)
		}
	}
	if g.pruned(r) && g.reachErr == nil {
		g.reachErr = fmt.Errorf("internal error: a generated call reaches the rule %s of %s, whose body is left out", r.Name, g.Table)
	}
}
