// Package golang is the Go backend of a compilation: it decides the tables of a Go parser, gives the
// planning what they are planned from, and declares the step print-go, which prints the parser with
// the Go printer (package print) from its tables at the Exec stage. The printer runs no step: it
// receives the parser that this package decided and the tables that the pipeline lowered.
package golang

import (
	"fmt"
	"slices"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tables of a Go parser are decided before any of them is built (Decide), built (Decision.Sources)
// and planned with the program (Decision.Cores, flowplan.Planning), lowered to the Exec stage by the
// pipeline of the compilation and printed from it (Printing).

// FlowTarget is the forms of the Flow passes that Go prints: every one.
var FlowTarget = flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}

// TableLabels are the names of the tables that a Go parser can have, in the order in which they are
// built: the Node table, the recognizer, the typed table, the Node table and the recognizer of the
// insertion program, and their traced copies.
var TableLabels = []string{"rules", "recRules", "typed", "insRules", "insRecRules", "rulesTraced", "recRulesTraced", "typedTraced", "insRulesTraced", "insRecRulesTraced"}

// Target is the Go target of a compilation (common.Target), which print-go reads: the program, the
// builds of the programs derived from it, the start rule and the options of the target, which
// print.Validate completed. The tables of its parser are decided once from them (Decision).
type Target struct {
	Prog    codegen.Checked
	Derived codegen.Derived
	Start   *common.Rule
	Opts    common.GenOptions
	// hooks are the hooks of a test, or nil (print.Hooks).
	hooks  *print.Hooks
	decide func() (*Decision, error)
}

// NewTarget returns the Go target of the program prog with the builds of the programs derived from it,
// the start rule start, the options opts, which print.Validate completed, and the hooks of a test,
// which production leaves nil.
func NewTarget(prog codegen.Checked, derived codegen.Derived, start *common.Rule, opts common.GenOptions, hooks *print.Hooks) *Target {
	t := &Target{Prog: prog, Derived: derived, Start: start, Opts: opts, hooks: hooks}
	t.decide = sync.OnceValues(func() (*Decision, error) {
		d, err := Decide(prog, derived, start, opts)
		if err == nil && hooks != nil {
			d.refuse = hooks.RefuseRecRule
		}
		return d, err
	})
	return t
}

// Backend names the backend of the target.
func (*Target) Backend() string { return "go" }

// Decision returns the decision of the parser of the target, which the first call makes (Decide).
func (t *Target) Decision() (*Decision, error) { return t.decide() }

// tableSpec is one table of a Go parser: its name in the dumps (print.Generator.IRTable), the rule table it
// fills (rules, recRules, trules, insRules or insRecRules), the program of its rules, the input of the
// elaboration of the program, and whether it is a traced copy.
type tableSpec struct {
	name, table string
	prog        *common.Program
	elab        elaborate.Program
	traced      bool
}

// A Decision is what a Go parser of a program is made of, decided from the program and the options
// before any table is built: the options as the program completes them, the recognizer, the
// insertion program and its recognizer, the typed entry points and the tables.
type Decision struct {
	print.Parser
	// roots are the typed rules that the typed entry points enter.
	roots []plandata.Entry
	specs []tableSpec
	// refuse is a test's hook (hooks.RefuseRecRule).
	refuse func(*common.Rule) bool
	// guards is what the guards of choices do under logical units (common.GenOptions.LogicalVariant).
	guards lowerdata.LogicalGuards
}

// Decide decides the Go parser of prog that starts at start with the options opts, which Validate
// completed: the typed entry points, and with them whether the typed table is built and whether a
// typed-only parser keeps its Node rules (common.GenOptions.KeepNodes), and the derived programs that
// derived checks: the recognizer, and for a grammar that uses #missing the insertion program, whose
// tables the parses with missing-token insertion enabled run, with its recognizer.
func Decide(prog codegen.Checked, derived codegen.Derived, start *common.Rule, opts common.GenOptions) (*Decision, error) {
	if opts.Stream && !start.Stream {
		return nil, fmt.Errorf("stream generation needs a #stream repetition at the top level of the start rule %s", start.Name)
	}
	// Generated typed values do not represent the missing type.
	if prog.View.Run.HasMissing && opts.Types {
		return nil, rprogram.MissingUnsupported("generated typed Go parsers")
	}
	d := &Decision{Parser: print.Parser{Opts: opts, Prog: prog.View, Start: start}}
	if opts.LogicalVariant != "" && !opts.LogicalUnits() {
		return nil, fmt.Errorf("the variant %q of logical units needs limit controls", opts.LogicalVariant)
	}
	switch opts.LogicalVariant {
	case "":
	case "keep-guards":
		d.guards = lowerdata.GuardsKeep
	case "event":
		d.guards = lowerdata.GuardsEvent
	default:
		return nil, fmt.Errorf("unknown variant %q of logical units", opts.LogicalVariant)
	}
	typed := false
	if opts.Types {
		// The typed entry points fix the typed rules that the reachable rules start from, and
		// whether the Node rule bodies are needed.
		roots, needTyped, needNodes, err := print.TypedEntries(&d.Parser)
		if err != nil {
			return nil, err
		}
		if opts.TypedOnly && needNodes && !opts.KeepNodes && !opts.DisableKeepNodes {
			// A typed entry point converts the Node result of its rule, so the Node rule functions
			// are needed; generate with them.
			opts.KeepNodes = true
			return Decide(prog, derived, start, opts)
		}
		typed, d.roots = needTyped, roots
	}
	var rec, ins, insRec codegen.Checked
	if opts.Recognize {
		// The recognizer: the program in which no rule builds a value, in a rule table of its own.
		var err error
		if rec, err = derived.Recognizer(); err != nil {
			return nil, err
		}
		d.Rec = rec.View
	}
	if prog.View.Run.HasMissing {
		// The parses with missing-token insertion enabled run the insertion program, as on every
		// backend: its tables lower the #missing sites with their insertions and keep no guard, so
		// that the tables of the parses without insertion stay as they are without #missing. A
		// recognize-only parser, which has no Parse, needs only the recognizer of the program.
		var err error
		if !opts.RecognizeOnly {
			if ins, err = derived.Insertion(); err != nil {
				return nil, err
			}
			d.Ins = ins.View
		}
		if opts.Recognize {
			if insRec, err = derived.InsertionRecognizer(); err != nil {
				return nil, err
			}
			d.InsRec = insRec.View
		}
	}
	add := func(traced bool, suffix string) {
		if !traced || !print.NoNodeRules(opts) {
			d.specs = append(d.specs, tableSpec{"rules" + suffix, "rules", prog.View, prog.Elab, traced})
		}
		if d.Rec != nil {
			d.specs = append(d.specs, tableSpec{"recRules" + suffix, "recRules", rec.View, rec.Elab, traced})
		}
		if typed {
			d.specs = append(d.specs, tableSpec{"typed" + suffix, "trules", prog.View, prog.Elab, traced})
		}
		if d.Ins != nil {
			d.specs = append(d.specs, tableSpec{"insRules" + suffix, "insRules", ins.View, ins.Elab, traced})
		}
		if d.InsRec != nil {
			d.specs = append(d.specs, tableSpec{"insRecRules" + suffix, "insRecRules", insRec.View, insRec.Elab, traced})
		}
	}
	add(false, "")
	if opts.Observability {
		// An observed parse runs the traced copies of the tables.
		add(true, "Traced")
	}
	return d, nil
}

// Labels returns the names of the tables of the parser in the order in which they are built.
func (d *Decision) Labels() []string {
	l := make([]string, len(d.specs))
	for i, s := range d.specs {
		l[i] = s.name
	}
	return l
}

// Sources returns the builds of the Core IR of the tables, in the order of Labels. The Node table,
// the typed table and the traced copies of both build the same module from the program, the
// recognizer and its traced copy theirs from the recognizer, and the tables of the insertion program
// and of its recognizer theirs from those programs, which the key of the build says.
func (d *Decision) Sources() []common.TableSource {
	s := make([]common.TableSource, len(d.specs))
	for i, t := range d.specs {
		switch t.table {
		case "recRules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(t.elab, plandata.TableConfig{}), Key: "recognizer", Origin: true}
		case "insRules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(t.elab, plandata.TableConfig{Values: true}), Key: "insertion", Origin: true}
		case "insRecRules":
			s[i] = common.TableSource{Build: flowplan.TableBuild(t.elab, plandata.TableConfig{}), Key: "insertion-recognizer", Origin: true}
		default:
			s[i] = common.TableSource{Build: flowplan.TableBuild(t.elab, plandata.TableConfig{Values: true}), Key: "program", Origin: true}
		}
	}
	return s
}

// module returns the Core module of the table of the rule table table that is no traced copy, from
// the modules mods of the tables in the order of Labels, or nil if the parser has none.
func (d *Decision) module(mods []*core.Module, table string) *core.Module {
	for i, s := range d.specs {
		if s.table == table && !s.traced {
			return mods[i]
		}
	}
	if table == "trules" || table == "rules" {
		// The typed table and the Node table are built from the same module.
		for i, s := range d.specs {
			if s.prog == d.Prog && !s.traced {
				return mods[i]
			}
		}
	}
	return nil
}

// Cores returns what the program is planned from (flowplan.Planning), given the Core modules mods of
// the tables in the order of Labels: the Node table, the recognizer if the parser has one and, with
// typed output, the typed table, the entry points: the start rules of the Node and recognizer tables
// and the typed rules that the typed entry points enter, and the tables of the parser as the units
// that the planning plans. Every table but the Node table of a typed-only parser, whose rows the
// typed table shares, leaves out the rules that the plan does not reach unless the options keep them
// (common.GenOptions.KeepUnreachable); the expectation table is numbered from the Node and
// recognizer tables with every rule.
func (d *Decision) Cores(mods []*core.Module) *plandata.Cores {
	c := &plandata.Cores{}
	for _, name := range d.Prog.StartRules {
		c.Roots = append(c.Roots, plandata.Entry{Table: "rules", ID: d.Prog.ByName[name].ID})
		if d.Rec != nil {
			c.Roots = append(c.Roots, plandata.Entry{Table: "recRules", ID: d.Rec.ByName[name].ID})
		}
		if d.Ins != nil {
			c.Roots = append(c.Roots, plandata.Entry{Table: "insRules", ID: d.Ins.ByName[name].ID})
		}
		if d.InsRec != nil {
			c.Roots = append(c.Roots, plandata.Entry{Table: "insRecRules", ID: d.InsRec.ByName[name].ID})
		}
	}
	c.Roots = append(c.Roots, d.roots...)
	add := func(table string, pr *common.Program) {
		t := plandata.CoreTable{Name: table, M: d.module(mods, table), Typed: table == "trules", Slots: (table == "rules" || table == "insRules") && d.Opts.Document}
		for _, r := range append(append([]*common.Rule(nil), pr.RuleList...), pr.Twins...) {
			_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
			t.Rules = append(t.Rules, plandata.CallRule{Plain: r.Plain, Leader: r.Leader, Operators: operatorsRule})
		}
		c.Tables = append(c.Tables, t)
	}
	add("rules", d.Prog)
	if d.Rec != nil {
		add("recRules", d.Rec)
	}
	if d.Opts.Types {
		add("trules", d.Prog)
	}
	if d.Ins != nil {
		add("insRules", d.Ins)
	}
	if d.InsRec != nil {
		add("insRecRules", d.InsRec)
	}
	nodes := print.NoNodeRules(d.Opts)
	for i, s := range d.specs {
		all := append(append([]*common.Rule(nil), s.prog.RuleList...), s.prog.Twins...)
		u := plandata.Unit{Label: s.name, Table: s.table, IR: d.tableIR(s, mods[i]), Rules: make([]*plandata.Rule, len(all)),
			Prune:       !d.Opts.KeepUnreachable && !(nodes && s.table == "rules"),
			NoOperators: nodes && s.table == "rules",
			Typed:       s.table == "trules", Frames: "rules",
			Numbered: !s.traced && s.table != "trules"}
		for j, r := range all {
			u.Rules[j] = r.PlanRule()
		}
		if d.Opts.Stream && (s.table == "rules" || s.table == "insRules") {
			// The Node table and the insertion table, which a stream parse with missing-token
			// insertion enabled runs, each have the body of the start rule that ParseStream runs.
			u.Streams = []*plandata.Rule{s.prog.ByName[d.Start.Name].PlanRule()}
		}
		if d.refuse != nil && !u.Typed {
			refuse := d.refuse
			u.Refuse = func(k plandata.Key) bool { return k.Use == plandata.RuleBody && refuse(all[k.Rule]) }
		}
		c.Units = append(c.Units, u)
	}
	return c
}

// tableIR returns the table whose Core IR after the Core passes is m, with the lowering options of the
// table s.
func (d *Decision) tableIR(s tableSpec, m *core.Module) *plandata.TableIR {
	o := d.Opts
	examined := o.Document || s.traced
	if s.table == "trules" {
		// Node is false: a function lowered with the options of one runtime is never printed for the other.
		return &plandata.TableIR{M: m, Values: true, Opts: lowerdata.Options{Logical: o.LogicalUnits(), Guards: d.guards, Recovers: flowlower.Recovers(m), Observe: s.traced, Document: examined, Traced: s.traced}}
	}
	if s.table == "insRules" || s.table == "insRecRules" {
		// The tables of the insertion program, which only parses inserting missing tokens run: they
		// lower the #missing sites with their insertions and keep no guard (lowerdata.GuardsNone), as
		// the insertion modules of the VMs do. A Document with a missing-token limit runs them, so with
		// Documents they resume runs as the other tables do; an observed parse records the examined input.
		return flowplan.TableOf(m, plandata.TableConfig{Values: s.table == "insRules", Logical: o.LogicalUnits(), Guards: lowerdata.GuardsNone, Observe: s.traced, Document: examined,
			Resume: o.Document, Traced: s.traced, Inserts: true})
	}
	return flowplan.TableOf(m, plandata.TableConfig{Values: s.table != "recRules", Logical: o.LogicalUnits(), Guards: d.guards, Observe: s.traced, Document: examined,
		// A parser with Documents resumes the runs of its repetitions; an observed parse records the
		// examined input (Document) but keeps no runs (lowerdata.Options.Resume). Its #missing sites
		// lower as their literals, so that the parses without insertion pay nothing for them.
		Resume: o.Document, Traced: s.traced})
}

// Printing is the step print-go: it prints the files of the Go parser of the target of the pipeline
// (codegen.TargetFeature) from its tables at the Exec stage, with the Go printer, which receives the
// values of the step's parameters (print.Params).
var Printing = pipe.New(pipe.Def[*execdata.Tables, result.Output]{
	Decl: pipe.Decl{Name: "print-go", Required: true, Obligation: "The files are the Go parser of the tables.", Features: []pipe.FeatureID{codegen.TargetFeature.ID()},
		Params: append(slices.Clone(codegen.Params), TypedObjectSlabs, TypedListSlabs, TypedOutputBootstrap)},
	In: execdata.TablesIR, Out: result.IR,
	Run: func(c *pipe.Ctx[*execdata.Tables], t *execdata.Tables) (result.Output, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, codegen.TargetFeature).(*Target)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: print-go runs for the target %s", pipe.Feature(c, codegen.TargetFeature).Backend())
		}
		d, err := target.Decision()
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		var h print.Hooks
		if target.hooks != nil {
			h = *target.hooks
		}
		params := print.Params{Params: codegen.ParamsOf(c.Value), TypedObjectSlabs: c.Value(TypedObjectSlabs) != 0, TypedListSlabs: c.Value(TypedListSlabs) != 0, TypedOutputBootstrap: c.Value(TypedOutputBootstrap) != 0}
		files, err := print.Print(&d.Parser, params, t, h)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return &result.Files{Lang: result.Go, Files: files}, pass.PreserveNone(), nil
	},
})
