package archtest

import (
	"slices"
	"strings"
	"testing"
)

// semanticModelPackage holds the semantic model of the front end (internal/frontend/check): the
// checker, its value types and the checks that the compiler makes of a grammar.
const semanticModelPackage = modulePath + "/internal/frontend/check"

// analysesRoot is the directory of the grammar analyses: every package under it is an analysis.
const analysesRoot = modulePath + "/internal/analysis/"

// analysesImportingSemanticModel returns, sorted, the analysis packages of g whose non-test files
// import the semantic model, and the number of analysis packages in g.
func analysesImportingSemanticModel(g *graph) (bad []string, n int) {
	for path, p := range g.pkgs {
		if !strings.HasPrefix(path, analysesRoot) {
			continue
		}
		n++
		if slices.Contains(p.Imports, semanticModelPackage) {
			bad = append(bad, path)
		}
	}
	slices.Sort(bad)
	return bad, n
}

// TestAnalysesImportNoSemanticModel checks, in every build, that no non-test file of a package
// under internal/analysis imports the semantic model of the front end. The analyses read only the
// result of the front end (internal/frontend) and the facts of Core IR and of the run-time program:
// the checked types of a grammar reach them on its structural module (spec/core-ir/modules.md,
// Structural modules). Their tests may still build a grammar with the checker's options.
func TestAnalysesImportNoSemanticModel(t *testing.T) {
	for _, g := range loadAll(t) {
		bad, n := analysesImportingSemanticModel(g)
		// Guard against a check that passes because it finds no analysis.
		for _, want := range []string{"complexity", "lint"} {
			if g.pkgs[analysesRoot+want] == nil {
				t.Fatalf("build %q: no package %s%s", g.tags, analysesRoot, want)
			}
		}
		for _, path := range bad {
			t.Errorf("build %q: %s imports the semantic model (%s); read the result of internal/frontend and the facts of Core IR", g.tags, path, semanticModelPackage)
		}
		t.Logf("build %q: %d analysis packages", g.tags, n)
	}
}

// TestAnalysesImportNoSemanticModelNegativeControl adds an import of the semantic model to an
// analysis of the default build's graph and checks that the rule reports exactly that package.
func TestAnalysesImportNoSemanticModelNegativeControl(t *testing.T) {
	g := loadAll(t)[0].clone()
	if g.tags != "" {
		t.Fatalf("the first graph is of build %q, not the default build", g.tags)
	}
	lint := analysesRoot + "lint"
	p := g.pkgs[lint]
	if p == nil {
		t.Fatalf("no package %s", lint)
	}
	if slices.Contains(p.Imports, semanticModelPackage) {
		t.Fatalf("%s already imports %s", lint, semanticModelPackage)
	}
	p.Imports = append(p.Imports, semanticModelPackage)
	if bad, _ := analysesImportingSemanticModel(g); !slices.Equal(bad, []string{lint}) {
		t.Errorf("an added import of %s by %s: the rule reports %v, want [%s]", semanticModelPackage, lint, bad, lint)
	}
}
