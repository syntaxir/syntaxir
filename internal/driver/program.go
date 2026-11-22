package driver

import (
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Program is the input of a compilation: the checked program and the options that every target
// shares (start rule, source information, default values, left-recursion bindings). Check makes it.
type Program struct {
	checked    codegen.Checked
	recognizer func() (codegen.Checked, error)
	opts       common.CompileOptions
	start      *common.Rule
}

// View returns the view of the checked program that the generators read (common.Program).
func (p *Program) View() *common.Program { return p.checked.View }

// Recognizer returns the view of the recognizer of the program, the program in which no rule builds
// a value, which it checks on the first call.
func (p *Program) Recognizer() (*common.Program, error) {
	r, err := p.recognizer()
	return r.View, err
}

// Options returns the options that the program was checked with.
func (p *Program) Options() common.CompileOptions { return p.opts }

// Start returns the start rule of the compilation.
func (p *Program) Start() *common.Rule { return p.start }

// compilation is the input of the pipeline of a target: the program, the target with its options
// completed from the program and the call, the options of the call, the target as its printer
// reads it (codegen.TargetFeature), and the reach of the plan that the fallback of planning gives
// (plandata.ReachFeature), nil for none; the assembled pipeline provides both.
type compilation struct {
	p      *Program
	t      Target
	o      Options
	target common.Target
	reach  map[plandata.Entry]bool
}

// compilationIR is the kind program: the input of a compilation.
var compilationIR = pipe.RegisterIR(&pipe.IR[*compilation]{Name: "program"})

// Compiler checks a grammar for a generator with the options opts. It returns the checked program
// and a function that returns that of its recognizer (the program in which no rule builds a value),
// which it checks on first use.
type Compiler func(g *ast.Grammar, opts check.Options) (prog *frontend.Result, recognizer func() (*frontend.Result, error), err error)

// Check checks the grammar g for generation with the options o, and start, the start rule of the
// generated Parse function ("" for the grammar's default start rule). The recognizer of the program
// is checked when a target first needs it. A grammar whose parser could insert missing tokens is an
// error, since no generated parser supports them.
func Check(g *ast.Grammar, o common.CompileOptions, start string) (*Program, error) {
	return CheckWith(checkForGenerator, g, o, start)
}

// CheckWith is Check with the compiler c, for the tests whose checker builds the programs as the
// engine does. The generators read the views of the checked programs (View), and the steps that build
// the Core IR of their tables the checked programs themselves.
func CheckWith(c Compiler, g *ast.Grammar, o common.CompileOptions, start string) (*Program, error) {
	prog, rec, err := c(g, CompileOptions(o))
	if err != nil {
		return nil, err
	}
	checked := codegen.Checked{View: View(prog), Elab: prog}
	if checked.View.Run.HasMissing {
		return nil, rprogram.MissingUnsupported("generated parsers")
	}
	r, err := checked.View.StartRule(start)
	if err != nil {
		return nil, err
	}
	recognizer := func() (codegen.Checked, error) {
		p, err := rec()
		if err != nil {
			return codegen.Checked{}, err
		}
		return codegen.Checked{View: View(p), Elab: p}, nil
	}
	return &Program{checked: checked, recognizer: sync.OnceValues(recognizer), opts: o, start: r}, nil
}

// CompileOptions returns the options of the compiler that the choices o of a generator stand for.
func CompileOptions(o common.CompileOptions) check.Options {
	return check.Options{SourceInfo: o.SourceInfo, DefaultValue: o.DefaultValue, LeftRecursionBindings: o.LeftRecursionBindings}
}

// View returns the view of the checked program p that the generators read (common.Program): the
// semantic model, the checker and the run-time program of the checked program, and a view of each
// rule over its run-time rule, with the facts that only the checked program holds copied (Lean, the twin and the
// projections).
func View(p *frontend.Result) *common.Program {
	run := p.Runtime()
	v := &common.Program{Model: p.Model, Check: p.Checker(), Typed: p.Typed, Run: run,
		RuleList: make([]*common.Rule, len(p.RuleList)), Twins: make([]*common.Rule, len(p.Twins)),
		ByName: make(map[string]*common.Rule, len(p.RuleList))}
	view := func(r *check.Rule) *common.Rule {
		rt := run.RuleList
		id := r.ID
		if id >= len(rt) {
			rt, id = run.Twins, id-len(rt)
		}
		return &common.Rule{Rule: rt[id], Def: r.Def, Action: r.Action, Captures: r.Scope, Lean: r.Lean,
			Projections: check.RuleProjections(p.Model, r)}
	}
	for i, r := range p.RuleList {
		v.RuleList[i] = view(r)
		v.ByName[r.Name] = v.RuleList[i]
	}
	for i, r := range p.Twins {
		v.Twins[i] = view(r)
	}
	for i, r := range p.RuleList {
		if r.Twin != nil {
			v.RuleList[i].Twin = v.Twins[r.Twin.ID-len(p.RuleList)]
		}
	}
	return v
}

// checkForGenerator checks g to its semantic model and sites, which is all a generator reads, and checks
// the recognizer of the program (as the engine's Program.Recognizer makes it) on first use. No
// backend of the engine is built.
func checkForGenerator(g *ast.Grammar, opts check.Options) (*frontend.Result, func() (*frontend.Result, error), error) {
	prog, err := checkGrammar(g, opts, nil)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*frontend.Result, error) {
		source := prog.Grammar
		if prog.Source != nil {
			source = prog.Source
		}
		o := prog.Opts
		o.Recognition = check.Recognize
		return checkGrammar(source, o, prog.Flags())
	}
	return prog, recognizer, nil
}

// checkGrammar checks g with opts and, if not nil, the rule flags flags, naming the hidden rules of
// inline actions in errors as written.
func checkGrammar(g *ast.Grammar, opts check.Options, flags []check.RuleFlags) (*frontend.Result, error) {
	names := desugar.InlineNames{}
	prog, err := frontend.Check(g, opts, flags, names, checkfacts.Source{}, nil)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return prog, nil
}
