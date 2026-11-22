package vm

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// compileModule compiles src with source information to a module.
func compileModule(t *testing.T, src string) *bytecode.Module {
	t.Helper()
	prog := compiletest.Program(t, src, check.Options{SourceInfo: true})
	m, err := vmlower.Compile(vmlower.Input{Run: prog.Runtime(), Elab: prog})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
