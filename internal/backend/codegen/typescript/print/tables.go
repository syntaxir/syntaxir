package print

import (
	"fmt"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tables of a TypeScript parser are built (TSTarget.Sources), planned with the program
// (TSTarget.Cores, flowplan.Planning), lowered to the Exec stage by the pipeline of the compilation
// and printed from it (Printing).

// FlowTarget is the forms of the Flow passes that TypeScript prints: none of the forms that only Go
// prints.
var FlowTarget = flowpass.Target{}

// TableLabels are the names of the tables that a TypeScript parser can have, in the order in which
// they are built: the Node table and the recognizer.
var TableLabels = []string{"rules", "recRules"}

// TSTarget is the TypeScript target of a compilation (common.Target), which print-ts reads: the
// program, the build of its recognizer, the start rule, the options of the target, which Validate
// checked, and the settings of the compilation.
type TSTarget struct {
	Prog     *common.Program
	Start    *common.Rule
	Opts     common.GenOptions
	Settings pass.Settings
	// rec returns the recognizer, built on the first call, or nil if the parser has none.
	rec func() (*common.Program, error)
}

// NewTSTarget returns the TypeScript target of the program prog with the build of its recognizer, the
// start rule start, the options opts, which Validate checked, and the settings of the compilation.
func NewTSTarget(prog *common.Program, recognizer func() (*common.Program, error), start *common.Rule, opts common.GenOptions, settings pass.Settings) *TSTarget {
	t := &TSTarget{Prog: prog, Start: start, Opts: opts, Settings: settings}
	t.rec = sync.OnceValues(func() (*common.Program, error) {
		if !opts.Recognize {
			return nil, nil
		}
		return recognizer()
	})
	return t
}

// Backend names the backend of the target.
func (*TSTarget) Backend() string { return "ts" }

// Sources returns the builds of the Core IR of the tables of the parser and their labels: the Node
// table and, with Recognize, the recognizer.
func (t *TSTarget) Sources() ([]common.TableSource, []string, error) {
	rec, err := t.rec()
	if err != nil {
		return nil, nil, err
	}
	s := []common.TableSource{{Build: flowplan.TableBuild(t.Prog.Elab, plandata.TableConfig{Values: true}), Key: "program"}}
	if rec != nil {
		s = append(s, common.TableSource{Build: flowplan.TableBuild(rec.Elab, plandata.TableConfig{}), Key: "recognizer"})
	}
	return s, TableLabels[:len(s)], nil
}

// Cores returns what the program is planned from (flowplan.Planning), given the Core modules mods of
// the tables in the order of Sources: the Node table and the recognizer, if the parser has one; the
// entry points: every start rule of the Node table, which parseRule can name, and the start rule
// that parse and recognize run; and the tables as the units that the planning plans, each leaving
// out the rules that the plan does not reach. A parser with a Document enters every rule through
// its slot (plandata.CoreTable.Slots), since a Document parse memoizes plain rules too. The
// expectation table is numbered as the rules are printed, so no unit is numbered.
func (t *TSTarget) Cores(mods []*core.Module) (*plandata.Cores, error) {
	rec, err := t.rec()
	if err != nil {
		return nil, err
	}
	o := t.Opts
	c := &plandata.Cores{}
	for _, name := range t.Prog.StartRules {
		c.Roots = append(c.Roots, plandata.Entry{Table: "rules", ID: t.Prog.ByName[name].ID})
	}
	c.Roots = append(c.Roots, plandata.Entry{Table: "rules", ID: t.Start.ID})
	if rec != nil {
		c.Roots = append(c.Roots, plandata.Entry{Table: "recRules", ID: rec.ByName[t.Start.Name].ID})
	}
	for i, pr := range []*common.Program{t.Prog, rec}[:len(mods)] {
		table := TableLabels[i]
		all := append(append([]*common.Rule(nil), pr.RuleList...), pr.Twins...)
		ct := plandata.CoreTable{Name: table, M: mods[i], Slots: o.Document}
		// A parser with Documents resumes the runs of its repetitions (lowerdata.Options.Resume).
		cfg := plandata.TableConfig{Values: pr == t.Prog, Budget: !o.NoLimitControls, Observe: o.Observability, Document: o.Document, Resume: o.Document, Outline: true}
		u := plandata.Unit{Label: table, Table: table, Rules: make([]*plandata.Rule, len(all)), Prune: true, IR: flowplan.TableOf(mods[i], cfg)}
		for j, r := range all {
			_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
			ct.Rules = append(ct.Rules, plandata.CallRule{Plain: r.Plain, Leader: r.Leader, Operators: operatorsRule})
			u.Rules[j] = r.PlanRule()
		}
		c.Tables = append(c.Tables, ct)
		c.Units = append(c.Units, u)
	}
	return c, nil
}

// Printing is the step print-ts: it prints the files of the TypeScript parser of the target of the
// pipeline (common.TargetFeature) from its tables at the Exec stage. If the plan computed its reach
// (plandata.ReachFeature) and the printed code enters other rules than it reaches, the step returns
// the reach of the printed code (common.Replan) instead of the files, unless a bisection limit is
// set (common.Debug.OptBisectLimit), under which one run numbers the runs of the optional passes.
var Printing = pipe.New(pipe.Def[*common.Tables, result.Output]{
	Decl: pipe.Decl{Name: "print-ts", Required: true, Obligation: "The files are the TypeScript parser of the tables.",
		Features: []pipe.FeatureID{common.TargetFeature.ID(), plandata.ReachFeature.ID()}},
	In: common.TablesIR, Out: result.IR,
	Run: func(c *pipe.Ctx[*common.Tables], t *common.Tables) (result.Output, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, common.TargetFeature).(*TSTarget)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: print-ts runs for the target %s", pipe.Feature(c, common.TargetFeature).Backend())
		}
		out, err := target.print(t, pipe.Feature(c, plandata.ReachFeature) != nil)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return out, pass.PreserveNone(), nil
	},
})

// print prints the parser of the target from its tables t at the Exec stage, or returns the reach of
// the printed code (common.Replan) if it differs from that of the plan, unless the compilation gave
// the plan its reach (given) or a bisection limit is set.
func (t *TSTarget) print(tables *common.Tables, given bool) (result.Output, error) {
	rec, err := t.rec()
	if err != nil {
		return nil, err
	}
	live := liveOf(tables.Plan)
	tsg := newGenerator(t.Prog, t.Opts, live)
	tsg.Settings, tsg.tables = t.Settings, tables
	if err := tsg.generate(rec); err != nil {
		return nil, err
	}
	if printed := tsg.printedReach(t.Prog, rec, t.Start); !given && t.Opts.Debug.OptBisectLimit == nil && !printed.equal(live) {
		return &common.Replan{Lang: result.TypeScript, Reach: printed.entries()}, nil
	}
	files, err := tsg.print(t.Opts, rec, t.Start)
	if err != nil {
		return nil, err
	}
	return &result.Files{Lang: result.TypeScript, Files: files}, nil
}
