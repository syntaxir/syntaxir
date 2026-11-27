package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestCorpusVerifiesEveryPass generates a Go parser for every grammar of the corpus, in every option set of
// codegentest.VerifyVariants, with the verification after every pass (Debug.VerifyEach), without building or
// running it. The bulk tests generate without it (codegentest.GenerateRaw), so this is the default-tier coverage
// of the per-pass verification: a pass that breaks the IR of the corpus fails here at the pass that broke it.
func TestCorpusVerifiesEveryPass(t *testing.T) {
	codegentest.PinOrdinary(t)
	for _, c := range codegentest.CorpusWithParsers(t) {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, v := range codegentest.VerifyVariants() {
			if v.Stream && !strings.Contains(c.Src, "#stream") {
				continue
			}
			opts := v.Opts
			opts.Package = "verifyeach"
			opts.Start = "main"
			opts.Debug.VerifyEach = true
			_, err := codegentest.GenerateCode(t, g, opts)
			// A #stream repetition that is not at the top level of the start rule is refused by design (as in
			// codegentest.reference); only that refusal skips the stream variant.
			if v.Stream && err != nil && strings.Contains(err.Error(), "needs a #stream repetition") {
				continue
			}
			if err != nil {
				t.Errorf("%s (%s): %v", c.Name, v.Name, err)
			}
		}
	}
}
