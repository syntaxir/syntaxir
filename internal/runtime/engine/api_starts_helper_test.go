package engine_test

import (
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/starttest"
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
