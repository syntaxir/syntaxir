package corepass_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestNullableAtAndBeforeEnd checks the nullability of rules at the end of the input and before
// it, as rule facts and as the node queries on the rule's body. A call takes the fact of its rule
// for the position it is asked about: main calls a, whose nullability differs at and before the
// end, so main's answers before the end are the negative control of a call that answered with the
// rule's nullability at the end.
func TestNullableAtAndBeforeEnd(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src              string
		atEnd, beforeEnd bool
	}{
		{src: "rule main = a\nrule a = $$", atEnd: true},
		{src: "rule main = a\nrule a = !$$", beforeEnd: true},
		{src: "rule main = a\nrule a = &.", beforeEnd: true},
		{src: "rule main = a a\nrule a = \"x\"?", atEnd: true, beforeEnd: true},
		{src: "rule main = a \"x\"\nrule a = $$"},
		{src: "rule main = a / b\nrule a = $$\nrule b = !$$", atEnd: true, beforeEnd: true},
	} {
		g, err := compiletest.Parse(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		m, err := elaborate.Structural(compile.StructuralInput(g, g.Rules(), nil), false)
		if err != nil {
			t.Fatal(err)
		}
		f := corepass.Compute(m, nil)
		main := m.Rules[0]
		if got := f.Rules[main.ID]; got.NullableAtEnd != tc.atEnd || got.NullableBeforeEnd != tc.beforeEnd {
			t.Errorf("%q: rule facts at end %v, before end %v; want %v, %v",
				tc.src, got.NullableAtEnd, got.NullableBeforeEnd, tc.atEnd, tc.beforeEnd)
		}
		if got := f.NullableAtEnd(main.Body); got != tc.atEnd {
			t.Errorf("%q: body NullableAtEnd %v, want %v", tc.src, got, tc.atEnd)
		}
		if got := f.NullableBeforeEnd(main.Body); got != tc.beforeEnd {
			t.Errorf("%q: body NullableBeforeEnd %v, want %v", tc.src, got, tc.beforeEnd)
		}
	}
}

// TestFactsWithControl requires the node queries of facts given another control to poll it, and
// the facts they were made from to keep their own.
func TestFactsWithControl(t *testing.T) {
	t.Parallel()
	g, err := compiletest.Parse("rule main = " + strings.Repeat("_ ", 64))
	if err != nil {
		t.Fatal(err)
	}
	m, err := elaborate.Structural(compile.StructuralInput(g, g.Rules(), nil), false)
	if err != nil {
		t.Fatal(err)
	}
	f := corepass.Compute(m, nil)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	polled := f.WithControl(&cancel.Control{Ctx: ctx})
	func() {
		defer func() {
			if c, ok := recover().(cancel.Cancellation); !ok || !errors.Is(c.Err, context.Canceled) {
				t.Errorf("a query of the facts with a cancelled control did not stop: %v", c)
			}
		}()
		polled.Succeeds(m.Rules[0].Body)
	}()
	if !f.Succeeds(m.Rules[0].Body) || polled.Rules[0].Succeeds != f.Rules[0].Succeeds {
		t.Errorf("the facts changed with the control of their copy")
	}
}
