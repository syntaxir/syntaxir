package engine

import (
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// The tests of this package compile the grammars that they generate parsers for with the engine.
func init() {
	codegentest.Compile = compileForGenerator
	codegentest.Reference = referenceProgram
}

// referenceProgram compiles src with the engine for the comparisons of generated parsers with it.
func referenceProgram(t *testing.T, src string, opts checkpkg.Options) codegentest.ReferenceParse {
	t.Helper()
	prog := compileBare(t, src, opts)
	return func(input string, bytes, recognize bool, missing int, units *uint64) (*value.Node, error) {
		unit := sxirinput.CodePoints
		if bytes {
			unit = sxirinput.Bytes
		}
		return prog.ParseWith("main", input, ParseOptions{Unit: unit, Recognize: recognize, MissingTokens: missing, ComputeReport: units})
	}
}
