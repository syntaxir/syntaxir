package engine

import (
	"context"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/schemaexport"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tests of this package compile grammars through the compiler side of the driver
// (checked.Source), which the compile entry of the driver (entry.Compile) wraps the same way; the
// entry itself imports this package, so the tests cannot call it.

// Compile compiles the grammar, as entry.Compile does.
func Compile(g *ast.Grammar, opts checkpkg.Options) (*Program, error) {
	s, err := checked.Compile(g, checked.Options{Compile: opts}, nil)
	if err != nil {
		return nil, err
	}
	return New(s), nil
}

// CompileContext compiles the grammar under ctx, as entry.CompileContext does.
func CompileContext(ctx context.Context, g *ast.Grammar, opts checkpkg.Options) (prog *Program, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return Compile(g, opts)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(cancel.Cancellation); ok {
				prog, err = nil, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	s, err := checked.Compile(g, checked.Options{Compile: opts}, &cancel.Control{Ctx: ctx})
	if err != nil {
		return nil, err
	}
	return New(s), nil
}

// build builds a program from the grammar without the check of its declarations of linear cost. If
// flags is not nil, it skips static analysis and type checking and uses flags instead.
func build(g *ast.Grammar, opts checkpkg.Options, flags []checkpkg.RuleFlags) (*Program, error) {
	s, err := checked.Check(g, checked.Options{Compile: opts}, flags, nil)
	if err != nil {
		return nil, err
	}
	return New(s), nil
}

// checkedProgram returns the program that runs the checked program sp.
func checkedProgram(sp *frontend.Result) *Program { return New(checked.Of(sp, pass.Settings{})) }

// Static returns the checked program that p was compiled from, or nil for a program loaded from a
// compiled grammar.
func (p *Program) Static() *frontend.Result {
	if s, ok := p.src.(*checked.Source); ok {
		return s.Checked()
	}
	return nil
}

// SourceGrammar returns the grammar as written that p was compiled from, or nil for a program
// loaded from a compiled grammar.
func (p *Program) SourceGrammar() *ast.Grammar {
	if s, ok := p.src.(*checked.Source); ok {
		return s.Grammar()
	}
	return nil
}

// SchemaSource is entry.SchemaSource.
func (p *Program) SchemaSource(start string) (schemaexport.Source, error) {
	if p == nil {
		return schemaexport.Source{}, program.ErrSchemaUnavailable
	}
	if err := program.EntryError(start); err != nil {
		return schemaexport.Source{}, err
	}
	sp := p.Static()
	if sp == nil {
		return schemaexport.Source{}, program.ErrSchemaNeedsSource
	}
	return checked.Of(sp, pass.Settings{}).Schema(), nil
}

// lazyModule returns the bytecode module that p holds: for a program compiled from a grammar, the
// module that its compiler side has built on first use (checked.Source.Module), or nil before that.
func lazyModule(p *Program) *bytecode.Module {
	if s, ok := p.src.(*checked.Source); ok {
		return s.BuiltModule()
	}
	return p.Bytecode
}
