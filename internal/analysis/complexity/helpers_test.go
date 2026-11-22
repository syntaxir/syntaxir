package complexity

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/check"
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
