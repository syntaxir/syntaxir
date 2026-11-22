//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func diagnosticAlternatives() string {
	var src strings.Builder
	src.WriteString("rule main = bad $$\nrule bad = ")
	for i := 0; i < 16; i++ {
		if i > 0 {
			src.WriteString(" / ")
		}
		src.WriteString(strconv.Quote(strings.Repeat("long_common_prefix_", 60) + strconv.Itoa(i)))
	}
	return src.String()
}

func TestBudgetDiagnosticBatchFailureAndTraceAccessor(t *testing.T) {
	t.Parallel()
	source := diagnosticAlternatives()
	prog := enginetest.MustCompile(t, source)
	accessorProg := enginetest.MustCompile(t, strings.Replace(source, "rule main = bad $$\n", `rule main = (bad #recover(skip=.* #atomic) "!" / fallback) $$`+"\n", 1)+"\nrule fallback = .* #atomic\n")
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, accessor := range []bool{false, true} {
					active := prog
					if accessor {
						active = accessorProg
					}
					for _, cancel := range []bool{false, true} {
						if cancel && !accessor {
							continue
						}
						armed, accessorReturned := false, false
						fallback := 0
						var observed *state.Parser
						var c *state.BudgetControl
						var at uint64
						n, err := active.ParseWith("main", "x", engine.ParseOptions{Expectations: true, Backend: b, Unit: u, Recognize: recognize, Limits: runtime.Limits{Compute: 1 << 50}, Trace: func(e engine.TraceEvent) {
							observed = state.TraceParser(e)
							if e.Kind == engine.TraceEnter && e.Rule == "fallback" {
								fallback++
							}
							if !armed && e.Kind == engine.TraceExit && ((accessor && e.Rule == "bad") || (!accessor && e.Rule == "main")) {
								if e.Matched || len(state.TraceExpectations(e)) != 16 {
									t.Fatalf("unexpected diagnostic fixture exp=%d", len(state.TraceExpectations(e)))
								}
								armed, c, at = true, state.TraceParser(e).Control, state.TraceParser(e).Control.Used
								// The limit leaves no room: the accessor and the descriptions and
								// sort of the error count nothing, so the parse stops at its next unit,
								// the error it builds or the recovery that the accessor variant makes.
								if accessor {
									if cancel {
										c.Ctx = statetest.NewPollingContext(1)
										c.SinceCheck = 0
									} else {
										c.Limits.Compute = at
									}
									_ = e.Failure()
									accessorReturned = true
								} else {
									c.Limits.Compute = at
								}
							}
						}})
						var want error = runtime.ErrLimit
						if cancel {
							want = context.Canceled
						}
						if !armed || n != nil || !errors.Is(err, want) || accessor != accessorReturned || fallback != 0 || observed == nil || observed.Scratch != nil || observed.Control != nil {
							t.Fatalf("%v/%v recognize=%t accessor=%t cancel=%t: armed=%t node=%t err=%v returned=%t fallback=%d", b, u, recognize, accessor, cancel, armed, n != nil, err, accessorReturned, fallback)
						}
						if !accessor && c.Used != at+1 {
							t.Fatalf("the failed parse counted %d units after main, want the one of its error", c.Used-at)
						}
						n, err = active.ParseWith("main", "x", engine.ParseOptions{Expectations: true, Backend: b, Unit: u, Recognize: recognize})
						if accessor {
							if err != nil || (recognize && n != nil) || (!recognize && n == nil) {
								t.Fatalf("accessor fresh retry differs: %v", err)
							}
						} else {
							var syntax *value.SyntaxError
							if n != nil || !errors.As(err, &syntax) || len(syntax.Expected) != 16 {
								t.Fatalf("failure fresh retry differs: %v", err)
							}
						}
					}
				}
			}
		}
	}
}

func TestBudgetDiagnosticRecoveryFatalAndRetainedOutputs(t *testing.T) {
	t.Parallel()
	message := strings.Repeat("recovery_message_λ", 100)
	// Stop before the trailing exclamation mark, including during the retry.
	src := `rule main = (bad #recover(skip=(!"!" .)+) "!" / fallback) $$
rule fallback = .* #atomic
rule bad = "a" @error(message=` + strconv.Quote(message) + `)`
	prog := enginetest.MustCompile(t, src)
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			retained, retainedErr := prog.ParseWith("main", "x!", engine.ParseOptions{Expectations: true, Backend: b, Unit: u})
			if retained == nil || retainedErr == nil {
				t.Fatal("recovery setup did not produce node and errors")
			}
			beforeNode, _ := json.Marshal(retained)
			beforeErr, _ := json.Marshal(retainedErr)
			for _, recognize := range []bool{false, true} {
				armed, fallback := false, 0
				var observed *state.Parser
				n, err := prog.ParseWith("main", "x", engine.ParseOptions{Expectations: true, Backend: b, Unit: u, Recognize: recognize, Limits: runtime.Limits{Compute: 1 << 50}, Trace: func(e engine.TraceEvent) {
					observed = state.TraceParser(e)
					if e.Kind == engine.TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if !armed && e.Kind == engine.TraceExit && e.Rule == "bad" {
						armed = true
						// The skip advances over one unit of input; a parse then counts one unit for
						// the error it builds, which this limit leaves no room for, and its message
						// counts nothing. Recognition builds no error, so it goes on to the fallback.
						room := uint64(1)
						if recognize {
							room = 256
						}
						state.TraceParser(e).Control.Limits.Compute = state.TraceParser(e).Control.Used + room
					}
				}})
				if !armed || n != nil || observed.Scratch != nil || observed.Control != nil {
					t.Fatalf("recovery interruption fixture/cleanup differs: armed=%t node=%t error=%v fallback=%d", armed, n != nil, err, fallback)
				}
				if recognize {
					// Recognition builds no Error node and does no recovery-message copy.
					if err != nil || fallback != 1 {
						t.Fatalf("recognition unnecessarily formats message: %v fallback=%d", err, fallback)
					}
				} else if !errors.Is(err, runtime.ErrLimit) || fallback != 0 {
					t.Fatalf("message interruption entered alternative: %v fallback=%d", err, fallback)
				}
				if !recognize {
					armed, fallback = false, 0
					n, err = prog.ParseWith("main", "x", engine.ParseOptions{Expectations: true, Backend: b, Unit: u, Limits: runtime.Limits{Compute: 1 << 50}, Trace: func(e engine.TraceEvent) {
						observed = state.TraceParser(e)
						if e.Kind == engine.TraceEnter && e.Rule == "fallback" {
							fallback++
						}
						if !armed && e.Kind == engine.TraceExit && e.Rule == "bad" {
							armed = true
							// The post-callback check succeeds; the check before the next event, the
							// entry of the fallback, observes the cancellation.
							state.TraceParser(e).Control.Ctx = statetest.NewPollingContext(2)
							state.TraceParser(e).Control.SinceCheck = 0
						}
					}})
					if !armed || n != nil || !errors.Is(err, context.Canceled) || fallback != 0 || observed.Scratch != nil || observed.Control != nil {
						t.Fatalf("recovery message cancellation entered alternative or retained scratch: %v fallback=%d", err, fallback)
					}
				}
				_, retryErr := prog.ParseWith("main", "x!", engine.ParseOptions{Expectations: true, Backend: b, Unit: u, Recognize: recognize})
				var recovered value.SyntaxErrors
				if !errors.As(retryErr, &recovered) || len(recovered) != 1 || !reflect.DeepEqual(recovered[0].Messages, []string{message}) {
					t.Fatalf("recovery retry differs: %v", retryErr)
				}
				if after, _ := json.Marshal(retained); string(after) != string(beforeNode) {
					t.Fatal("retry changed retained recovery Node")
				}
				if after, _ := json.Marshal(retainedErr); string(after) != string(beforeErr) {
					t.Fatal("retry changed retained recovery errors")
				}
			}
		}
	}
}
