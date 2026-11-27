package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestCorpusVerifiesEveryPass generates a TypeScript parser for every grammar of the corpus with the
// verification after every pass (Debug.VerifyEach), without building or running it. The bulk tests
// generate without it (codegentest.GenerateTSRaw), so this is the default-tier coverage of the per-pass
// verification: a pass that breaks the IR of the corpus fails here at the pass that broke it.
func TestCorpusVerifiesEveryPass(t *testing.T) {
	for _, c := range codegentest.CorpusWithParsers(t) {
		// The TypeScript backend refuses a grammar whose type names a type of its runtime (types.ts), by
		// design; the other cases of the corpus still verify.
		if strings.HasSuffix(c.Name, "spanname.sxir") {
			continue
		}
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts := common.GenOptions{Start: "main", Types: true, TypedOnly: true}
		opts.Debug.VerifyEach = true
		if _, err := codegentest.GenerateTSRaw(g, opts); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
	}
}
