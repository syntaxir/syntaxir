package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestCorpusVerifiesEveryPass generates a Go parser for every grammar of the corpus with the
// verification after every pass (Debug.VerifyEach), without building or running it. The bulk tests
// generate without it (codegentest.Generate), so this is the default-tier coverage of the per-pass
// verification: a pass that breaks the IR of the corpus fails here at the pass that broke it.
func TestCorpusVerifiesEveryPass(t *testing.T) {
	codegentest.PinOrdinary(t)
	for _, c := range codegentest.CorpusWithParsers(t) {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts := common.GenOptions{Package: "verifyeach", Start: "main", Types: true, TypedOnly: true}
		opts.Debug.VerifyEach = true
		if _, err := codegentest.GenerateCode(t, g, opts); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
	}
}
