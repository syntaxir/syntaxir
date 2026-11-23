package engine

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// missingCorpus returns the test grammars that use #missing, with their inputs: the examined
// shapes that use it and the action cases over missing tokens.
func missingCorpus() []codegentest.Case {
	var out []codegentest.Case
	for _, c := range codegentest.ExaminedShapes() {
		if strings.Contains(c.Src, "#missing") {
			out = append(out, c)
		}
	}
	for _, cases := range [][]codegentest.ActionCase{
		codegentest.MissingActionCases(),
		codegentest.MissingFieldActionCases(),
		codegentest.RecoveredStructActionCases(true),
	} {
		for _, c := range cases {
			out = append(out, c.Case)
		}
	}
	return out
}

// TestBackendsMatchOnMissingWithoutInsertion checks that the bytecode VMs parse and recognize the
// grammars that use #missing as the closure engine does when the parse inserts no token: a literal
// with #missing is then strict.
func TestBackendsMatchOnMissingWithoutInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range missingCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			prog := mustCompile(t, c.Src)
			if !prog.HasMissing {
				t.Fatal("the grammar does not use #missing")
			}
			for _, in := range c.Inputs {
				checkBackends(t, prog, "main", in)
			}
		})
	}
}
