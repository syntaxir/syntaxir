// Package checked is the compiler side of a program compiled from a grammar: it compiles a grammar
// to a checked program and supplies, to the parses that run its run-time program, what they build
// from the grammar on first use (state.Source): the Core IR and closures of the closure backend,
// the bytecode module of the VMs, and the programs compiled again for recognition and for
// missing-token insertion. The runtime reads none of the compiler's representations; it reaches them
// only through a Source.
package checked

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/schemaexport"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/schema"
)

// Options configures the compilation of a grammar to a run-time program: what the program is,
// which the check reads, and how its backends build it, which only the backends read.
type Options struct {
	// Compile are the options of the check: what the program is.
	Compile check.Options
	// Settings are the optimization settings that the VM and the closure backend build the program
	// with. The zero settings are the speed profile, in which every Core pass is on.
	Settings pass.Settings
	// Debug are the debugging settings of the call that compiles the program, which apply to the
	// builds of its backends (Source.Module, Source.Core): the first build of each backend writes
	// what they select to their Output, labeled with the backend, and every build runs under their
	// verification and bisection limit, so that every build of a backend makes the same artifact.
	// The programs compiled again for recognition and for missing-token insertion take them as the
	// program holds them (Source.again).
	Debug common.Debug
}

// Source is the compiler side of a checked program (state.Source). It holds the checked program
// with the settings that its backends build it with, and what the backends build from them, which
// the checked program does not keep: the bytecode module, made once under moduleOnce, with the
// internal error of its compilation, if any, kept beside it so that every parse that runs this
// source reports it.
type Source struct {
	prog     *frontend.Result
	settings pass.Settings
	// role is which program of its grammar prog is, and parser the options that the grammar's parser
	// was checked with (driver.ProgramOf); program is prog wrapped once for the driver.
	role    driver.Role
	parser  check.Options
	program func() *driver.Program

	moduleOnce sync.Once
	module     *bytecode.Module
	moduleErr  error
	// built is module once Module has made it, for BuiltModule.
	built atomic.Pointer[bytecode.Module]

	// debug are the debugging settings that the backends build under (Options.Debug); Prepare
	// keeps only their verification and bisection limit once the build of the call has written
	// what they select (quiet).
	debug common.Debug
}

var _ state.Source = (*Source)(nil)

// Compile compiles the grammar and checks its declarations of linear cost; control, if not nil,
// polls the compilation and may cancel it with a cancel.Cancellation panic.
func Compile(g *ast.Grammar, opts Options, control *cancel.Control) (*Source, error) {
	s, err := Check(g, opts, nil, control)
	if err != nil {
		return nil, err
	}
	if err := checkDeclaredLinear(g, control); err != nil {
		return nil, err
	}
	return s, nil
}

// CompileContext is Compile under ctx, the compilation of an editor: a cancellation of ctx
// discards the whole program with the diagnostics it gathered and returns the error of ctx. The
// polls cover the validation of the grammar AST, the normalization of the source, the loops over
// rules and their dependencies, the shared proofs of success, and the checks of expressions and
// types; shared metadata and single operations still have stretches that do not poll, so it is no
// guarantee of latency. The Core IR and what the backends build from it are made when a parse
// first needs them, outside the compilation, under the context of that parse.
func CompileContext(ctx context.Context, g *ast.Grammar, opts Options) (s *Source, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return Compile(g, opts, nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(cancel.Cancellation); ok {
				s, err = nil, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	return Compile(g, opts, &cancel.Control{Ctx: ctx})
}

// Check compiles the grammar to a checked program without checking its declarations of linear
// cost. If flags is not nil, it skips the static analysis of the rules and uses flags instead. The
// errors name the rules as written, not the hidden rules that inline actions are lowered to. The
// program keeps a copy of g, from which it builds its recognizer and insertion program when a
// parse first needs them, so a later change to g does not change the program.
func Check(g *ast.Grammar, opts Options, flags []check.RuleFlags, control *cancel.Control) (*Source, error) {
	g = g.Clone()
	names := desugar.InlineNames{}
	prog, err := frontend.Check(g, opts.Compile, flags, names, checkfacts.Source{}, control)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	s := newSource(prog, opts.Settings, driver.Parser, opts.Compile)
	s.debug = opts.Debug
	return s, nil
}

// Of returns the Source of the checked program prog, a parser, which its backends build with the
// settings.
func Of(prog *frontend.Result, settings pass.Settings) *Source {
	return newSource(prog, settings, driver.Parser, prog.Opts)
}

// newSource returns the Source of prog, the program of the role among the programs of a grammar
// whose parser was checked with the options parser, which its backends build with the settings.
func newSource(prog *frontend.Result, settings pass.Settings, role driver.Role, parser check.Options) *Source {
	s := &Source{prog: prog, settings: settings, role: role, parser: parser}
	s.program = sync.OnceValue(func() *driver.Program { return driver.ProgramOf(prog, role, parser) })
	return s
}

// Checked returns the checked program, which the generators, the analyses and the output schema
// read.
func (s *Source) Checked() *frontend.Result { return s.prog }

// Settings returns the optimization settings that the backends build the program with.
func (s *Source) Settings() pass.Settings { return s.settings }

// Program returns the program for the driver (driver.ProgramOf), which it wraps once, on the first
// call, and checks nothing again for.
func (s *Source) Program() *driver.Program { return s.program() }

// Schema returns the checked grammar that the output schema is exported from.
func (s *Source) Schema() schemaexport.Source {
	return schemaexport.Source{Model: s.prog.Model, Checker: s.prog.Checker(), SourceInfo: s.prog.RecordsSourceInfo}
}

// Grammar returns the grammar the program was compiled from, as written: it passes grammar
// validation and compiles again to the same program.
func (s *Source) Grammar() *ast.Grammar { return s.prog.Input }

// Normalized returns the grammar the program was compiled from with its attributes normalized
// (frontend.Canonical.Normalized), without the hidden rules that inline actions are lowered to. It
// holds forms that only the compiler writes, such as #recover with its skip expression, so it is
// not input to the compiler.
func (s *Source) Normalized() *ast.Grammar {
	if s.prog.Normalized != nil {
		return s.prog.Normalized
	}
	return s.prog.Grammar
}

// Runtime returns the run-time program of the checked program.
func (s *Source) Runtime() *program.Program { return s.prog.Runtime() }

// Starts returns the start rules of the grammar, the rules marked @start where parsing may begin,
// in declaration order.
func (s *Source) Starts() []string { return slices.Clone(s.Runtime().StartRules) }

// DefaultStart returns the default start rule: the one marked @start(default), or the only start
// rule.
func (s *Source) DefaultStart() string { return s.Runtime().DefaultStartRule }

// Module builds the bytecode module of the program once, with the VM target of the driver under the
// settings of the program and a background context: the bytecode has no parse whose context it runs
// under, and its build is not cancelled.
func (s *Source) Module() (*bytecode.Module, error) {
	s.moduleOnce.Do(func() {
		outs, err := s.build(context.Background(), result.VM, s.debug)
		if err != nil {
			s.moduleErr = err
			return
		}
		s.module = outs[0].(*vmlower.Bytecode).Module
		s.built.Store(s.module)
	})
	return s.module, s.moduleErr
}

// BuiltModule returns the module that Module has made, or nil before it has: it builds nothing,
// so a caller observes whether a program is still cold.
func (s *Source) BuiltModule() *bytecode.Module { return s.built.Load() }

// Core builds the module that the closures of the program are built from (closure.Module: its Core
// and Action modules), with the closure target of the driver under the settings of the program,
// polling ctx (nil for none) wrapped so that
// a panic of its methods is attributed to it (attributedContext): a failure that a cancellation
// caused, by the manager's check of the context before a step or by a poll of the build or the
// Core passes, returns a state.BuildCancelled with the context's error, and a panic of a method of
// ctx unwinds as state.ContextPanic. Any other failure is an internal error: the program compiled,
// so its Core IR builds.
func (s *Source) Core(ctx context.Context) (any, error) {
	run := context.Background()
	if ctx != nil {
		run = attributedContext{ctx}
	}
	outs, err := s.build(run, result.Closure, s.debug)
	if err != nil {
		var c cancel.Cancellation
		if errors.As(err, &c) {
			return nil, state.BuildCancelled{Err: c.Err}
		}
		return nil, fmt.Errorf("internal error: Core IR: %w", err)
	}
	return outs[0].(*closure.Module), nil
}

// build runs the compilation of the program of s for the run-time backend b under the debugging
// settings d: it writes the pipeline text of the build first if d asks for it (Describe), and the
// driver writes the dumps and reports of d labeled with b.
func (s *Source) build(ctx context.Context, b result.Backend, d common.Debug) ([]driver.Output, error) {
	if d.PrintPipeline {
		text, err := Describe(s, d, b)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(d.Output, text); err != nil {
			return nil, err
		}
	}
	return driver.Compile(ctx, s.Program(), driver.Options{Settings: s.settings, Debug: d}, driver.Target{Backend: b})
}

// quiet returns the debugging settings d without what they write: a build again under them makes
// the artifact of the first build, under its verification and bisection limit, and writes nothing.
func quiet(d common.Debug) common.Debug {
	return common.Debug{VerifyEach: d.VerifyEach, OptBisectLimit: d.OptBisectLimit}
}

// Prepare builds the artifact of the program of s that the run-time backend b runs (Module for the
// VMs, Core for the closures) when the debugging settings of its compilation write something, so
// that their output belongs to the call that compiled the program rather than to its first parse,
// and then keeps only what makes the artifact (quiet): the closures build their Core module again
// on demand, and a parse may build the other backend's artifact, without writing again. Without
// such settings it builds nothing, and the backends build on first use. The caller calls it before
// the program is shared.
func (s *Source) Prepare(b result.Backend) error {
	if !s.debug.Writes() {
		return nil
	}
	var err error
	switch b {
	case result.VM:
		_, err = s.Module()
	case result.Closure:
		_, err = s.Core(nil)
	default:
		return fmt.Errorf("the backend %s builds no run-time program (vm, closure)", b)
	}
	s.debug = quiet(s.debug)
	return err
}

// attributedContext is a parse's context whose Err attributes a panic to the caller's context
// (state.ContextPanic), as the parse's own checks do, so the parse re-raises the caller's value.
type attributedContext struct{ context.Context }

func (c attributedContext) Err() error {
	defer func() {
		if x := recover(); x != nil {
			switch x.(type) {
			case state.Fatal, state.ContextPanic:
				panic(x) // already attributed
			default:
				panic(state.ContextPanic{Value: x})
			}
		}
	}()
	return c.Context.Err()
}

// Family builds the closures of the family f from the module core that Core built (closure.Build).
func (s *Source) Family(core any, f state.Family) ([]*state.Rule, error) {
	return closure.Build(s.prog.Runtime(), s.prog.Checker(), core.(*closure.Module), f)
}

// Recognizer compiles the grammar again as a program that builds no tree, with the rule flags of
// this one: the recognizer of the parser, or of the insertion program for an insertion program.
func (s *Source) Recognizer() (state.Source, error) {
	role := driver.Recognizer
	if s.role == driver.Insertion {
		role = driver.InsertionRecognizer
	}
	return s.again(role, func(o *check.Options) { o.Recognition = check.Recognize }, s.prog.Flags())
}

// Insertion compiles the grammar again as the program of the parses with missing-token insertion
// enabled, with the rule flags of insertion: those of the program itself when insertion forms no
// left-recursion cycle of its own (check.Model.InsertionFlags is nil).
func (s *Source) Insertion() (state.Source, error) {
	flags := s.prog.InsertionFlags
	if flags == nil {
		flags = s.prog.Flags()
	}
	return s.again(driver.Insertion, func(*check.Options) {}, flags)
}

// again checks the program of the role from the canonical grammar of this one (frontend.Derive),
// with the rule flags and the options the program was compiled with, changed by derive for what
// the build is for, and with the program's settings. This is the one place that makes them, so no option or setting is dropped from
// a program built again (TestLazyProgramsCarryEveryOption), and a derived program keeps the options
// of the parser it is derived from. It also takes the program's debugging settings as they are
// (Prepare): a derived program that the call itself builds, such as the recognizer that Marshal
// saves, writes what they select, and one that a parse builds later builds quietly.
func (s *Source) again(role driver.Role, derive func(*check.Options), flags []check.RuleFlags) (state.Source, error) {
	o := s.prog.Opts
	derive(&o)
	prog, err := frontend.Derive(s.prog, o, flags, checkfacts.Source{}, nil)
	if err != nil {
		return nil, s.prog.InlineNames.Rewrite(err)
	}
	d := newSource(prog, s.settings, role, s.parser)
	d.debug = s.debug
	return d, nil
}

// RuleType is check.Checker.RuleType.
func (s *Source) RuleType(name string) (string, bool) { return s.prog.Checker().RuleType(name) }

// RuleTypeWithPoll is check.Model.RuleTypeWithPoll.
func (s *Source) RuleTypeWithPoll(name string, poll func()) (string, bool) {
	return s.prog.RuleTypeWithPoll(name, poll)
}

// ShapeSignature is check.Model.ShapeSignature.
func (s *Source) ShapeSignature(name string, poll func()) (string, bool) {
	return s.prog.ShapeSignature(name, poll)
}

// ValueType is check.Checker.ValueType.
func (s *Source) ValueType(name string) (string, bool) { return s.prog.Checker().ValueType(name) }

// OutputSchema exports the output schema of the rule start, or of every rule and type with
// allRules, from the checked grammar.
func (s *Source) OutputSchema(start string, allRules bool) (*schema.Output, error) {
	return schemaexport.Export(s.Schema(), start, allRules)
}
