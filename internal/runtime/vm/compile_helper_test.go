package vm

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// compileModule compiles src with source information to a module.
func compileModule(t *testing.T, src string) *bytecode.Module {
	t.Helper()
	m, err := vmlower.Compile(compiletest.Program(t, src, compile.Options{SourceInfo: true}))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
