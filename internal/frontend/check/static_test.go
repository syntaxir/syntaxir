package check

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tests of this package check grammars to their static programs (Check), which builds no
// matchers: they check what the compiler decides, which the engine's tests check by running it.

// parseTest parses SyntaxIR source for a test whose subject is not start rules: unless the source has
// declaration attributes, every rule becomes a start rule, the first one the default.
func parseTest(src string) (*ast.Grammar, error) {
	g, err := parser.Parse(src)
	return starttest.MarkAll(g), err
}

// compileStatic compiles g as the engine does, without building matchers; flags, if not nil,
// replace static analysis and type checking.
func compileStatic(g *ast.Grammar, opts Options, flags []RuleFlags) (*Program, error) {
	names := desugar.InlineNames{}
	p, err := Check(g, opts, flags, names, nil)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return p, nil
}

// compilePollContext cancels at a deterministic poll, without timing or global compiler hooks.
type compilePollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *compilePollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

// compileTestContext returns a context that is cancelled at its polls-th check.
func compileTestContext(t *testing.T, polls int) *compilePollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &compilePollContext{Context: ctx, cancel: cancel, left: polls}
}

// checksWithinSteps compiles g like compileStatic and reports whether it finishes within limit steps of
// the compiler (cancel.Control). The work is counted in steps, not time, so the answer is the same on
// every run and under any load; a compilation that needs more steps is cancelled at the limit.
func checksWithinSteps(t *testing.T, g *ast.Grammar, limit uint32) (finished bool) {
	t.Helper()
	control := &cancel.Control{Ctx: compileTestContext(t, int(limit/cancel.PollStride))}
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(cancel.Cancellation); !ok {
				panic(failure)
			}
			finished = false
		}
	}()
	if _, err := Check(g, Options{}, nil, desugar.InlineNames{}, control); err != nil {
		t.Fatal(err)
	}
	return true
}
