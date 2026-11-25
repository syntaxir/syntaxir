package complexity

import (
	"context"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// complexitySinkKey is the context key of the function that WithComplexitySink installs.
type complexitySinkKey struct{}

// WithComplexitySink returns a context under which ClassifyComplexity passes each classification
// it completes to sink. A caller that runs the lint checks with the classification requested
// gets in this way the classification behind the super-linear findings, without classifying the
// grammar again.
func WithComplexitySink(ctx context.Context, sink func(*Complexity)) context.Context {
	return context.WithValue(ctx, complexitySinkKey{}, sink)
}

// ClassifyComplexity is ClassifyComplexityControlled under ctx: it returns ctx's error, and no
// classification, if ctx ends. It passes the classification to the sink of ctx, if any (see
// WithComplexitySink).
func ClassifyComplexity(ctx context.Context, g *ast.Grammar, start string) (*Complexity, error) {
	return sunk(ctx, func(control *cancel.Control) (*Complexity, error) {
		return ClassifyComplexityControlled(g, start, control)
	})
}

// ClassifyChecked is ClassifyComplexity of the grammar whose front-end result is res, which it
// does not check again.
func ClassifyChecked(ctx context.Context, res *frontend.Result, start string) (*Complexity, error) {
	return sunk(ctx, func(control *cancel.Control) (*Complexity, error) {
		return ClassifyCheckedControlled(res, start, control)
	})
}

// GrowingBindings is GrowingBindingsControlled under ctx: it returns the Growing member of
// ClassifyComplexity(ctx, g, start) at a lower cost.
func GrowingBindings(ctx context.Context, g *ast.Grammar, start string) ([]GrowingBinding, error) {
	return underContext(ctx, func(control *cancel.Control) ([]GrowingBinding, error) {
		return GrowingBindingsControlled(g, start, control)
	})
}

// GrowingBindingsChecked is GrowingBindings of the grammar whose front-end result is res, which it
// does not check again.
func GrowingBindingsChecked(ctx context.Context, res *frontend.Result, start string) ([]GrowingBinding, error) {
	return underContext(ctx, func(control *cancel.Control) ([]GrowingBinding, error) {
		return GrowingBindingsCheckedControlled(res, start, control)
	})
}

// sunk runs classify under ctx (underContext) and passes the classification to the sink of ctx,
// if any.
func sunk(ctx context.Context, classify func(*cancel.Control) (*Complexity, error)) (*Complexity, error) {
	c, err := underContext(ctx, classify)
	if sink, ok := ctx.Value(complexitySinkKey{}).(func(*Complexity)); ok && err == nil {
		sink(c)
	}
	return c, err
}

// underContext runs f with the control of ctx (cancel.New), or with none if ctx can neither end nor
// carries a meter, and returns ctx's error, and no result, if ctx ends.
func underContext[T any](ctx context.Context, f func(*cancel.Control) (T, error)) (out T, err error) {
	if err := ctx.Err(); err != nil {
		return out, err
	}
	control := cancel.New(ctx)
	if control == nil {
		return f(nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(cancel.Cancellation); ok {
				var zero T
				out, err = zero, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	return f(control)
}
