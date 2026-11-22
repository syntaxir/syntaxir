// Package entry is the compile entry of the driver: it compiles a grammar to a program that the
// runtime runs (engine.Program), with the checked program on the compiler side of it
// (checked.Source), and gives the tools that read the checked program (the generators, the output
// schema, the language server) access to it.
package entry

import (
	"context"

	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/frontend/schemaexport"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Options configures the compilation of a grammar to a run-time program: the options of the check
// (Compile) and the optimization settings of its backends (Settings).
type Options = checked.Options

// A checked source exports the output schemas of the parsers of its programs.
var _ engine.SchemaSource = (*checked.Source)(nil)

// Compile compiles the grammar.
func Compile(g *ast.Grammar, opts Options) (*engine.Program, error) {
	s, err := checked.Compile(g, opts, nil)
	if err != nil {
		return nil, err
	}
	return engine.New(s), nil
}

// CompileParser compiles g and returns a parser that starts at the rule start, which must be a
// start rule; an empty start selects the default start rule.
func CompileParser(g *ast.Grammar, start string, opts Options) (*engine.Parser, error) {
	prog, err := Compile(g, opts)
	if err != nil {
		return nil, err
	}
	return engine.NewParser(prog, start)
}

// CompileSource parses the grammar source src and compiles it with CompileParser.
func CompileSource(src, start string, opts Options) (*engine.Parser, error) {
	g, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return CompileParser(g, start, opts)
}

// CompileContext is Compile under ctx, with the cancellation of checked.CompileContext.
func CompileContext(ctx context.Context, g *ast.Grammar, opts Options) (*engine.Program, error) {
	s, err := checked.CompileContext(ctx, g, opts)
	if err != nil {
		return nil, err
	}
	return engine.New(s), nil
}

// Source returns the compiler side of p, or nil for a program loaded from a compiled grammar.
func Source(p *engine.Program) *checked.Source {
	s, _ := p.Source().(*checked.Source)
	return s
}

// Checked returns the checked program that p was compiled from, or nil for a program loaded from a
// compiled grammar.
func Checked(p *engine.Program) *frontend.Result {
	if s := Source(p); s != nil {
		return s.Checked()
	}
	return nil
}

// Grammar returns the grammar that p was compiled from, as written, or nil for a program loaded
// from a compiled grammar.
func Grammar(p *engine.Program) *ast.Grammar {
	if s := Source(p); s != nil {
		return s.Grammar()
	}
	return nil
}

// SchemaSource returns the checked grammar that an output schema of the start rule is exported
// from, after checking that the export can run: p must not be nil, start must be able to start a
// parse, and p must hold its grammar, which a program loaded from a compiled grammar does not.
// The errors of a missing program or grammar wrap program.ErrSchemaUnavailable.
func SchemaSource(p *engine.Program, start string) (schemaexport.Source, error) {
	if p == nil {
		return schemaexport.Source{}, program.ErrSchemaUnavailable
	}
	if err := program.EntryError(start); err != nil {
		return schemaexport.Source{}, err
	}
	s := Source(p)
	if s == nil {
		return schemaexport.Source{}, program.ErrSchemaNeedsSource
	}
	return s.Schema(), nil
}
