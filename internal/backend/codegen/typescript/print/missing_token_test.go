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

// TestMissingTokensStreamRejected checks that a stream, alone or beside a Document, is refused for
// a grammar with #missing like every other generated TypeScript parser: generated TypeScript parsers
// do not insert missing tokens, so the insertion operations of the stream table are never printed.
func TestMissingTokensStreamRejected(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = item* #stream $$
rule item = "a" #missing ";"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []common.GenOptions{
		{Start: "main", Stream: true},
		{Start: "main", Stream: true, Document: true},
		{Start: "main", Stream: true, Types: true, Observability: true},
	} {
		if _, err := generateTS(g, opts, driver.Options{}, nil); err == nil || !strings.Contains(err.Error(), "missing-token recovery is not supported for generated TypeScript parsers; the engine and generated Go parsers support it") {
			t.Errorf("%+v: generated a stream parser for the grammar: %v", opts, err)
		}
	}
}
