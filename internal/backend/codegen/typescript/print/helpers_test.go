package print_test

import (
	"errors"
	"fmt"
	"testing"

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
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
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

// engineUnits returns the compute units that the engine counts when it parses, or recognizes, in
// from the rule start of the grammar src in the position unit (spec/runtime/parsing.md, Compute
// Units). A generated parser counts the same units. A parse that fails with a syntax error counts
// its units too.
func engineUnits(t *testing.T, src, start, in string, unit input.PositionUnit, recognize bool) uint64 {
	t.Helper()
	var units uint64
	_, err := mustCompile(t, src).ParseWith(start, in, engine.ParseOptions{Unit: unit, Recognize: recognize, ComputeReport: &units})
	if limit := (*runtime.LimitError)(nil); errors.As(err, &limit) {
		t.Fatalf("the engine's parse of %q from %s: %v", in, start, err)
	}
	return units
}

// tsEngineUnits returns a TypeScript Map from the position units of a generated parser, imported as
// p, to the units engineUnits returns for them.
func tsEngineUnits(t *testing.T, src, start, in string, recognize bool) string {
	t.Helper()
	return fmt.Sprintf("new Map<p.Unit,number>([[p.CodePoints,%d],[p.Bytes,%d]])",
		engineUnits(t, src, start, in, input.CodePoints, recognize), engineUnits(t, src, start, in, input.Bytes, recognize))
}

// tsUnitsOf is TypeScript that declares unitsOf, which returns the compute units that a run of a
// generated parser, given its options, reports.
const tsUnitsOf = `
function unitsOf(unit:p.Unit,run:(o:p.ParseOptions)=>unknown):number{const report={units:0};run({unit,computeReport:report});return report.units}
`

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
