package engine

import (
	"syntaxir.dev/sxir/internal/frontend"
	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/schemaexport"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/schema"
)

// The engine does not import the generators or the output schema; packages gen and sxir join them
// (codegen.Go, codegen.TypeScript and Parser.OutputSchema). These helpers join them the
// same way for the tests of this package.

// compileForGenerator compiles g for the generators, as the compileForGenerator of package
// codegen does.
func compileForGenerator(g *ast.Grammar, opts checkpkg.Options) (*frontend.Result, func() (*frontend.Result, error), error) {
	prog, err := Compile(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*frontend.Result, error) {
		rec, err := prog.Recognizer()
		if err != nil {
			return nil, err
		}
		return rec.Static(), nil
	}
	return prog.Static(), recognizer, nil
}

// outputSchemaPrototype exports the output schema of p for start, as Parser.OutputSchema does.
func outputSchemaPrototype(p *Program, start string, allRules bool) (*schema.Output, error) {
	sp, err := p.SchemaSource(start)
	if err != nil {
		return nil, err
	}
	return schemaexport.Export(sp, start, allRules)
}
