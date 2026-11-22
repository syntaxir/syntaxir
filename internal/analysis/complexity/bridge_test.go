package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestClassifierNodeBridge requires, for every corpus grammar classified from its default start
// rule with every rule a start rule, that every expression of the cost view that the classifier
// reads stands for a Core IR node (classifier.nodes), and that the facts of the node agree with
// what the classifier's rule analysis answers for the expression: whether it is nullable and
// whether it certainly succeeds.
func TestClassifierNodeBridge(t *testing.T) {
	t.Parallel()
	for _, path := range factstest.Corpus {
		g, err := parser.Parse(factstest.Read(t, path))
		if err != nil {
			continue
		}
		k, _, err := prepareClassifier(starttest.MarkAll(g), "", nil)
		if err != nil {
			continue
		}
		k.classify()
		f := k.facts.source.Facts
		for _, r := range k.rules {
			astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
				n, ok := k.nodes[e]
				if !ok {
					t.Errorf("%s: rule %s: no node for %T %s", path, r.Name, e, k.show(e))
					return
				}
				if got, want := f.Nullable(n), k.an.isNullable(e); got != want {
					t.Errorf("%s: rule %s: %T %s as %T: nullable %v, want %v", path, r.Name, e, k.show(e), n, got, want)
				}
				if got, want := f.Succeeds(n), k.an.AlwaysSucceeds(e); got != want {
					t.Errorf("%s: rule %s: %T %s as %T: succeeds %v, want %v", path, r.Name, e, k.show(e), n, got, want)
				}
			})
		}
	}
}
