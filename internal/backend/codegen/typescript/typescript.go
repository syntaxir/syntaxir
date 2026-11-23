// Package typescript is the TypeScript backend of a compilation: it builds the tables of a TypeScript
// parser, gives the planning what they are planned from, and declares the step print-ts, which prints
// the parser with the TypeScript printer (package print) from its tables at the Exec stage. The
// printer runs no step: it receives the parser and the tables that the pipeline lowered.
package typescript

import (
	"fmt"
	"slices"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tables of a TypeScript parser are built (Target.Sources), planned with the program
// (Target.Cores, flowplan.Planning), lowered to the Exec stage by the pipeline of the compilation
// and printed from it (Printing).

// FlowTarget is the forms of the Flow passes that TypeScript prints: none of the forms that only Go
// prints.
var FlowTarget = flowpass.Target{}

// TableLabels are the names of the tables that a TypeScript parser can have, in the order in which
// they are built: the Node table and the recognizer.
var TableLabels = []string{"rules", "recRules"}

// Target is the TypeScript target of a compilation (common.Target), which print-ts reads: the
// program, the build of its recognizer, the start rule, the options of the target, which
// print.Validate checked, and the hooks of a test.
type Target struct {
	Prog  codegen.Checked
	Start *common.Rule
	Opts  common.GenOptions
	// rec returns the recognizer, built on the first call, or nil if the parser has none.
	rec func() (*codegen.Checked, error)
	// hooks are the hooks of a test, or nil.
	hooks *print.Hooks
}

// NewTarget returns the TypeScript target of the program prog with the build of its recognizer, the
// start rule start, the options opts, which print.Validate checked, and the hooks of a test, which
// production leaves nil.
func NewTarget(prog codegen.Checked, recognizer func() (codegen.Checked, error), start *common.Rule, opts common.GenOptions, hooks *print.Hooks) *Target {
	t := &Target{Prog: prog, Start: start, Opts: opts, hooks: hooks}
	t.rec = sync.OnceValues(func() (*codegen.Checked, error) {
		if !opts.Recognize || opts.RecognizeOnly {
			// A recognize-only parser is the recognizer itself, the program it was given.
			return nil, nil
		}
		rec, err := recognizer()
		if err != nil {
			return nil, err
		}
		return &rec, nil
	})
	return t
}

// Backend names the backend of the target.
func (*Target) Backend() string { return "ts" }

// Sources returns the builds of the Core IR of the tables of the parser and their labels: the Node
// table and, with Recognize, the recognizer.
func (t *Target) Sources() ([]common.TableSource, []string, error) {
	rec, err := t.rec()
	if err != nil {
		return nil, nil, err
	}
	s := []common.TableSource{{Build: flowplan.TableBuild(t.Prog.Elab, plandata.TableConfig{Values: !t.Opts.RecognizeOnly}), Key: "program", Origin: true}}
	if rec != nil {
		s = append(s, common.TableSource{Build: flowplan.TableBuild(rec.Elab, plandata.TableConfig{}), Key: "recognizer", Origin: true})
	}
	return s, TableLabels[:len(s)], nil
}

// Cores returns what the program is planned from (flowplan.Planning), given the Core modules mods of
// the tables in the order of Sources: the Node table and the recognizer, if the parser has one; the
// entry points: every start rule of the Node table, which parseRule and parseRuleValue can name
// (none for a recognize-only parser), and the start rule that parse, parseValue and recognize run;
// and the tables as the units that the planning plans, each leaving
// out the rules that the plan does not reach. A parser with a Document enters every rule through
// its slot (plandata.CoreTable.Slots), since a Document parse memoizes plain rules too. The
// expectation table is numbered as the rules are printed, so no unit is numbered.
func (t *Target) Cores(mods []*core.Module) (*plandata.Cores, error) {
	rec, err := t.rec()
	if err != nil {
		return nil, err
	}
	o := t.Opts
	c := &plandata.Cores{}
	prog := t.Prog.View
	// parseRule and parseRuleValue enter the other start rules, so only a recognize-only parser has no such roots.
	if !o.RecognizeOnly {
		for _, name := range prog.StartRules {
			c.Roots = append(c.Roots, plandata.Entry{Table: "rules", ID: prog.ByName[name].ID})
		}
	}
	c.Roots = append(c.Roots, plandata.Entry{Table: "rules", ID: t.Start.ID})
	if rec != nil {
		c.Roots = append(c.Roots, plandata.Entry{Table: "recRules", ID: rec.View.ByName[t.Start.Name].ID})
	}
	progs := []*common.Program{prog}
	if rec != nil {
		progs = append(progs, rec.View)
	}
	for i, pr := range progs[:len(mods)] {
		table := TableLabels[i]
		all := append(append([]*common.Rule(nil), pr.RuleList...), pr.Twins...)
		ct := plandata.CoreTable{Name: table, M: mods[i], Slots: o.Document}
		// A parser with limit controls counts logical compute units, with the exact guards
		// (lowerdata.GuardsExact); a parser with Documents resumes the runs of its repetitions
		// (lowerdata.Options.Resume).
		cfg := plandata.TableConfig{Values: pr == prog && !o.RecognizeOnly, Logical: !o.NoLimitControls, Observe: o.Observability, Document: o.Document, Resume: o.Document, Inserts: true}
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
// pipeline (codegen.TargetFeature) from its tables at the Exec stage, with the TypeScript printer. If the plan computed its reach
// (plandata.ReachFeature) and the printed code enters other rules than it reaches, the step returns
// the reach of the printed code (common.Replan) instead of the files, unless a bisection limit is
// set (common.Debug.OptBisectLimit), under which one run numbers the runs of the optional passes.
var Printing = pipe.New(pipe.Def[*execdata.Tables, result.Output]{
	Decl: pipe.Decl{Name: "print-ts", Required: true, Obligation: "The files are the TypeScript parser of the tables.",
		Features: []pipe.FeatureID{codegen.TargetFeature.ID(), plandata.ReachFeature.ID()},
		Params:   append(slices.Clone(codegen.Params), TSLiteralSharing, SparseMemo)},
	In: execdata.TablesIR, Out: result.IR,
	Run: func(c *pipe.Ctx[*execdata.Tables], t *execdata.Tables) (result.Output, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, codegen.TargetFeature).(*Target)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: print-ts runs for the target %s", pipe.Feature(c, codegen.TargetFeature).Backend())
		}
		rec, err := target.rec()
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		p := &print.Parser{Prog: target.Prog.View, Start: target.Start, Opts: target.Opts}
		if rec != nil {
			p.Rec = rec.View
		}
		params := print.Params{Params: codegen.ParamsOf(c.Value), SparseMemo: c.Value(SparseMemo) != 0}
		// A TypeScript parser shares its literal tables only with ts-literal-sharing too.
		params.LiteralSharing = params.LiteralSharing && c.Value(TSLiteralSharing) != 0
		out, err := print.Print(p, params, t, pipe.Feature(c, plandata.ReachFeature) != nil, target.hooks)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return out, pass.PreserveNone(), nil
	},
})
