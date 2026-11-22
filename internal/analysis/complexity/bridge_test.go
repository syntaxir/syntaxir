package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestClassifierNodeBridge requires, for every corpus grammar classified from its default start
// rule with every rule a start rule, that every expression of the cost view that the classifier
// reads stands for a Core IR node (classifier.nodes), whose facts the classifier reads, and that
// every node of the rule bodies stands for an expression (classifier.exprs) that stands for the
// node again. TestClassifierHelperAnswers pins what they answer.
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
		w := writtenOf(t, k)
		for _, r := range w.rules {
			astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
				if _, ok := w.nodes[e]; !ok {
					t.Errorf("%s: rule %s: no node for %T %s", path, r.Name, e, w.show(e))
				}
			})
			core.Walk(w.nodes[r.Expr], func(n core.Node) {
				if e, ok := w.exprs[n]; !ok {
					t.Errorf("%s: rule %s: no expression for %T", path, r.Name, n)
				} else if w.nodes[e] != n {
					t.Errorf("%s: rule %s: the expression %s of a %T stands for a %T", path, r.Name, w.show(e), n, w.nodes[e])
				}
			})
		}
	}
}
