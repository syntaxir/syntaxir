package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
)

// TestMissingTokensInsertionOnlyLeftRecursionRejected checks that a grammar whose left recursion
// exists only through an insertion is rejected like any grammar with #missing.
func TestMissingTokensInsertionOnlyLeftRecursionRejected(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = a $$
rule a = "(" #missing a "x" / "y"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generateTS(g, common.GenOptions{Start: "main"}, driver.Options{}, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatal("generated a parser for the grammar:", err)
	}
}
