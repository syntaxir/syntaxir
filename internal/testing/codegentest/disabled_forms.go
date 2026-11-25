package codegentest

// DisabledFormTest names the test that shows a parser generated without an optimization (or the engine
// with it off) returns what the default does: the test function and the directory of its package, relative
// to the module root.
type DisabledFormTest struct{ Test, Dir string }

// DisabledParity is the test that covers the optimizations whose disabled form is compared over the shared
// optimization cases of the Go printer.
const DisabledParity = "TestOptimizationDisabledParity"

// DisabledFormTests says for each registered optimization the test of its disabled form. It is the one
// registry that the printer's TestOptimizationsHaveTests and the engine's TestPassFlagClasses read: the
// engine differential covers the optimizations that the engine runs, and this lists the test of the
// others in generated code.
var DisabledFormTests = map[string]DisabledFormTest{
	"terminals":              {"TestTerminalProgramDisabledParity", "internal/backend/codegen/golang/print"},
	"text-dispatch":          {"TestLiteralTextPredicateDispatchDisabledParity", "internal/backend/codegen/golang/print"},
	"field-compare":          {"TestGeneratedTextFieldComparisons", "internal/backend/codegen/golang/print"},
	"dispatch":               {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"guards":                 {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"guarded-classes":        {"TestGuardedClassDisabledParity", "internal/backend/codegen/golang/print"},
	"scans":                  {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"outline":                {"TestOptimizationDisabledTSParity", "internal/backend/codegen/typescript/print"},
	"class-runs":             {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"class-tables":           {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"inline-literals":        {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"method-sharing":         {"TestGeneratedUnsharedParsersMatchEngine", "internal/backend/codegen/golang/print"},
	"used-features":          {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"literal-sharing":        {"TestGeneratedUnsharedParsersMatchEngine", "internal/backend/codegen/golang/print"},
	"ts-literal-sharing":     {"TestGeneratedSharingIdentity", "internal/backend/codegen/common"},
	"typed-object-slabs":     {"TestGeneratedTypedOutputRetentionFixture", "internal/backend/codegen/golang/print"},
	"typed-list-slabs":       {"TestGeneratedTypedOutputRetentionFixture", "internal/backend/codegen/golang/print"},
	"typed-output-bootstrap": {"TestGeneratedTypedOutputRetentionFixture", "internal/backend/codegen/golang/print"},
	"sparse-memo":            {"TestGeneratedTSSparseMemoControls", "internal/backend/codegen/typescript/print"},
	"ts-merged-records":      {"TestTSExecPassesKeepResultsAndSpends", "internal/backend/codegen/typescript/print"},
	"ts-tested-inits":        {"TestTSExecPassesKeepResultsAndSpends", "internal/backend/codegen/typescript/print"},
	"canonicalize":           {"TestCanonicalizePassOff", "internal/middleend/codepass"},
	"drop-after-exit":        {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"drop-unused-labels":     {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"fold-branches":          {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"fold-constants":         {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"splice-blocks":          {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
}
