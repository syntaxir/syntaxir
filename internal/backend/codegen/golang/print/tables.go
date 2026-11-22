package print

import (
	"fmt"
	"go/token"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	irbuild "syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tables of a Go parser are decided before any of them is built (Decide), built (Decision.Sources)
// and planned with the program (Decision.Cores, flowplan.Planning), lowered to the Exec stage by the
// pipeline of the compilation and printed from it (Printing).

// FlowTarget is the forms of the Flow passes that Go prints: every one.
var FlowTarget = flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}

// TableLabels are the names of the tables that a Go parser can have, in the order in which they are
// built: the Node table, the recognizer, the typed table and their traced copies.
var TableLabels = []string{"rules", "recRules", "typed", "rulesTraced", "recRulesTraced", "typedTraced"}

// Validate checks the options of a Go parser that do not depend on the grammar and returns them
// completed: the package name derived from the module path when none is given, Recognize for a
// recognize-only parser, and KeepUnreachable under a bisection limit, so that the numbered runs of
// the optional passes are those of every rule whatever the plan leaves out.
func Validate(opts common.GenOptions) (common.GenOptions, error) {
	if opts.Debug.OptBisectLimit != nil {
		opts.KeepUnreachable = true
	}
	if opts.Module == "" && (opts.Lib == common.LibEmbed || opts.Package == "") {
		return opts, fmt.Errorf("module path is required")
	}
	derived := opts.Package == ""
	if derived {
		opts.Package = defaultPackageName(opts.Module)
	}
	if !token.IsIdentifier(opts.Package) || opts.Package == "_" {
		if derived {
			return opts, fmt.Errorf("the module path %q gives the package name %q, which is not a valid Go identifier; choose the package name with -pkg (PackageName)", opts.Module, opts.Package)
		}
		return opts, fmt.Errorf("invalid package name %q", opts.Package)
	}
	if opts.RecognizeOnly {
		switch {
		case opts.Types || opts.TypedOnly || opts.Document || opts.Stream:
			return opts, fmt.Errorf("recognize-only output generates only Recognize: it cannot have typed output, typed-only output, a Document or a stream")
		}
		opts.Recognize = true
	}
	if opts.TypedOnly {
		switch {
		case !opts.Types:
			return opts, fmt.Errorf("typed-only output needs typed output (-types)")
		case opts.Document || opts.Stream:
			return opts, fmt.Errorf("typed-only output cannot have a Document or a stream, which use the untyped Node (Recognize is allowed)")
		}
	}
	if opts.Types && opts.Module == "" {
		return opts, fmt.Errorf("module path is required for typed output: the parser imports the package module/types")
	}
	if err := opts.ValidateMaxDepth(false); err != nil {
		return opts, err
	}
	if err := opts.ValidateDefaultValue(); err != nil {
		return opts, err
	}
	return opts, nil
}

// GoTarget is the Go target of a compilation (common.Target), which print-go reads: the program, the
// build of its recognizer, the start rule, the options of the target, which Validate completed, and
// the settings of the compilation. The tables of its parser are decided once from them (Decision).
type GoTarget struct {
	Prog       *common.Program
	Recognizer func() (*common.Program, error)
	Start      *common.Rule
	Opts       common.GenOptions
	Settings   pass.Settings
	// hooks are the hooks of a test, or nil (Hooks).
	hooks  *Hooks
	decide func() (*Decision, error)
}

// NewGoTarget returns the Go target of the program prog with its recognizer, the start rule start, the
// options opts, which Validate completed, the settings of the compilation and the hooks of a test,
// which production leaves nil.
func NewGoTarget(prog *common.Program, recognizer func() (*common.Program, error), start *common.Rule, opts common.GenOptions, settings pass.Settings, hooks *Hooks) *GoTarget {
	t := &GoTarget{Prog: prog, Recognizer: recognizer, Start: start, Opts: opts, Settings: settings, hooks: hooks}
	t.decide = sync.OnceValues(func() (*Decision, error) {
		d, err := Decide(prog, recognizer, start, opts)
		if err == nil && hooks != nil {
			d.refuse = hooks.RefuseFlowRecRule
		}
		return d, err
	})
	return t
}

// Backend names the backend of the target.
func (*GoTarget) Backend() string { return "go" }

// Decision returns the decision of the parser of the target, which the first call makes (Decide).
func (t *GoTarget) Decision() (*Decision, error) { return t.decide() }

// tableSpec is one table of a Go parser: its name in the dumps (Generator.IRTable), the rule table it
// fills (rules, recRules or trules), the program of its rules and whether it is a traced copy.
type tableSpec struct {
	name, table string
	prog        *common.Program
	traced      bool
}

// A Decision is what a Go parser of a program is made of, decided from the program and the options
// before any table is built: the options as the program completes them, the recognizer, the typed
// entry points and the tables.
type Decision struct {
	Opts  common.GenOptions
	Prog  *common.Program
	Rec   *common.Program // the recognizer, or nil
	Start *common.Rule
	// roots are the typed rules that the typed entry points enter.
	roots []plandata.Entry
	specs []tableSpec
	// refuse is a test's hook (hooks.RefuseFlowRecRule).
	refuse func(*common.Rule) bool
	// guards is what the guards of choices do under logical units (common.GenOptions.LogicalVariant).
	guards lowerdata.LogicalGuards
}

// Decide decides the Go parser of prog that starts at start with the options opts, which Validate
// completed: the typed entry points, and with them whether the typed table is built and whether a
// typed-only parser keeps its Node rules (common.GenOptions.KeepNodes), and the recognizer, which
// recognizer checks.
func Decide(prog *common.Program, recognizer func() (*common.Program, error), start *common.Rule, opts common.GenOptions) (*Decision, error) {
	if opts.Stream && !start.Stream {
		return nil, fmt.Errorf("stream generation needs a #stream repetition at the top level of the start rule %s", start.Name)
	}
	d := &Decision{Opts: opts, Prog: prog, Start: start}
	if opts.LogicalUnits {
		if err := logicalRefusal(prog, opts); err != nil {
			return nil, err
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
	}
	g := d.generator()
	typed := false
	if opts.Types {
		// The typed entry points fix the typed rules that the reachable rules start from, and
		// whether the Node rule bodies are needed.
		e, err := g.typedEntries(start, opts.ConvertTypes)
		if err != nil {
			return nil, err
		}
		if opts.TypedOnly && e.needNodes && !opts.KeepNodes && !opts.DisableKeepNodes {
			// A typed entry point converts the Node result of its rule, so the Node rule functions
			// are needed; generate with them.
			opts.KeepNodes = true
			return Decide(prog, recognizer, start, opts)
		}
		typed, d.roots = e.needTyped, g.typedRoots
	}
	if opts.Recognize {
		// The recognizer: the program in which no rule builds a value, in a rule table of its own.
		rec, err := recognizer()
		if err != nil {
			return nil, err
		}
		d.Rec = rec
	}
	add := func(traced bool, suffix string) {
		if !traced || !g.typedOnly {
			d.specs = append(d.specs, tableSpec{"rules" + suffix, "rules", prog, traced})
		}
		if d.Rec != nil {
			d.specs = append(d.specs, tableSpec{"recRules" + suffix, "recRules", d.Rec, traced})
		}
		if typed {
			d.specs = append(d.specs, tableSpec{"typed" + suffix, "trules", prog, traced})
		}
	}
	add(false, "")
	if opts.Observability {
		// An observed parse runs the traced copies of the tables.
		add(true, "Traced")
	}
	return d, nil
}

// generator returns a generator of the parser with the options of d, printing the Node table.
func (d *Decision) generator() *Generator {
	o := d.Opts
	nodes := (o.TypedOnly || o.RecognizeOnly) && !o.KeepNodes
	g := &Generator{typedOnly: nodes, keepTableRules: nodes, Document: o.Document, NoBudget: o.NoLimitControls || o.LogicalUnits,
		Ordinary: (o.NoLimitControls || o.LogicalUnits) && !o.Document, Prog: d.Prog, Table: "rules"}
	g.FlowTarget = FlowTarget
	g.Logical, g.LogicalGuards = o.LogicalUnits, d.guards
	if o.Stream {
		g.streamStart = d.Start
	}
	return g
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
// the typed table and the traced copies of both build the same module from the program, and the
// recognizer and its traced copy theirs from the recognizer, which the key of the build says.
func (d *Decision) Sources() []common.TableSource {
	s := make([]common.TableSource, len(d.specs))
	for i, t := range d.specs {
		if t.table == "recRules" {
			s[i] = common.TableSource{Build: flowplan.TableBuild(t.prog.Elab, plandata.TableConfig{}), Key: "recognizer"}
		} else {
			s[i] = common.TableSource{Build: func() (*core.Module, error) { return irbuild.CoreFrom(t.prog.Elab, true) }, Key: "program"}
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
		c.Roots = append(c.Roots, ruleKey{Table: "rules", ID: d.Prog.ByName[name].ID})
		if d.Rec != nil {
			c.Roots = append(c.Roots, ruleKey{Table: "recRules", ID: d.Rec.ByName[name].ID})
		}
	}
	c.Roots = append(c.Roots, d.roots...)
	add := func(table string, pr *common.Program) {
		t := plandata.CoreTable{Name: table, M: d.module(mods, table), Typed: table == "trules", Slots: table == "rules" && d.Opts.Document}
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
	g := d.generator()
	for i, s := range d.specs {
		all := append(append([]*common.Rule(nil), s.prog.RuleList...), s.prog.Twins...)
		u := plandata.Unit{Label: s.name, Table: s.table, IR: d.tableIR(s, mods[i]), Rules: make([]*plandata.Rule, len(all)),
			Prune:       !d.Opts.KeepUnreachable && !(g.keepTableRules && s.table == "rules"),
			NoOperators: g.typedOnly && s.table == "rules",
			Typed:       s.table == "trules", Frames: "rules",
			Numbered: !s.traced && s.table != "trules"}
		for j, r := range all {
			u.Rules[j] = r.PlanRule()
		}
		if g.streamStart != nil && s.table == "rules" {
			u.Stream = g.streamStart.PlanRule()
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
	// Logical units count no budget controls in the code: they count the logical units instead.
	budget := !o.NoLimitControls && !o.LogicalUnits
	if s.table == "trules" {
		// Node is false: a function lowered with the options of one runtime is never printed for the other.
		return &plandata.TableIR{M: m, Opts: lowerdata.Options{Budget: budget, Logical: o.LogicalUnits, Guards: d.guards, Recovers: flowlower.Recovers(m), Observe: s.traced, Document: examined, Traced: s.traced}}
	}
	return flowplan.TableOf(m, plandata.TableConfig{Values: s.table != "recRules", Budget: budget, Logical: o.LogicalUnits, Guards: d.guards, Observe: s.traced, Document: examined,
		// A parser with Documents resumes the runs of its repetitions; an observed parse records the
		// examined input (Document) but keeps no runs (lowerdata.Options.Resume).
		Resume: o.Document, Traced: s.traced})
}

// Printing is the step print-go: it prints the files of the Go parser of the target of the pipeline
// (common.TargetFeature) from its tables at the Exec stage.
var Printing = pipe.New(pipe.Def[*execdata.Tables, result.Output]{
	Decl: pipe.Decl{Name: "print-go", Required: true, Obligation: "The files are the Go parser of the tables.", Features: []pipe.FeatureID{common.TargetFeature.ID()}},
	In:   execdata.TablesIR, Out: result.IR,
	Run: func(c *pipe.Ctx[*execdata.Tables], t *execdata.Tables) (result.Output, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, common.TargetFeature).(*GoTarget)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: print-go runs for the target %s", pipe.Feature(c, common.TargetFeature).Backend())
		}
		d, err := target.Decision()
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		var h Hooks
		if target.hooks != nil {
			h = *target.hooks
		}
		files, _, err := printTables(d, target.Settings, t, h)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return &result.Files{Lang: result.Go, Files: files}, pass.PreserveNone(), nil
	},
})
