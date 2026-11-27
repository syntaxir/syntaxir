//go:build sxir_long

package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestCorpusVerifiesEveryPassLong runs the long-tier option sets of codegentest.VerifyVariants (class-tables at the
// opt-in threshold, Recognize, Document, Stream) with the verification after every pass, as
// TestCorpusVerifiesEveryPass does for the default-tier sets.
func TestCorpusVerifiesEveryPassLong(t *testing.T) {
	verifyCorpus(t, func(v codegentest.VerifyVariant) bool { return v.Long })
}
