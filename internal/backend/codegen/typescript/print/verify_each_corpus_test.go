package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestCorpusVerifiesEveryPass generates a TypeScript parser for every grammar of the corpus, in the default-tier
// option sets of codegentest.VerifyVariants, with the verification after every pass (Debug.VerifyEach), without
// building or running it. The bulk tests generate without it (codegentest.GenerateTSRaw), so this is the
// default-tier coverage of the per-pass verification: a pass that breaks the IR of the corpus fails here at the pass
// that broke it. A grammar in codegentest.TSRefused must still be refused with its listed refusal. The other option
// sets run in TestCorpusVerifiesEveryPassLong (build tag sxir_long).
func TestCorpusVerifiesEveryPass(t *testing.T) {
	verifyCorpus(t, func(v codegentest.VerifyVariant) bool { return !v.Long }, true)
}

// verifyCorpus generates every corpus grammar in the option sets that keep returns true for. With refusals set it
// also asserts the refusals of codegentest.TSRefused instead of verifying those grammars.
func verifyCorpus(t *testing.T, keep func(codegentest.VerifyVariant) bool, refusals bool) {
	for _, c := range codegentest.CorpusWithParsers(t) {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if want, ok := codegentest.TSRefused[c.Name]; ok {
			if !refusals {
				continue
			}
			opts := common.GenOptions{Start: "main", Types: true, TypedOnly: true}
			if _, err := codegentest.GenerateTSRaw(g, opts); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: want the refusal %q, got %v", c.Name, want, err)
			}
			continue
		}
		for _, v := range codegentest.VerifyVariants() {
			if !keep(v) || (v.Stream && !strings.Contains(c.Src, "#stream")) {
				continue
			}
			opts := v.Opts
			opts.Start = "main"
			opts.Debug.VerifyEach = true
			generate := codegentest.GenerateTSRaw
			if v.Stream {
				generate = codegentest.GenerateTSStream
			}
			_, err := generate(g, opts)
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
