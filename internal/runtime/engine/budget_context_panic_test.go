//go:build !sxir_budget_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func budgetContextNodeJSON(t *testing.T, n *value.Node) []byte {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func budgetContextVariants() []ParseOptions {
	var variants []ParseOptions
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			for _, recognize := range []bool{false, true} {
				variants = append(variants, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
			}
		}
	}
	return variants
}

func budgetContextVariantName(opts ParseOptions) string {
	return opts.Backend.String() + "/" + opts.Unit.String() + "/recognize=" + strconv.FormatBool(opts.Recognize)
}

func TestBudgetContextErrPanicIdentityBeforeExecution(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("a", 8192)
	prog := mustCompile(t, `rule main = "`+text+`" $$`)
	for _, base := range budgetContextVariants() {
		for _, phase := range []string{"admission", "later-check", "Done"} {
			t.Run(budgetContextVariantName(base)+"/"+phase, func(t *testing.T) {
				token := &statetest.ContextPanicToken{Phase: phase}
				opts := base
				ctx := statetest.NewContextErrPanic(token, 1)
				ctx.Armed = true
				opts.Context = ctx
				if phase == "later-check" {
					ctx.PanicAt = 3
				}
				if phase == "Done" {
					opts.Context = &statetest.ContextDonePanic{Context: context.Background(), Token: token}
				}
				entries := 0
				opts.Trace = func(e TraceEvent) {
					if e.Kind == TraceEnter {
						entries++
					}
				}
				var node *value.Node
				var returnedErr error
				got := statetest.CatchPanic(func() { node, returnedErr = prog.ParseWith("main", text, opts) })
				if got != token || node != nil || returnedErr != nil || entries != 0 {
					t.Fatalf("panic=%#v node=%v error=%v trace entries=%d", got, node, returnedErr, entries)
				}
				if phase != "Done" && ctx.Calls != ctx.PanicAt {
					t.Fatalf("Err calls=%d want %d at %s", ctx.Calls, ctx.PanicAt, phase)
				}
				// This fixture checks identity before execution, without assigning
				// an ordinal Context call to a specific lazy-setup component.
				// Phase predicates and direct input tests isolate those boundaries.
				retry, err := prog.ParseWith("main", text, base)
				if err != nil || base.Recognize && retry != nil || !base.Recognize && retry == nil {
					t.Fatalf("setup/preparation retry node=%v error=%v", retry, err)
				}
			})
		}
	}
}

func TestBudgetContextErrPanicIdentityAtTraceBoundaries(t *testing.T) {
	t.Parallel()
	childProg := mustCompile(t, `rule main = child $$

rule child = "a"`)
	plainProg := mustCompile(t, `rule main = "a" $$`)
	for _, base := range budgetContextVariants() {
		backend := base.Backend
		t.Run(budgetContextVariantName(base)+"/child precheck", func(t *testing.T) {
			token := &statetest.ContextPanicToken{Phase: "child trace precheck"}
			ctx := statetest.NewContextErrPanic(token, 2)
			var observed *state.Parser
			childCallbacks := 0
			var node *value.Node
			var returnedErr error
			got := statetest.CatchPanic(func() {
				node, returnedErr = childProg.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, Limits: sxirruntime.Limits{Compute: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						observed = state.TraceParser(e)
						ctx.Control, ctx.Armed = state.TraceParser(e).Control, true
					}
					if e.Kind == TraceEnter && e.Rule == "child" {
						childCallbacks++
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.Calls != 2 || childCallbacks != 0 {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d child callback=%d, want panic at child's pre-callback check", got, node, returnedErr, ctx.Calls, childCallbacks)
			}
			statetest.AssertReleased(t, observed)
		})

		t.Run(budgetContextVariantName(base)+"/callback postcheck", func(t *testing.T) {
			token := &statetest.ContextPanicToken{Phase: "trace postcheck"}
			ctx := statetest.NewContextErrPanic(token, 1)
			var observed *state.Parser
			var node *value.Node
			var returnedErr error
			got := statetest.CatchPanic(func() {
				node, returnedErr = plainProg.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, Limits: sxirruntime.Limits{Compute: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						observed = state.TraceParser(e)
						ctx.Control, ctx.Armed = state.TraceParser(e).Control, true
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.Calls != 1 {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d, want exact panic at callback postcheck", got, node, returnedErr, ctx.Calls)
			}
			statetest.AssertReleased(t, observed)
		})
	}
}

func TestBudgetContextErrPanicIdentityAtPublicationAndFailedReservation(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "a" $$`)
	for _, base := range budgetContextVariants() {
		backend := base.Backend
		t.Run(budgetContextVariantName(base)+"/publication", func(t *testing.T) {
			token := &statetest.ContextPanicToken{Phase: "publication"}
			ctx := statetest.NewContextErrPanic(token, 2)
			var observed *state.Parser
			var node *value.Node
			var returnedErr error
			got := statetest.CatchPanic(func() {
				node, returnedErr = prog.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, Limits: sxirruntime.Limits{Compute: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceExit && e.Rule == "main" {
						observed = state.TraceParser(e)
						ctx.Control, ctx.Armed = state.TraceParser(e).Control, true
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.Calls != 2 {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d, want final publication panic", got, node, returnedErr, ctx.Calls)
			}
			statetest.AssertReleased(t, observed)
		})

		t.Run(budgetContextVariantName(base)+"/failed reservation", func(t *testing.T) {
			token := &statetest.ContextPanicToken{Phase: "failed reservation"}
			ctx := statetest.NewContextErrPanic(token, 2)
			var observed *state.Parser
			var node *value.Node
			var returnedErr error
			got := statetest.CatchPanic(func() {
				node, returnedErr = prog.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, Limits: sxirruntime.Limits{Compute: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						observed = state.TraceParser(e)
						ctx.Control = state.TraceParser(e).Control
						ctx.Armed = true
						ctx.Control.Limits.Compute = ctx.Control.Used
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.Calls != 2 || ctx.Control == nil || ctx.AtPanic != ctx.Control.Used || ctx.AtPanic != ctx.Control.Limits.Compute {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d control=%v, want context panic on rejected reservation", got, node, returnedErr, ctx.Calls, ctx.Control)
			}
			statetest.AssertReleased(t, observed)
		})
	}
}

func TestBudgetContextErrPanicIdentityAtCountedBudgetAndRetry(t *testing.T) {
	t.Parallel()
	literal := strings.Repeat("a", 1024)
	prog := mustCompile(t, `rule main = "`+literal+`" $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					var retained *value.Node
					var retainedJSON []byte
					if !recognize {
						var err error
						retained, err = prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || retained == nil {
							t.Fatalf("baseline parse node=%v err=%v", retained, err)
						}
						retainedJSON = budgetContextNodeJSON(t, retained)
					}

					token := &statetest.ContextPanicToken{Phase: "spent budget"}
					ctx := statetest.NewContextErrPanic(token, 2)
					var observed *state.Parser
					var node *value.Node
					var returnedErr error
					got := statetest.CatchPanic(func() {
						node, returnedErr = prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, Context: ctx, Limits: sxirruntime.Limits{Compute: 1 << 30}, Trace: func(e TraceEvent) {
							if e.Kind == TraceEnter && e.Rule == "main" {
								observed = state.TraceParser(e)
								ctx.Control, ctx.Armed = state.TraceParser(e).Control, true
							}
						}})
					})
					if got != token || node != nil || returnedErr != nil {
						t.Fatalf("panic=%#v node=%v error=%v, want exact panic during spent budget", got, node, returnedErr)
					}
					if ctx.Calls != 2 || ctx.Since != state.BudgetCheckInterval-1 || ctx.AtPanic < state.BudgetCheckInterval-1 {
						t.Fatalf("Err calls=%d steps=%d since check=%d, want callback postcheck then next counted-budget poll", ctx.Calls, ctx.AtPanic, ctx.Since)
					}
					statetest.AssertReleased(t, observed)

					if !recognize {
						if string(budgetContextNodeJSON(t, retained)) != string(retainedJSON) {
							t.Fatal("caller panic mutated a previously retained Node")
						}
						retry, err := prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || string(budgetContextNodeJSON(t, retry)) != string(retainedJSON) || string(budgetContextNodeJSON(t, retained)) != string(retainedJSON) {
							t.Fatalf("panic/retry changed result or retained Node: node=%v err=%v", retry, err)
						}
					} else if n, err := prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit, Recognize: true}); n != nil || err != nil {
						t.Fatalf("recognition retry node=%v err=%v", n, err)
					}
				})
			}
		}
	}
}

func TestBudgetContextErrPanicIdentityLoadedASTOmittedProgram(t *testing.T) {
	t.Parallel()
	literal := strings.Repeat("a", 1024)
	source := mustCompile(t, `rule main = "`+literal+`" $$`)
	// The parse below is traced, which a compiled grammar serves in document mode.
	prog := saveLoad(t, source, "main", CompiledDocument)
	if prog.Static() != nil {
		t.Fatal("loaded fixture retained an AST")
	}

	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				retained, err := prog.ParseWith("main", literal, servedBy(CompiledDocument, ParseOptions{Backend: backend, Unit: unit}))
				if err != nil || retained == nil {
					t.Fatalf("baseline node=%v err=%v", retained, err)
				}
				before := budgetContextNodeJSON(t, retained)

				token := &statetest.ContextPanicToken{Phase: "loaded spent budget"}
				ctx := statetest.NewContextErrPanic(token, 2)
				var observed *state.Parser
				var node *value.Node
				var returnedErr error
				got := statetest.CatchPanic(func() {
					node, returnedErr = prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit, Context: ctx, Limits: sxirruntime.Limits{Compute: 1 << 30}, Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							observed = state.TraceParser(e)
							ctx.Control, ctx.Armed = state.TraceParser(e).Control, true
						}
					}})
				})
				if got != token || node != nil || returnedErr != nil || ctx.Calls != 2 {
					t.Fatalf("panic=%#v node=%v error=%v Err calls=%d, want exact loaded-VM panic", got, node, returnedErr, ctx.Calls)
				}
				if ctx.Since != state.BudgetCheckInterval-1 || ctx.AtPanic < state.BudgetCheckInterval-1 {
					t.Fatalf("steps at panic=%d since check=%d, want counted-spend polling boundary", ctx.AtPanic, ctx.Since)
				}
				statetest.AssertReleased(t, observed)

				retry, retryErr := prog.ParseWith("main", literal, servedBy(CompiledDocument, ParseOptions{Backend: backend, Unit: unit}))
				if retryErr != nil || string(budgetContextNodeJSON(t, retry)) != string(before) || string(budgetContextNodeJSON(t, retained)) != string(before) {
					t.Fatalf("loaded retry node=%v err=%v; retained=%v", retry, retryErr, retained)
				}
			})
		}
	}
}
