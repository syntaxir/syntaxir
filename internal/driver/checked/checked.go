// Package checked is the compiler side of a program compiled from a grammar: it compiles a grammar
// to a checked program and supplies, to the parses that run its run-time program, what they build
// from the grammar on first use (state.Source): the Core IR and closures of the closure backend,
// the bytecode module of the VMs, and the programs compiled again for recognition and for
// missing-token insertion. The runtime reads none of the compiler's representations; it reaches them
// only through a Source.
package checked

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/frontend/schemaexport"
	ir "syntaxir.dev/sxir/internal/ir/core"
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
// errors name the rules as written, not the hidden rules that inline actions are lowered to.
func Check(g *ast.Grammar, opts Options, flags []check.RuleFlags, control *cancel.Control) (*Source, error) {
	names := desugar.InlineNames{}
	prog, err := frontend.Check(g, opts.Compile, flags, names, checkfacts.Source{}, control)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return newSource(prog, opts.Settings, driver.Parser, opts.Compile), nil
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

// Grammar returns the grammar the program was compiled from: the grammar as written, without the
// hidden rules that inline actions are lowered to.
func (s *Source) Grammar() *ast.Grammar {
	if s.prog.Source != nil {
		return s.prog.Source
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

// Module compiles the program to a bytecode module (vmlower.Compile) from its planned Node table
// (VMInput), once.
func (s *Source) Module() (*bytecode.Module, error) {
	s.moduleOnce.Do(func() {
		in, err := s.VMInput()
		if err != nil {
			s.moduleErr = err
			return
		}
		s.module, _, s.moduleErr = vmlower.Compile(in)
		s.built.Store(s.module)
	})
	return s.module, s.moduleErr
}

// VMInput returns what the bytecode lowering reads of the program: its run-time program, the
// program itself, and its planned Node table (FlowTable), which each call builds anew.
func (s *Source) VMInput() (vmlower.Input, error) {
	ft, err := s.FlowTable()
	if err != nil {
		return vmlower.Input{}, err
	}
	return vmlower.Input{Run: s.prog.Runtime(), Elab: s.prog, Table: vmlower.Table{Core: ft.IR.M, Plan: ft.Plan, Funcs: ft.Funcs}}, nil
}

// BuiltModule returns the module that Module has made, or nil before it has: it builds nothing,
// so a caller observes whether a program is still cold.
func (s *Source) BuiltModule() *bytecode.Module { return s.built.Load() }

// Core builds the Core IR of the program for the closures (elaborate.CoreControlled, then
// closure.Core), polling ctx (nil for none): an ended context returns a state.BuildCancelled.
func (s *Source) Core(ctx context.Context) (m any, err error) {
	if ctx == nil {
		return closureCore(s.prog, nil)
	}
	defer func() {
		if x := recover(); x != nil {
			cancelled, ok := x.(cancel.Cancellation)
			if !ok {
				panic(x)
			}
			m, err = nil, state.BuildCancelled{Err: cancelled.Err}
		}
	}()
	return closureCore(s.prog, &cancel.Control{Ctx: attributedContext{ctx}})
}

// closureCore builds the Core IR of the program prog, computes the attributes of its nodes
// (core.Analyze) and optimizes it for the closures (closure.Core). The build and the passes poll control (nil for none), and a cancellation unwinds
// as cancel.Cancellation. A failure is an internal error: the program compiled, so its Core IR
// builds.
func closureCore(prog *frontend.Result, control *cancel.Control) (*ir.Module, error) {
	m, err := elaborate.CoreControlled(prog, control)
	if err != nil {
		return nil, fmt.Errorf("internal error: Core IR: %w", err)
	}
	// The Core passes take a Core module with the attributes of its nodes computed.
	ir.Analyze(m)
	return closure.Core(m)
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

// Family builds the closures of the family f from the Core IR core (closure.Build).
func (s *Source) Family(core any, f state.Family) ([]*state.Rule, error) {
	return closure.Build(s.prog.Runtime(), s.prog.Checker(), core.(*ir.Module), f)
}

// Recognizer compiles the grammar again as a program that builds no tree, with the rule flags of
// this one.
func (s *Source) Recognizer() (state.Source, error) {
	return s.again(driver.Recognizer, func(o *check.Options) { o.Recognition = check.Recognize }, s.prog.Flags())
}

// Insertion compiles the grammar again with the rule flags of missing-token insertion.
func (s *Source) Insertion() (state.Source, error) {
	return s.again(driver.Insertion, func(*check.Options) {}, s.prog.InsertionFlags)
}

// again compiles the grammar again as the program of the role, with the rule flags and the options
// the program was compiled with, changed by derive for what the build is for, and with the
// program's settings. This is the one place that makes them, so no option or setting is dropped from
// a program built again (TestLazyProgramsCarryEveryOption), and a derived program keeps the options
// of the parser it is derived from.
func (s *Source) again(role driver.Role, derive func(*check.Options), flags []check.RuleFlags) (state.Source, error) {
	o := s.prog.Opts
	derive(&o)
	d, err := Check(s.Grammar(), Options{Compile: o, Settings: s.settings}, flags, nil)
	if err != nil {
		return nil, err
	}
	return newSource(d.prog, s.settings, role, s.parser), nil
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
