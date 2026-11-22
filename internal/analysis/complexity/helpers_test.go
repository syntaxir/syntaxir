package complexity

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/core"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// compileStatic compiles g as the engine does, without building matchers.
func compileStatic(g *ast.Grammar) (*frontend.Result, error) {
	return compiletest.Compile(g, check.Options{})
}

// skipOf returns the skip expression of e if e is an expression with #recover, or nil.
func skipOf(e ast.Expr) ast.Expr {
	switch e := e.(type) {
	case *ast.Recovery:
		return e.Skip
	case *ast.Attributed:
		return astfacts.RecoverSkip(e)
	}
	return nil
}

// writtenOf returns the rules of the view that k reads written as grammar rules, with the node
// that each of their expressions stands for and the expression of each node (View.written).
func writtenOf(t testing.TB, k *classifier) *writtenView {
	t.Helper()
	rules := make([]*ast.RuleDef, len(k.rules))
	for i, r := range k.rules {
		rules[i] = &ast.RuleDef{Name: r.Name}
	}
	w, err := k.view.written(rules)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// show describes the expression e of the written view for a message, as the source text that it
// stands for.
func (w *writtenView) show(e ast.Expr) string {
	if t, ok := w.text[e]; ok {
		return abbreviate(t)
	}
	return abbreviate(syntaxfmt.FormatExpr(e))
}

// firstNode returns the first node of type T in a preorder walk of n (core.Walk), or nil.
func firstNode[T core.Node](n core.Node, ok func(T) bool) T {
	var first T
	found := false
	core.Walk(n, func(x core.Node) {
		if y, is := x.(T); is && !found && (ok == nil || ok(y)) {
			first, found = y, true
		}
	})
	return first
}

// pollContext cancels at a deterministic poll, without timing or global hooks.
type pollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *pollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

// cancelledAt returns a context that is cancelled at its polls-th check.
func cancelledAt(t *testing.T, polls int) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &pollContext{Context: ctx, cancel: cancel, left: polls}
}
