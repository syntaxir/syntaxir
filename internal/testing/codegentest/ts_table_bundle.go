package codegentest

import (
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

var (
	tableBundleOnce sync.Once
	tableBundle     string
	tableBundleErr  error
)

// TSTableBundle returns the TypeScript library as a generated parser of a minimal grammar bundles it, with
// the whole library (the used-features analysis off): the tables of the value conformance suite call
// functions that the parser of this grammar does not use. The harnesses of the tables and their mutants
// mutate this one source, which is generated once per test process instead of once per run.
func TSTableBundle(t testing.TB) string {
	t.Helper()
	tableBundleOnce.Do(func() {
		g, err := compiletest.Parse("@start\nrule main = \"a\"\n")
		if err != nil {
			tableBundleErr = err
			return
		}
		code, err := GenerateTSBundle(g, common.GenOptions{Start: "main", NoOpt: []string{codegen.UsedFeatures}})
		tableBundle, tableBundleErr = string(code), err
	})
	if tableBundleErr != nil {
		t.Fatal(tableBundleErr)
	}
	return tableBundle
}
