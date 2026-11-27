package print_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestTypedValuesMatchEngine checks parseValue of TypeScript parsers generated with types against the
// engine: on the grammars of the typed Go corpora (those of TestGeneratedTypes and of the typed
// parsers that insert missing tokens), in each unit and form of each input, the typed value is the
// engine's value as the TypeScript table of typed values maps it, its error is the error of the
// parse, and a parse without a value has none; the grammars with #missing are also parsed with a limit
// of missing tokens that inserts. The default tier runs the first grammar that
// converts types of each row of the table, the long tier all of them.
func TestTypedValuesMatchEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript parsers")
	}
	node := tooltest.RequireNode(t)
	for _, m := range typedValuesMismatches(t, node, codegentest.TSTypedRows.Cases(t, typedTSCases(t)), nil) {
		t.Error(m)
	}
}

// typedValuesMismatches compares the typed values of cases with the engine's
// (codegentest.TSTypedValuesMismatches) without a limit of missing tokens, which inserts nothing, and
// with a limit that inserts on the cases whose grammars use #missing and on the typed insertion
// grammars (codegentest.TypedInsertionCases). These hold missing values in the typed values of
// accepted inputs, which a grammar that only names the type need not: the first grammar of the corpus
// with the missing type accepts no input.
func typedValuesMismatches(t *testing.T, node string, cases []codegentest.Case, mutate func(string) string) []string {
	t.Helper()
	got := codegentest.TSTypedValuesMismatches(t, node, cases, common.GenOptions{}, 0, mutate)
	var inserting []codegentest.Case
	for _, c := range append(slices.Clone(cases), codegentest.TypedInsertionCases()...) {
		if codegentest.InsertsMissing(t, c) && !slices.ContainsFunc(inserting, func(d codegentest.Case) bool { return d.Name == c.Name }) {
			inserting = append(inserting, c)
		}
	}
	for _, m := range codegentest.TSTypedValuesMismatches(t, node, inserting, common.GenOptions{}, 8, mutate) {
		got = append(got, "missing tokens 8: "+m)
	}
	return got
}

// typedTSCases returns the grammars of the typed Go corpora that have a typed TypeScript parser: the
// grammars that declare a type TypeScript reserves (print.TsReservedTypes) have none.
func typedTSCases(t *testing.T) []codegentest.Case {
	t.Helper()
	var out []codegentest.Case
	for _, c := range append(codegentest.GeneratedTypesCases(t), codegentest.TypedInsertionCorpus()...) {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !slices.ContainsFunc(g.Types(), func(d ast.TypeDecl) bool { return slices.Contains(print.TsReservedTypes, d.DeclName()) }) {
			out = append(out, c)
		}
	}
	return out
}
