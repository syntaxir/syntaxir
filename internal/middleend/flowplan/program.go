package flowplan

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PlanName is the name of the step that plans a program (Planning).
const PlanName = "plan"

// Planning is the step that plans a program from the Core IR of its tables (PlanProgram). Its facts
// are those of the whole program, which no table decides alone: a call in one table's rule reaches
// rules that only the program's entry points make reachable.
var Planning = pipe.New(pipe.Def[*plandata.Cores, *plandata.Program]{
	Decl: pipe.Decl{Name: PlanName, Required: true, Obligation: "The plan holds the route of every call and the rules that the entry points of the program reach."},
	In:   plandata.CoresIR, Out: plandata.ProgramIR,
	Run: func(_ *pipe.Ctx[*plandata.Cores], c *plandata.Cores) (*plandata.Program, pass.PreservedSet, error) {
		// The manager verifies what a step makes, so the step checks what it is given.
		if err := plandata.CoresIR.Verify(c); err != nil {
			return nil, pass.PreserveNone(), err
		}
		return PlanProgram(c), pass.PreserveNone(), nil
	},
})

// PlanProgram plans the program c: the route of a call of each rule (CallsWrapper) and the rules that
// the entry points of c reach through those routes (Reach).
func PlanProgram(c *plandata.Cores) *plandata.Program {
	// An entry point that the program enters in several ways (a typed start rule that several entry
	// points run) is one root of the plan.
	p := &plandata.Program{}
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
	p.Reach = Reach(c, p, p.Roots)
	return p
}

// CallsWrapper reports whether a call of the rule r of the table t enters the rule's wrapper, a
// function of its own that does not run the rule's slot: a plain rule, or in the typed table a rule
// that is not a left-recursion leader, unless the rule is an Operators Rule, whose levels the slot
// runs, or every call of the table enters the slot (CoreTable.Slots).
func CallsWrapper(t plandata.CoreTable, r plandata.CallRule) bool {
	return !t.Slots && !r.Operators && (r.Plain || t.Typed && !r.Leader)
}

// Reach returns the entries that roots reach in the tables of c, following the calls of the Core IR of
// each table (core.Module.Reach) through the routes of the plan p.
func Reach(c *plandata.Cores, p *plandata.Program, roots []plandata.Entry) map[plandata.Entry]bool {
	reach := map[plandata.Entry]bool{}
	for _, t := range c.Tables {
		var rs []core.Root
		for _, k := range roots {
			if k.Table == t.Name {
				e := core.EnterSlot
				if k.Wrapper {
					e = core.EnterWrapper
				}
				rs = append(rs, core.Root{Rule: k.ID, Entry: e})
			}
		}
		entries := t.M.Reach(rs, func(id int) core.Entry {
			if p.Wrapper(t.Name, id) {
				return core.EnterWrapper
			}
			return core.EnterSlot
		})
		for id, e := range entries {
			if e&core.EnterSlot != 0 {
				reach[plandata.Entry{Table: t.Name, ID: id}] = true
			}
			if e&core.EnterWrapper != 0 {
				reach[plandata.Entry{Table: t.Name, ID: id, Wrapper: true}] = true
			}
		}
	}
	return reach
}
