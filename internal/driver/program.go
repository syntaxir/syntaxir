package driver

import (
	"fmt"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Program is the input of a compilation: the checked program and the options that every target
// shares (start rule, source information, default values, left-recursion bindings). Check makes it
// for a generation, and ProgramOf for a run-time program checked elsewhere.
type Program struct {
	// prog is the checked program, and view its view for the generators (View), which a run-time
	// program makes on first use only, since only a generating backend reads it.
	prog *frontend.Result
	view func() *common.Program
	// derived builds the programs that the generators derive from it (codegen.Derived).
	derived codegen.Derived
	opts    common.CompileOptions
	// start is the start rule of a generation; nil for a run-time program, which starts at any of
	// its start rules.
	start *common.Rule
	// role is which of the run-time programs of its grammar the program is, and parser the options
	// that the grammar's parser was checked with, from which the program of the role is derived.
	role   Role
	parser check.Options
}

// Role names which of the run-time programs of a grammar a program is: the compiler side of a
// run-time program makes the parser's, and derives the recognizer and the insertion program from it
// in one place, so the parser's options and the role reproduce a program exactly.
type Role uint8

const (
	Parser              Role = iota // the program of the grammar
	Recognizer                      // derived for recognition: no rule builds a value
	Insertion                       // derived for missing-token insertion
	InsertionRecognizer             // derived from the insertion program for recognition
)

// String returns the name of the role in a pipeline text: parser, recognizer, insertion or
// insertion-recognizer.
func (r Role) String() string {
	switch r {
	case Parser:
		return "parser"
	case Recognizer:
		return "recognizer"
	case Insertion:
		return "insertion"
	case InsertionRecognizer:
		return "insertion-recognizer"
	}
	return fmt.Sprintf("Role(%d)", uint8(r))
}

// ProgramOf returns the Program of p, a run-time program checked elsewhere, with rule flags and a
// cancel control: role is which program of its grammar p is, and parser the options that the
// grammar's parser was checked with, which the program header writes. It checks nothing again. A
// run-time program has no start rule of its own and no views of derived programs, since the compiler
// side of the program derives each of them as a program of its own.
func ProgramOf(p *frontend.Result, role Role, parser check.Options) *Program {
	none := func(derived Role) func() (codegen.Checked, error) {
		return func() (codegen.Checked, error) {
			return codegen.Checked{}, fmt.Errorf("internal error: the %s program of a run-time build has no %s view", role, derived)
		}
	}
	derived := codegen.Derived{Recognizer: none(Recognizer), Insertion: none(Insertion), InsertionRecognizer: none(InsertionRecognizer)}
	return &Program{prog: p, view: sync.OnceValue(func() *common.Program { return View(p) }), derived: derived,
		opts: common.CompileOptions{SourceInfo: p.Opts.SourceInfo, DefaultValue: p.Opts.DefaultValue, LeftRecursionBindings: p.Opts.LeftRecursionBindings},
		role: role, parser: parser}
}

// View returns the view of the checked program that the generators read (common.Program).
func (p *Program) View() *common.Program { return p.view() }

// checked returns the checked program with its view, as the generators read it.
func (p *Program) checked() codegen.Checked { return codegen.Checked{View: p.view(), Elab: p.prog} }

// Recognizer returns the view of the recognizer of the program, the program in which no rule builds
// a value, which it checks on the first call.
func (p *Program) Recognizer() (*common.Program, error) {
	r, err := p.derived.Recognizer()
	return r.View, err
}

// Options returns the options that the program was checked with.
func (p *Program) Options() common.CompileOptions { return p.opts }

// Start returns the start rule of the compilation, or nil for a run-time program (ProgramOf).
func (p *Program) Start() *common.Rule { return p.start }

// Role returns which of the run-time programs of its grammar the program is; Parser for the program
// of a generation.
func (p *Program) Role() Role { return p.role }

// ParserOptions returns the options that the grammar's parser was checked with: those of the
// program itself for its parser, and those it is derived from for a recognizer or an insertion
// program.
func (p *Program) ParserOptions() check.Options { return p.parser }

// programIR is the kind program: the input of a compilation, which every target of it shares. The
// splits of the targets read the target of their branch from its feature (codegen.TargetFeature).
var programIR = pipe.RegisterIR(&pipe.IR[*Program]{Name: "program"})

// Compiler checks a grammar for a generator with the options opts. It returns the checked program
// and a function that checks the program of a derived role (Recognizer, Insertion or
// InsertionRecognizer) from it, as the engine derives it; the caller calls it at most once per role.
type Compiler func(g *ast.Grammar, opts check.Options) (prog *frontend.Result, derive func(Role) (*frontend.Result, error), err error)

// Check checks the grammar g for generation with the options o, and start, the start rule of the
// generated Parse function ("" for the grammar's default start rule). Each derived program
// (codegen.Derived) is checked when a target first needs it.
func Check(g *ast.Grammar, o common.CompileOptions, start string) (*Program, error) {
	return CheckWith(CheckForGenerator, g, o, start)
}

// CheckWith is Check with the compiler c, for the tests whose checker builds the programs as the
// engine does. The generators read the views of the checked programs (View), and the steps that build
// the Core IR of their tables the checked programs themselves.
func CheckWith(c Compiler, g *ast.Grammar, o common.CompileOptions, start string) (*Program, error) {
	prog, derive, err := c(g, CompileOptions(o))
	if err != nil {
		return nil, err
	}
	checked := codegen.Checked{View: View(prog), Elab: prog}
	r, err := checked.View.StartRule(start)
	if err != nil {
		return nil, err
	}
	of := func(role Role) func() (codegen.Checked, error) {
		return sync.OnceValues(func() (codegen.Checked, error) {
			p, err := derive(role)
			if err != nil {
				return codegen.Checked{}, err
			}
			return codegen.Checked{View: View(p), Elab: p}, nil
		})
	}
	derived := codegen.Derived{Recognizer: of(Recognizer), Insertion: of(Insertion), InsertionRecognizer: of(InsertionRecognizer)}
	return &Program{prog: prog, view: func() *common.Program { return checked.View }, derived: derived, opts: o, start: r, parser: prog.Opts}, nil
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
		return &common.Rule{Rule: rt[id], Def: r.Def, Action: r.Action, Captures: r.Scope, Lean: r.Lean}
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

// CheckForGenerator checks g to its semantic model and sites, which is all a generator reads, and
// checks the programs derived from it on first use as the engine's checked sources derive them
// (checked.Source): the recognizer and the insertion program with the rule flags of the program, or
// those of insertion when insertion forms left-recursion cycles of its own
// (check.Model.InsertionFlags), and the recognizer of the insertion program with its flags. No
// backend of the engine is built. It is the Compiler of the generators.
func CheckForGenerator(g *ast.Grammar, opts check.Options) (*frontend.Result, func(Role) (*frontend.Result, error), error) {
	prog, err := checkGrammar(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func(p *frontend.Result) (*frontend.Result, error) {
		o := p.Opts
		o.Recognition = check.Recognize
		return deriveProgram(p, o, p.Flags())
	}
	insertion := sync.OnceValues(func() (*frontend.Result, error) {
		flags := prog.InsertionFlags
		if flags == nil {
			flags = prog.Flags()
		}
		return deriveProgram(prog, prog.Opts, flags)
	})
	derive := func(role Role) (*frontend.Result, error) {
		switch role {
		case Recognizer:
			return recognizer(prog)
		case Insertion:
			return insertion()
		case InsertionRecognizer:
			ins, err := insertion()
			if err != nil {
				return nil, err
			}
			return recognizer(ins)
		}
		return nil, fmt.Errorf("internal error: no program of the role %s is derived for a generator", role)
	}
	return prog, derive, nil
}

// deriveProgram checks the program derived from prog with opts and the rule flags flags
// (frontend.Derive), naming the hidden rules of inline actions in errors as written.
func deriveProgram(prog *frontend.Result, opts check.Options, flags []check.RuleFlags) (*frontend.Result, error) {
	d, err := frontend.Derive(prog, opts, flags, checkfacts.Source{}, nil)
	if err != nil {
		return nil, prog.InlineNames.Rewrite(err)
	}
	return d, nil
}

// checkGrammar checks g with opts, naming the hidden rules of inline actions in errors as written.
func checkGrammar(g *ast.Grammar, opts check.Options) (*frontend.Result, error) {
	names := desugar.InlineNames{}
	prog, err := frontend.Check(g, opts, nil, names, checkfacts.Source{}, nil)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return prog, nil
}
