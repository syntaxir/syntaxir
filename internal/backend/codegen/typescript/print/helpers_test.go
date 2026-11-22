package print_test

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tests of this package compile the grammars that they generate parsers for with the engine,
// as package engine does.
func init() { enginetest.UseEngine() }

var (
	mustCompile         = enginetest.MustCompile
	compileBare         = enginetest.CompileBare
	referenceProgram    = enginetest.Reference
	compileForGenerator = enginetest.CompileForGenerator
	parseTest           = enginetest.Parse
	mustParse           = enginetest.MustParse
	compileStarts       = enginetest.CompileStarts
)

// inferredType returns the value type the checker infers for the rule name, with the rules it
// refers to replaced by their values at the top level and in unions and optional types. RuleType
// refers to a rule without a declared type by name instead.
func inferredType(p *engine.Program, name string) (string, bool) {
	t, ok := entry.Checked(p).Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &check.ValueInference{Checker: entry.Checked(p).Checker(), Out: entry.Checked(p).Typed.Values}
	return v.Expand(t).String(), true
}

// generateTS generates the TypeScript parser of g with opts, the library of the tests, the edits and
// observers of x and the hooks h of the printer, as package generate does (generate.TypeScriptWith).
func generateTS(g *ast.Grammar, opts common.GenOptions, x driver.Options, h *print.Hooks) ([]common.GeneratedFile, error) {
	return generate.TypeScriptWith(compileForGenerator, g, codegentest.WithLib(opts), x, h)
}
