package driver

import (
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/pass/pipe"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Program is the input of a compilation: the checked program and the options that every target
// shares (start rule, source information, default values, left-recursion bindings). Check makes it.
type Program struct {
	view       *common.Program
	recognizer func() (*common.Program, error)
	opts       common.CompileOptions
	start      *common.Rule
}

// View returns the view of the checked program that the generators read (common.Program).
func (p *Program) View() *common.Program { return p.view }

// Recognizer returns the view of the recognizer of the program, the program in which no rule builds
// a value, which it checks on the first call.
func (p *Program) Recognizer() (*common.Program, error) { return p.recognizer() }

// Options returns the options that the program was checked with.
func (p *Program) Options() common.CompileOptions { return p.opts }

// Start returns the start rule of the compilation.
func (p *Program) Start() *common.Rule { return p.start }

// programIR is the kind program: the input of a compilation.
var programIR = pipe.RegisterIR(&pipe.IR[*Program]{Name: "program"})

// Check checks the grammar g for generation with the options o, and start, the start rule of the
// generated Parse function ("" for the grammar's default start rule). The recognizer of the program
// is checked when a target first needs it. A grammar whose parser could insert missing tokens is an
// error, since no generated parser supports them.
func Check(g *ast.Grammar, o common.CompileOptions, start string) (*Program, error) {
	return CheckWith(GeneratorCompiler(checkForGenerator), g, o, start)
}

// CheckWith is Check with the compiler c, for the tests whose checker builds the programs as the
// engine does.
func CheckWith(c common.Compiler, g *ast.Grammar, o common.CompileOptions, start string) (*Program, error) {
	view, recognizer, err := c(g, o)
	if err != nil {
		return nil, err
	}
	if view.Run.HasMissing {
		return nil, rprogram.MissingUnsupported("generated parsers")
	}
	r, err := view.StartRule(start)
	if err != nil {
		return nil, err
	}
	return &Program{view: view, recognizer: sync.OnceValues(recognizer), opts: o, start: r}, nil
}

// GeneratorCompiler returns the compiler of the generators (common.Compiler) that checks a grammar
// with c, which returns the checked program and a function that returns that of its recognizer, and
// gives the generators their views (View).
func GeneratorCompiler(c func(g *ast.Grammar, opts check.Options) (*frontend.Result, func() (*frontend.Result, error), error)) common.Compiler {
	return func(g *ast.Grammar, opts common.CompileOptions) (*common.Program, func() (*common.Program, error), error) {
		prog, rec, err := c(g, CompileOptions(opts))
		if err != nil {
			return nil, nil, err
		}
		recognizer := func() (*common.Program, error) {
			r, err := rec()
			if err != nil {
				return nil, err
			}
			return View(r), nil
		}
		return View(prog), recognizer, nil
	}
}

// CompileOptions returns the options of the compiler that the choices o of a generator stand for.
func CompileOptions(o common.CompileOptions) check.Options {
	return check.Options{SourceInfo: o.SourceInfo, DefaultValue: o.DefaultValue, LeftRecursionBindings: o.LeftRecursionBindings}
}

// View returns the view of the checked program p that the generators read (common.Program): the
// semantic model, the checker and the run-time program of the checked program, the checked program
// as the input of the elaboration to Core IR (elaborate.Program), and a view of each rule over its
// run-time rule, with the facts that only the checked program holds copied (Lean, the twin and the
// projections).
func View(p *frontend.Result) *common.Program {
	run := p.Runtime()
	v := &common.Program{Model: p.Model, Check: p.Checker(), Typed: p.Typed, Run: run, Elab: p,
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
