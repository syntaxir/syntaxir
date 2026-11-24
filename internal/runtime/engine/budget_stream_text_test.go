//go:build !sxir_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestBudgetStreamTextPublicTraceInterruption(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = (!"\n" .)+ #atomic "\n"`)
	budgetStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"budget", "cancel"} {
			t.Run(phase, func(t *testing.T) {
				text := strings.Repeat("aé😀\xff", 350) + "\n" + strings.Repeat("b", 100) + "\n"
				o := base
				o.Limits.Compute = 1 << 30
				var saved TraceEvent
				var previous state.StreamTextSnapshot
				armed := false
				emissions := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o.Trace = func(e TraceEvent) {
					if state.TraceParser(saved) == nil {
						saved = e
					}
					if !armed && e.Kind == TraceEnter && e.Rule == "row" {
						// The rule has not captured text yet. Fill loads input
						// explicitly; Text on the entry exercises materialization.
						p := state.TraceParser(e)
						p.Fill(p.Pos + 64)
						previous = p.StreamText
						armed = true
						if phase == "budget" {
							p.Control.Limits.Compute = p.Control.Used + 1
						} else {
							cancel()
							p.Control.Ctx = ctx
						}
						_ = p.Text(p.Pos, p.Pos+64)
					}
				}
				err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { emissions++; return nil }, o)
				want := runtime.ErrLimit
				if phase == "cancel" {
					want = context.Canceled
				}
				if !armed || !errors.Is(err, want) || emissions != 0 {
					t.Fatalf("phase=%s armed=%t error=%v emits=%d old=%+v", phase, armed, err, emissions, previous)
				}
				statetest.AssertTraceParserDetached(t, state.TraceParser(saved), false)
				if err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { return nil }, base); err != nil {
					t.Fatalf("fresh replay: %v", err)
				}
			})
		}
	})
}
