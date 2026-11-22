package flowplan

import (
	"slices"

	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// PlanName is the name of the step that plans a program (Planning).
const PlanName = "plan"

// Planning is the step that plans a program from the Core IR of its tables (planProgram), with the
// reach that the compilation gives, if any (plandata.ReachFeature). Its facts are those of the whole
// program, which no table decides alone: a call in one table's rule reaches rules that only the
// program's entry points make reachable.
var Planning = pipe.New(pipe.Def[*plandata.Cores, *plandata.Program]{
	Decl: pipe.Decl{Name: PlanName, Required: true, Obligation: "The plan holds the route of every call and the rules that the entry points of the program reach.",
		Features: []pipe.FeatureID{plandata.ReachFeature.ID()}},
	In: plandata.CoresIR, Out: plandata.ProgramIR,
	Run: func(x *pipe.Ctx[*plandata.Cores], c *plandata.Cores) (*plandata.Program, pass.PreservedSet, error) {
		// The manager verifies what a step makes, so the step checks what it is given.
		if err := plandata.CoresIR.Verify(c); err != nil {
			return nil, pass.PreserveNone(), err
		}
		return planProgram(c, pipe.Feature(x, plandata.ReachFeature)), pass.PreserveNone(), nil
	},
})

// planProgram plans the program c: the route of a call of each rule (CallsWrapper), the rules that
// the entry points of c reach through those routes (Reach), or reach if it is not nil, and the tables
// of its units (planUnits).
func planProgram(c *plandata.Cores, reach map[plandata.Entry]bool) *plandata.Program {
	// An entry point that the program enters in several ways (a typed start rule that several entry
	// points run) is one root of the plan.
	p := &plandata.Program{Cores: c}
	for _, e := range c.Roots {
		if !slices.Contains(p.Roots, e) {
			p.Roots = append(p.Roots, e)
		}
	}
	for _, t := range c.Tables {
		r := plandata.Routes{Table: t.Name, Names: make([]string, len(t.Rules)), Wrappers: make([]bool, len(t.Rules))}
		for id, cr := range t.Rules {
			r.Names[id] = t.M.Rules[id].Name
			r.Wrappers[id] = CallsWrapper(t, cr)
		}
		p.Tables = append(p.Tables, r)
	}
	p.Reach = reach
	if p.Reach == nil {
		p.Reach = p.Reached(p.Roots)
	}
	planUnits(c, p)
	return p
}

// UnreachableSuffix ends the label of the unreachable part of a numbered unit.
const UnreachableSuffix = "Unreachable"

// planUnits plans the table of each unit of c with the plan p of the program and adds it to p.Units.
// A unit that prunes leaves out the rules that p reaches by neither their slot nor their wrapper. The
// expectation table is numbered from each numbered unit planned with every rule (Program.Numbered):
// when the plan prunes the unit, the functions that it leaves out are planned as a part of their own,
// labeled with UnreachableSuffix and lowered like the unit, so that each function of the table is
// lowered once, and the printed tables keep the rows and the expectations of every rule.
func planUnits(c *plandata.Cores, p *plandata.Program) {
	layouts := map[string][][]string{}
	var unreachable []plandata.PlannedUnit
	for _, u := range c.Units {
		keep := func(id int) bool {
			if _, ops := u.Rules[id].Body.(*ast.OperatorsRule); ops && u.NoOperators {
				return false
			}
			return true
		}
		live := func(id int) bool {
			return keep(id) && (!u.Prune || p.Reach[plandata.Entry{Table: u.Table, ID: id}] || p.Reach[plandata.Entry{Table: u.Table, ID: id, Wrapper: true}])
		}
		plan := func(live func(int) bool) *plandata.Plan {
			if u.Typed {
				frames := layouts[u.Frames]
				return PlanTyped(u.IR, u.Rules, live, func(id int) []string { return frames[id] })
			}
			t := Plan(u.IR, u.Rules, live)
			for _, r := range u.Streams {
				Stream(t, u.IR, r)
			}
			return t
		}
		t := plan(live)
		if _, seen := layouts[u.Label]; !seen {
			layouts[u.Label] = t.Layouts
		}
		p.Units = append(p.Units, plandata.PlannedUnit{Label: u.Label, Planned: Unit(t, u.IR, u.Refuse), Printed: true})
		if !u.Numbered {
			continue
		}
		n := plandata.Numbered{Full: t, Parts: []int{len(p.Units) - 1}}
		if u.Prune {
			n.Full = plan(keep)
			if rest := n.Full.Excluding(t); len(rest.Funcs) > 0 {
				unreachable = append(unreachable, plandata.PlannedUnit{Label: u.Label + UnreachableSuffix, Planned: Unit(rest, u.IR, u.Refuse)})
				n.Parts = append(n.Parts, len(c.Units)+len(unreachable)-1)
			}
		}
		p.Numbered = append(p.Numbered, n)
	}
	p.Units = append(p.Units, unreachable...)
}

// CallsWrapper reports whether a call of the rule r of the table t enters the rule's wrapper, a
// function of its own that does not run the rule's slot: a plain rule, or in the typed table a rule
// that is not a left-recursion leader, unless the rule is an Operators Rule, whose levels the slot
// runs, or every call of the table enters the slot (CoreTable.Slots).
func CallsWrapper(t plandata.CoreTable, r plandata.CallRule) bool {
	return !t.Slots && !r.Operators && (r.Plain || t.Typed && !r.Leader)
}
