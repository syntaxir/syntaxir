package vm

import (
	"testing"

	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// compileModule compiles src with source information to a module (checked.Source.Module).
func compileModule(t *testing.T, src string) *bytecode.Module {
	t.Helper()
	m, err := checked.Of(compiletest.Program(t, src, check.Options{SourceInfo: true}), pass.Settings{}).Module()
	if err != nil {
		t.Fatal(err)
	}
	return m
}
