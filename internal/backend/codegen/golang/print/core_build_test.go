package print_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	irbuild "syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// TestCoreBuildCorpus builds, analyzes and verifies the Core IR of every grammar of the corpus
// and of testdata/grammars, and checks that the scope of every rule is the frame layout the
// generated Node rules use, which the typed rules share.
func TestCoreBuildCorpus(t *testing.T) {
	t.Parallel()
	cases := codegentest.CorpusWithParsers(t)
	built := 0
	for _, ok := range codegentest.MapParallel(len(cases), func(i int) bool { return checkCoreBuild(t, cases[i]) }) {
		if ok {
			built++
		}
	}
	if built < 100 {
		t.Fatalf("built %d grammars", built)
	}
}

// checkCoreBuild builds, analyzes and verifies the Core IR of the grammar c and compares its scopes
// with the generated Node rules. It reports whether it built the grammar.
func checkCoreBuild(t *testing.T, c codegentest.Case) bool {
	prog, ok := enginetest.CorpusProgram(t, c)
	if !ok {
		return false
	}
	m, err := irbuild.CoreFrom(entry.Checked(prog), false)
	if err != nil {
		t.Errorf("%s: %v", c.Name, err)
		return false
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Errorf("%s: %v", c.Name, err)
		return false
	}
	g, ok := coreGenerate(c.Src, common.GenOptions{})
	if !ok {
		return false
	}
	for _, r := range typedRules(g) {
		if _, ok := m.Rules[r.ID].Body.(*core.OperatorsRule); ok {
			continue
		}
		if got, want := core.SlotNames(m.Rules[r.ID].Scope), g.NodeScopes[r]; !slices.Equal(got, want) {
			t.Errorf("%s: rule %s (%d): scope %v, generated Node rule %v", c.Name, r.Name, r.ID, got, want)
		}
	}
	return true
}
