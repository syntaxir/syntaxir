//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

const budgetGuardedChoiceAlternatives = 512

func budgetGuardedChoiceExpr() *ast.Choice {
	alts := make([]ast.Expr, budgetGuardedChoiceAlternatives)
	for i := range alts {
		alts[i] = &ast.Literal{Value: string(rune(0x1000 + i))}
	}
	return &ast.Choice{Alts: alts}
}

func budgetGuardedChoiceProgram(t *testing.T) (*engine.Program, *ast.Choice) {
	t.Helper()
	choice := budgetGuardedChoiceExpr()
	return budgetGuardedChoiceProgramOf(t, choice, &ast.Seq{Items: []ast.Expr{
		&ast.Not{Expr: choice},
		&ast.Literal{Value: "z"},
	}}), choice
}

// budgetGuardedChoiceValueProgram places the choice where it builds the value of main.
func budgetGuardedChoiceValueProgram(t *testing.T) (*engine.Program, *ast.Choice) {
	t.Helper()
	choice := budgetGuardedChoiceExpr()
	return budgetGuardedChoiceProgramOf(t, choice, choice), choice
}

// budgetGuardedChoiceProgramOf compiles main = first / fallback, where first holds choice.
func budgetGuardedChoiceProgramOf(t *testing.T, choice *ast.Choice, first ast.Expr) *engine.Program {
	t.Helper()
	main := &ast.Choice{Alts: []ast.Expr{first, &ast.Ref{Name: "fallback"}}}
	g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{
		&ast.RuleDef{Name: "main", Expr: main},
		&ast.RuleDef{Name: "fallback", Expr: &ast.Literal{Value: "z"}},
	}}}
	starttest.MarkAll(g)
	prog, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatalf("compile guarded-choice grammar: %v", err)
	}
	return prog
}

func budgetGuardedChoiceSnapshot(n *value.Node) *value.Node {
	if n == nil {
		return nil
	}
	copy := *n
	if nodes.D(n).Children != nil {
		nodes.D(&copy).Children = make([]*value.Node, len(nodes.D(n).Children))
		for i, child := range nodes.D(n).Children {
			nodes.D(&copy).Children[i] = budgetGuardedChoiceSnapshot(child)
		}
	}
	nodes.D(&copy).Fields = append(valueimpl.Fields(nil), nodes.D(n).Fields...)
	for i := range nodes.D(&copy).Fields {
		if child, ok := nodes.D(&copy).Fields[i].Value.(*value.Node); ok {
			nodes.D(&copy).Fields[i].Value = budgetGuardedChoiceSnapshot(child)
		}
	}
	return &copy
}

// A guarded choice is straight-line code: skipping its alternatives counts nothing, and so does the
// peek that reads the input, since reading input is no event; only the input a match advances over
// counts. The size of the grammar bounds the skips between two counts, as it bounds the
// straight-line code of a generated rule.
// The choice is checked where it builds no value (under a lookahead) and where it builds the value
// of its rule, whose alternatives are wrapped to give single values.
func TestBudgetGuardedChoiceSilentSkipLoopBudgetAndCancellation(t *testing.T) {
	t.Parallel()
	for _, build := range []bool{false, true} {
		program := budgetGuardedChoiceProgram
		if build {
			program = budgetGuardedChoiceValueProgram
		}
		t.Run("build="+strconv.FormatBool(build), func(t *testing.T) {
			t.Parallel()
			prog, choice := program(t)
			n := closureExprNode(t, entry.Checked(prog), choice)
			if n.Attrs().Want != build {
				t.Fatalf("the choice builds a value: %v, want %v", n.Attrs().Want, build)
			}
			budgetGuardedChoiceSilentSkip(t, prog, closureExprMatcher(t, entry.Checked(prog), choice))
		})
	}
}

func budgetGuardedChoiceSilentSkip(t *testing.T, prog *engine.Program, m state.Matcher) {
	t.Helper()
	for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
		t.Run(unit.String()+"/budget", func(t *testing.T) {
			p := state.NewParser(prog.Shared(), "z", unit, false)
			p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
			p.Silent = 1
			p.MaxDepth = budgetGuardedChoiceAlternatives + 1
			if n, ok := m(p); n != nil || ok || p.Control.Used != 0 {
				t.Fatalf("direct silent choice node=%v ok=%v spent=%d, want no unit for the peek and the %d skipped alternatives", n, ok, p.Control.Used, budgetGuardedChoiceAlternatives)
			}
		})

		t.Run(unit.String()+"/cancel", func(t *testing.T) {
			p := state.NewParser(prog.Shared(), "z", unit, false)
			ctx := statetest.NewPollingContext(1)
			p.Control = state.NewBudgetControl(ctx, runtime.Limits{})
			p.Silent = 1
			p.MaxDepth = budgetGuardedChoiceAlternatives + 1
			if err := statetest.CatchFatal(t, func() { _, _ = m(p) }); err != nil || p.Control.Used != 0 {
				t.Fatalf("direct silent choice err=%v steps=%d, want no poll between the charges of straight-line code", err, p.Control.Used)
			}
		})
	}
}

func TestBudgetGuardedChoiceExactSkipBudget(t *testing.T) {
	t.Parallel()
	prog, choice := budgetGuardedChoiceProgram(t)
	m := closureExprMatcher(t, entry.Checked(prog), choice)
	for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
		for _, text := range []string{"z", ""} {
			for _, silent := range []bool{true, false} {
				t.Run(unit.String()+"/input="+strconv.Quote(text)+"/silent="+strconv.FormatBool(silent), func(t *testing.T) {
					p := state.NewParser(prog.Shared(), text, unit, false)
					p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
					p.MaxDepth = budgetGuardedChoiceAlternatives + 1
					if silent {
						p.Silent = 1
					} else {
						// An older failure position also suppresses expectations.
						p.Farthest = 1
					}
					if n, ok := m(p); n != nil || ok {
						t.Fatalf("rejected choice returned %v, %v", n, ok)
					}
					// A rejected choice counts nothing, whatever the number of alternatives skipped.
					if p.Control.Used != 0 || len(p.Exp) != 0 || p.Pos != 0 {
						t.Fatalf("choice state spent=%d expectations=%v pos=%d", p.Control.Used, p.Exp, p.Pos)
					}
				})
			}
		}
	}
}

func TestBudgetGuardedChoicePassingMatcherCharge(t *testing.T) {
	t.Parallel()
	for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
		for _, skip := range []bool{false, true} {
			t.Run(unit.String()+"/skip="+strconv.FormatBool(skip), func(t *testing.T) {
				alts := []ast.Expr{&ast.Literal{Value: "z"}}
				steps := uint64(1) // the one input unit the literal advances over; the peek and a skipped alternative count nothing
				if skip {
					alts = append([]ast.Expr{&ast.Literal{Value: "x"}}, alts...)
				}
				choice := &ast.Choice{Alts: alts}
				prog := compileExprProgram(t, &ast.Not{Expr: choice})
				m := closureExprMatcher(t, entry.Checked(prog), choice)
				p := state.NewParser(prog.Shared(), "z", unit, false)
				p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: steps})
				p.Silent = 1
				if n, ok := m(p); n != nil || !ok || p.Pos != 1 || p.Control.Used != steps {
					t.Fatalf("matching choice node=%v ok=%v pos=%d spent=%d", n, ok, p.Pos, p.Control.Used)
				}
			})
		}
	}
}

func TestBudgetGuardedChoicePublicParityAndFatalRetry(t *testing.T) {
	t.Parallel()
	prog, _ := budgetGuardedChoiceProgram(t)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					base := engine.ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
					for _, text := range []string{"z", "q", ""} {
						want, wantErr := prog.ParseWith("main", text, base)
						controlled := base
						controlled.Limits = runtime.Limits{Compute: 1 << 20}
						got, gotErr := prog.ParseWith("main", text, controlled)
						if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
							t.Fatalf("generous controls changed result: got=%#v/%#v want=%#v/%#v", got, gotErr, want, wantErr)
						}
						if text == "z" && (wantErr != nil || !recognize && want == nil || recognize && want != nil) {
							t.Fatalf("fixture result node=%v err=%v recognize=%v", want, wantErr, recognize)
						}
						if text != "z" && (wantErr == nil || want != nil) {
							t.Fatalf("fixture accepted %q: node=%v err=%v", text, want, wantErr)
						}
					}
				})
			}
			for _, recognize := range []bool{false, true} {
				budgetGuardedChoiceFatalAndRetry(t, prog, backend, unit, recognize, false)
				budgetGuardedChoiceFatalAndRetry(t, prog, backend, unit, recognize, true)
			}
		}
	}
}

func budgetGuardedChoiceFatalAndRetry(t *testing.T, prog *engine.Program, backend engine.Backend, unit input.PositionUnit, recognize, cancel bool) {
	t.Helper()
	name := "budget"
	if cancel {
		name = "cancel"
	}
	t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize)+"/"+name, func(t *testing.T) {
		retained, err := prog.ParseWith("main", "z", engine.ParseOptions{Backend: backend, Unit: unit})
		if err != nil || retained == nil {
			t.Fatalf("baseline parse: node=%v err=%v", retained, err)
		}
		snapshot := budgetGuardedChoiceSnapshot(retained)

		var observed *state.Parser
		fallbackEntries, exits := 0, 0
		opts := engine.ParseOptions{
			Backend:   backend,
			Unit:      unit,
			Recognize: recognize,
			Limits:    runtime.Limits{Compute: 1 << 30},
			Trace: func(e engine.TraceEvent) {
				if e.Kind == engine.TraceEnter && e.Rule == "main" {
					observed = state.TraceParser(e)
					if cancel {
						state.TraceParser(e).Control.Ctx = statetest.NewPollingContext(2)
					} else {
						state.TraceParser(e).Control.Limits.Compute = state.TraceParser(e).Control.Used + 1
					}
				}
				if e.Kind == engine.TraceEnter && e.Rule == "fallback" {
					fallbackEntries++
				}
				if e.Kind == engine.TraceExit {
					exits++
				}
			},
		}
		n, gotErr := prog.ParseWith("main", "z", opts)
		if n != nil || observed == nil || observed.Scratch != nil || observed.Control != nil {
			t.Fatalf("fatal parse node=%v err=%v parser=%v", n, gotErr, observed)
		}
		if cancel {
			if !errors.Is(gotErr, context.Canceled) {
				t.Fatalf("cancelled parse error=%v, want context cancellation", gotErr)
			}
		} else {
			var limit *runtime.LimitError
			if !errors.As(gotErr, &limit) || limit.Kind != runtime.LimitCompute {
				t.Fatalf("budgeted parse error=%v, want spend LimitError", gotErr)
			}
		}
		if fallbackEntries != 0 || exits != 0 {
			t.Fatalf("fatal spend entered fallback %d times or emitted %d exits", fallbackEntries, exits)
		}
		if !reflect.DeepEqual(retained, snapshot) {
			t.Fatal("fatal parse mutated a node retained from an earlier successful parse")
		}

		retry, retryErr := prog.ParseWith("main", "z", engine.ParseOptions{Backend: backend, Unit: unit})
		if retryErr != nil || !reflect.DeepEqual(retry, snapshot) || !reflect.DeepEqual(retained, snapshot) {
			t.Fatalf("pool retry node=%v err=%v; retained=%v", retry, retryErr, retained)
		}
	})
}

// compileExprProgram compiles the grammar whose start rule main is e.
func compileExprProgram(t *testing.T, e ast.Expr) *engine.Program {
	t.Helper()
	g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "main", Expr: e}}}}
	starttest.MarkAll(g)
	prog, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return prog
}
