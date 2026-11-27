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
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
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
// they are built: the Node table and the recognizer, the tables of the insertion program and of its
// recognizer, and the tables of the stream, which are the Node table and the insertion table lowered
// for a suspended stream (print.StreamTable, print.InsStreamTable).
var TableLabels = []string{"rules", "recRules", "insRules", "insRecRules", print.StreamTable, print.InsStreamTable}

// Target is the TypeScript target of a compilation (common.Target), which print-ts reads: the
// program, the builds of the programs derived from it that the parser has, the start rule, the
// options of the target, which print.Validate checked, and the hooks of a test.
type Target struct {
	Prog  codegen.Checked
	Start *common.Rule
	Opts  common.GenOptions
	// rec, ins and insRec return the recognizer, the insertion program and the recognizer of the
	// insertion program, each built on the first call, or nil if the parser has none.
	rec, ins, insRec func() (*codegen.Checked, error)
	// hooks are the hooks of a test, or nil.
	hooks *print.Hooks
}

// NewTarget returns the TypeScript target of the program prog with the builds of the programs derived
// from it, of which it reads the recognizer, the start rule start, the options opts, which
// print.Validate checked, and the hooks of a test, which production leaves nil.
func NewTarget(prog codegen.Checked, derived codegen.Derived, start *common.Rule, opts common.GenOptions, hooks *print.Hooks) *Target {
	if hooks != nil && hooks.Stream {
		opts.Stream = true
	}
	t := &Target{Prog: prog, Start: start, Opts: opts, hooks: hooks}
	build := func(has bool, derive func() (codegen.Checked, error)) func() (*codegen.Checked, error) {
		return sync.OnceValues(func() (*codegen.Checked, error) {
			if !has {
				return nil, nil
			}
			p, err := derive()
			if err != nil {
				return nil, err
			}
			return &p, nil
		})
	}
	// A recognize-only parser is the recognizer itself, the program it was given.
	t.rec = build(opts.Recognize && !opts.RecognizeOnly, derived.Recognizer)
	// The parses with missing-token insertion enabled run the insertion program, as on every backend:
	// its tables lower the #missing sites with their insertions and keep no guard, so that the tables of
	// the parses without insertion stay as they are without #missing. A recognize-only parser, which
	// has no parse, needs only the recognizer of the insertion program.
	missing := prog.View != nil && prog.View.Run.HasMissing
	t.ins = build(missing && !opts.RecognizeOnly, derived.Insertion)
	t.insRec = build(missing && opts.Recognize, derived.InsertionRecognizer)
	return t
}

// parser returns the parser of the target as the TypeScript printer reads it: the program, the
// programs derived from it that the parser has, the start rule and the options.
func (t *Target) parser() (*print.Parser, error) {
	p := &print.Parser{Prog: t.Prog.View, Start: t.Start, Opts: t.Opts}
	for _, d := range []struct {
		build func() (*codegen.Checked, error)
		view  **common.Program
	}{{t.rec, &p.Rec}, {t.ins, &p.Ins}, {t.insRec, &p.InsRec}} {
		c, err := d.build()
		if err != nil {
			return nil, err
		}
		if c != nil {
			*d.view = c.View
		}
	}
	return p, nil
}

// tables returns the rule tables of the parser that the target builds, in the order of TableLabels,
// with the checked program of each: the Node table, the recognizer, the insertion table and the
// recognizer of the insertion program, as far as the parser has them.
func (t *Target) tables() ([]string, []codegen.Checked, error) {
	labels, progs := []string{"rules"}, []codegen.Checked{t.Prog}
	for i, build := range []func() (*codegen.Checked, error){t.rec, t.ins, t.insRec} {
		c, err := build()
		if err != nil {
			return nil, nil, err
		}
		if c != nil {
			labels, progs = append(labels, TableLabels[i+1]), append(progs, *c)
		}
	}
	return labels, progs, nil
}

// Backend names the backend of the target.
func (*Target) Backend() string { return "ts" }

// Sources returns the builds of the Core IR of the tables of the parser and their labels: the Node
// table and, with Recognize, the recognizer; and for a grammar with #missing the tables of the
// insertion program and of its recognizer, each built from its own program, which the key of the
// build says.
func (t *Target) Sources() ([]common.TableSource, []string, error) {
	labels, progs, err := t.tables()
	if err != nil {
		return nil, nil, err
	}
	s := make([]common.TableSource, len(labels))
	for i, table := range labels {
		switch table {
		case "rules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(progs[i].Elab, plandata.TableConfig{Values: !t.Opts.RecognizeOnly}), Key: "program", Origin: true}
		case "recRules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(progs[i].Elab, plandata.TableConfig{}), Key: "recognizer", Origin: true}
		case "insRules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(progs[i].Elab, plandata.TableConfig{Values: true}), Key: "insertion", Origin: true}
		case "insRecRules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(progs[i].Elab, plandata.TableConfig{}), Key: "insertion-recognizer", Origin: true}
		}
	}
	return s, labels, nil
}

// Cores returns what the program is planned from (flowplan.Planning), given the Core modules mods of
// the tables in the order of Sources: the Node table, the recognizer and the tables of the insertion
// program and of its recognizer, as far as the parser has them; the entry points: every start rule of
// the Node table and of the insertion table, which parseRule and parseRuleValue can name (none for a
// recognize-only parser), and the start rule that parse, parseValue and recognize run, in each table;
// and the tables as the units that the planning plans, each leaving out the rules that the plan does
// not reach. A parser with a Document enters every rule of the Node table and of the insertion table
// through its slot (plandata.CoreTable.Slots), since a Document parse memoizes plain rules too. The
// expectation table is numbered as the rules are printed, so no unit is numbered.
func (t *Target) Cores(mods []*core.Module) (*plandata.Cores, error) {
	labels, progs, err := t.tables()
	if err != nil {
		return nil, err
	}
	o := t.Opts
	if o.Stream && !t.Start.Stream {
		return nil, fmt.Errorf("stream generation needs a #stream repetition at the top level of the start rule %s", t.Start.Name)
	}
	c := &plandata.Cores{}
	for i, table := range labels {
		pr := progs[i].View
		// parseRule and parseRuleValue enter the other start rules, so only a recognize-only parser has
		// no such roots; the insertion table is entered where the Node table is (Generator.insertionLinks).
		if !o.RecognizeOnly && (table == "rules" || table == "insRules") {
			for _, name := range pr.StartRules {
				c.Roots = append(c.Roots, plandata.Entry{Table: table, ID: pr.ByName[name].ID})
			}
		}
		c.Roots = append(c.Roots, plandata.Entry{Table: table, ID: pr.ByName[t.Start.Name].ID})
	}
	var streams []plandata.Unit
	for i, table := range labels[:len(mods)] {
		pr := progs[i].View
		inserts := table == "insRules" || table == "insRecRules"
		all := append(append([]*common.Rule(nil), pr.RuleList...), pr.Twins...)
		ct := plandata.CoreTable{Name: table, M: mods[i], Slots: o.Document && (table == "rules" || table == "insRules")}
		// A parser with limit controls counts logical compute units, with the exact guards
		// (lowerdata.GuardsExact); a parser with Documents resumes the runs of its repetitions
		// (lowerdata.Options.Resume). The tables of the insertion program, which only parses inserting
		// missing tokens run, lower the #missing sites with their insertions and keep no guard
		// (lowerdata.GuardsNone), as the insertion modules of the VMs do; the other tables lower them
		// as their literals, so that the parses without insertion pay nothing for them.
		cfg := plandata.TableConfig{Values: table == "rules" && !o.RecognizeOnly || table == "insRules", Logical: !o.NoLimitControls, Observe: o.Observability,
			Document: o.Document, Resume: o.Document, Inserts: inserts}
		if inserts {
			cfg.Guards = lowerdata.GuardsNone
		}
		u := plandata.Unit{Label: table, Table: table, Rules: make([]*plandata.Rule, len(all)), Prune: true, IR: flowplan.TableOf(mods[i], cfg)}
		for j, r := range all {
			_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
			ct.Rules = append(ct.Rules, plandata.CallRule{Plain: r.Plain, Leader: r.Leader, Operators: operatorsRule})
			u.Rules[j] = r.PlanRule()
		}
		c.Tables = append(c.Tables, ct)
		c.Units = append(c.Units, u)
		if o.Stream && (table == "rules" || table == "insRules") {
			// The stream parses with the Node table lowered for a suspended stream, whose functions
			// demand their input, and a stream parse with missing-token insertion enabled with the
			// insertion table lowered so; neither numbers an expectation, as the batch tables number
			// them all. A stream never parses a Document, so its table is lowered without the examined
			// ends and the repetition runs of a Document, which the batch table of a package with both
			// has.
			cfg.Suspend = true
			cfg.Document, cfg.Resume = false, false
			s := u
			s.Label, s.IR = print.StreamTable, flowplan.TableOf(mods[i], cfg)
			if table == "insRules" {
				s.Label = print.InsStreamTable
			}
			// The body of the start rule that the stream runs delivers the elements of its #stream
			// repetition (plandata.StreamBody).
			s.Streams = []*plandata.Rule{pr.ByName[t.Start.Name].PlanRule()}
			streams = append(streams, s)
		}
	}
	c.Units = append(c.Units, streams...)
	return c, nil
}

// Printing is the step print-ts: it prints the files of the TypeScript parser of the target of the
// pipeline (codegen.TargetFeature) from its tables at the Exec stage, with the TypeScript printer. If the plan computed its reach
// (plandata.ReachFeature) and the printed code enters other rules than it reaches, the step returns
// the reach of the printed code (common.Replan) instead of the files, unless a bisection limit is
// set (common.Debug.OptBisectLimit), under which one run numbers the runs of the optional passes.
// The parameters of the printer (common.Params) and sparse-memo are patterns of the step too, whose
// decisions a bisection limit can skip (pipe.Ctx.Allow): the sites of the printer's parameters, with
// those of ts-literal-sharing decided as literal-sharing, and sparse-memo, if on, decided once.
// used-features is decided by ts-library, except the shaking of the embedded library.
var Printing = pipe.New(pipe.Def[*execdata.Tables, result.Output]{
	Decl: pipe.Decl{Name: "print-ts", Required: true, Obligation: "The files are the TypeScript parser of the tables.",
		Features: []pipe.FeatureID{codegen.TargetFeature.ID(), plandata.ReachFeature.ID()},
		Params:   append(slices.Clone(codegen.Params), TSLiteralSharing, SparseMemo),
		Patterns: append(slices.Clone(codegen.Params), SparseMemo)},
	In: execdata.TablesIR, Out: result.IR,
	Run: func(c *pipe.Ctx[*execdata.Tables], t *execdata.Tables) (result.Output, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, codegen.TargetFeature).(*Target)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: print-ts runs for the target %s", pipe.Feature(c, codegen.TargetFeature).Backend())
		}
		p, err := target.parser()
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		params := print.Params{Params: codegen.ParamsOf(c.Value, c.Allow), SparseMemo: c.Value(SparseMemo) != 0 && c.Allow(SparseMemo, nil), Control: cancel.New(c.Context())}
		// A TypeScript parser shares its literal tables only with ts-literal-sharing too.
		params.LiteralSharing = params.LiteralSharing && c.Value(TSLiteralSharing) != 0
		out, err := print.Print(p, params, t, pipe.Feature(c, plandata.ReachFeature) != nil, target.hooks)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return out, pass.PreserveNone(), nil
	},
})
