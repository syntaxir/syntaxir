package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestWrittenViewRoundTrip requires, for every corpus grammar classified from its default start
// rule with every rule a start rule, that every expression of the cost view that the classifier
// reads, written as grammar rules (View.written), is the expression of its own node:
// exprs[nodes[e]] == e. TestClassifierHelperAnswers names each node by the expression that it is
// written as, so each expression must name the node that it is written from.
func TestWrittenViewRoundTrip(t *testing.T) {
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
		w := writtenOf(t, k)
		for _, r := range w.rules {
			astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
				if back := w.exprs[w.nodes[e]]; back != e {
					t.Errorf("%s: rule %s: the node of %T %s stands for %T %s", path, r.Name, e, w.show(e), back, w.show(back))
				}
			})
		}
	}
}
