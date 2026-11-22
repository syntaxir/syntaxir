//go:build !sxir_budget_controls_reference

package engine

import (
	"context"
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/document"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func budgetDocumentVariants(t *testing.T, run func(*testing.T, ParseOptions)) {
	t.Helper()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%s", b, u), func(t *testing.T) { run(t, ParseOptions{Backend: b, Unit: u}) })
		}
	}
}

func TestBudgetDocumentConstructorCallerPanicIdentity(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = .* $$`)
	budgetDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, at := range []int{0, 1, 2} {
			token := &statetest.ContextPanicToken{Phase: fmt.Sprint(at)}
			o := base
			ctx := statetest.NewContextErrPanic(token, at)
			ctx.Armed = true
			o.Context = ctx
			if at == 0 {
				o.Context = &statetest.ContextDonePanic{Context: context.Background(), Token: token}
			}
			var d *document.Document
			var err error
			got := statetest.CatchPanic(func() { d, err = prog.NewDocumentWith("main", "é💡abc", o) })
			if got != token || d != nil || err != nil {
				t.Fatalf("checkpoint %d: panic=%v document=%v error=%v", at, got, d, err)
			}
			if at > 0 && ctx.Calls != at {
				t.Fatalf("calls=%d want %d", ctx.Calls, at)
			}
		}
	})
}

type budgetDocumentInactiveContext struct{ context.Context }

func (budgetDocumentInactiveContext) Err() error { panic("inactive context must not be polled") }

func TestBudgetDocumentInactiveContext(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "a" $$`)
	budgetDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		o.Context = budgetDocumentInactiveContext{Context: context.Background()}
		d, err := prog.NewDocumentWith("main", "a", o)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if n, err := d.Parse(); n == nil || err != nil {
				t.Fatalf("inactive parse: node=%v error=%v", n, err)
			}
		}
	})
}
