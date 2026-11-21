package query_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/query"
	"syntaxir.dev/sxir/pkg/runtime/schema"
)

// compileTestSource compiles SyntaxIR source as enginetest.CompileSource does, for a test whose subject is
// not start rules: unless the source has declaration attributes, every rule becomes a start rule,
// the first one the default.
func compileTestSource(src, start string, opts ...enginetest.CompileOption) (*engine.Parser, error) {
	g, err := enginetest.ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return enginetest.Compile(starttest.MarkAll(g), start, opts...)
}

// mustCompile compiles SyntaxIR source with the start rule main.
func mustCompile(t testing.TB, src string, opts ...enginetest.CompileOption) *engine.Parser {
	t.Helper()
	p, err := compileTestSource(src, "main", opts...)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// mustSchema exports the output schema of p.
func mustSchema(t testing.TB, p *engine.Parser, opts ...sxirruntime.SchemaOption) *schema.Output {
	t.Helper()
	s, err := p.OutputSchema(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(name string, p query.Pattern) query.FieldPattern {
	return query.FieldPattern{Name: name, Pattern: p}
}

// textStruct matches a value of the text struct type name, such as Int or Name.
func textStruct(name string) query.Pattern {
	return query.Pattern{Type: name, Fields: []query.FieldPattern{{Name: "Text", Pattern: query.Pattern{Category: "string"}}}}
}
