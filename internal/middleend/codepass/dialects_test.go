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
	// The pass declares at each stage the patterns of the stage's dialect, in its order, so that a pipeline
	// text lists the patterns that run there.
	for stage, c := range map[string]struct {
		declared []string
		ps       []code.Pattern
	}{
		"Flow": {FlowCanonicalize.Decl().Patterns, code.PatternsAt[code.Flow]()},
		"Exec": {ExecCanonicalize.Decl().Patterns, code.PatternsAt[code.Exec]()},
	} {
		var names []string
		for _, p := range c.ps {
			names = append(names, p.Name)
		}
		if !slices.Equal(c.declared, names) {
			t.Errorf("%s canonicalize declares %v, the dialect has %v", stage, c.declared, names)
		}
	}
}
