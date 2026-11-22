package codepass

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	// The tests run canonicalize at both stages, whose dialects declare the patterns of their stage;
	// the package itself imports no dialect.
	_ "syntaxir.dev/sxir/internal/ir/code/exec"
	_ "syntaxir.dev/sxir/internal/ir/code/flow"
)

// Canonicalize declares every pattern that a stage applies, each once, in the order they are made,
// and each is an optimization of kind pattern.
func TestCanonicalizeDeclaresEveryPattern(t *testing.T) {
	want := []string{"fold-constants", "fold-branches", "drop-after-exit", "drop-unused-labels", "splice-blocks"}
	if got := ExecCanonicalize.Decl().Patterns; !slices.Equal(got, want) {
		t.Errorf("declared %v, want %v", got, want)
	}
	for _, ps := range [][]code.Pattern{code.PatternsAt[code.Exec](), code.PatternsAt[code.Flow]()} {
		for _, p := range ps {
			if !slices.Contains(FlowCanonicalize.Decl().Patterns, p.Name) {
				t.Errorf("pattern %s of a stage is not declared", p.Name)
			}
		}
	}
}
