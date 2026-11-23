package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestBackendsMatchOnMissingWithoutInsertion checks that the bytecode VMs parse and recognize the
// grammars that use #missing as the closure engine does when the parse inserts no token: a literal
// with #missing is then strict.
func TestBackendsMatchOnMissingWithoutInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.InsertionCorpus() {
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
