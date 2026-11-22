package corepass_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/factstest"
)

// TestFactsGolden computes the facts of every corpus grammar on its Core IR module, with the
// rules' policies cleared so that no fact is read back from compilation, and compares them byte
// for byte with the pinned facts (internal/testing/factstest). It also requires the node queries to
// answer a copy of each node that Analyze did not see as Analyze does (checkUnnumberedNullable).
func TestFactsGolden(t *testing.T) {
	t.Parallel()
	for _, path := range factstest.Corpus {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			g, err := compiletest.Parse(factstest.Read(t, path))
			if err != nil {
				t.Fatal(err)
			}
			prog, err := compiletest.Compile(g, check.Options{})
			if err != nil {
				t.Fatal(err)
			}
			m, err := elaborate.CoreFrom(prog, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range m.Rules {
				r.Policy = core.Policy{}
			}
			f := corepass.Compute(m, nil)
			factstest.Golden(t, path, factstest.Render(factstest.CoreRules(m, f)))
			if err := checkUnnumberedNullable(m, f); err != nil {
				t.Error(err)
			}
		})
	}
}
