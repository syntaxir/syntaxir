//go:build !sxir_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

const budgetClassRangeCount = 2048

func budgetClassRanges(n int) []ast.CharRange {
	ranges := make([]ast.CharRange, n)
	for i := range ranges {
		// Keep a one-code-point gap between ranges so normalization cannot merge them.
		r := rune(0x10000 + 2*i)
		ranges[i] = ast.CharRange{Lo: r, Hi: r}
	}
	return ranges
}

func budgetClassNode(t *testing.T, p *Program, input string, opts ParseOptions) *value.Node {
	t.Helper()
	n, err := p.ParseWith("main", input, opts)
	if err != nil || n == nil {
		t.Fatalf("parse %q: node=%v err=%v", input, n, err)
	}
	return n
}

func budgetClassBudgetOptions(backend Backend, unit sxirinput.PositionUnit, allowance uint64, observed **state.Parser) ParseOptions {
	return ParseOptions{
		Backend: backend,
		Unit:    unit,
		Limits:  runtime.Limits{Compute: 1 << 30},
		Trace: func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				if observed != nil {
					*observed = state.TraceParser(e)
				}
				// The call of main counts its unit after the event of its entry.
				state.TraceParser(e).Control.Limits.Compute = state.TraceParser(e).Control.Used + 1 + allowance
			}
		},
	}
}

// requireClassBudgetLimit leaves no room after the entry of main: the membership of a class is no
// event, so the parse stops at the next counted one (the input that a hit advances over, or the syntax
// error or recovered entry of a miss), within the overshoot of a coarse check.
func requireClassBudgetLimit(t *testing.T, p *Program, input string, backend Backend, unit sxirinput.PositionUnit) *state.Parser {
	t.Helper()
	var observed *state.Parser
	n, err := p.ParseWith("main", input, budgetClassBudgetOptions(backend, unit, 0, &observed))
	if n != nil || !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("%v/%v input %q: node=%v err type=%T, want fatal compute limit error", backend, unit, input, n, err)
	}
	var limit *runtime.LimitError
	if !errors.As(err, &limit) || limit.Kind != runtime.LimitCompute {
		t.Fatalf("%v/%v input %q: error=%T %v, want spend LimitError", backend, unit, input, err, err)
	}
	if limit.Requested != 0 || limit.Used <= limit.Limit || limit.Used > limit.Limit+2*4096 {
		t.Fatalf("limit=%+v, want used in (limit, limit+K+B] and nothing requested", limit)
	}
	return observed
}

func TestBudgetClassLargeNonASCIIRangesStopAtTheNextUnit(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	late := string(ranges[len(ranges)-1].Lo)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	positive := classMembershipProgram(t, &ast.CharClass{Ranges: ranges})
	negative := classMembershipProgram(t, &ast.CharClass{Ranges: ranges, Negated: true})

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				if n := budgetClassNode(t, positive, late, ParseOptions{Backend: backend, Unit: unit}); nodes.D(n).Text != late {
					t.Fatalf("late class hit text=%q, want %q", nodes.D(n).Text, late)
				}
				if n := budgetClassNode(t, negative, gap, ParseOptions{Backend: backend, Unit: unit}); nodes.D(n).Text != gap {
					t.Fatalf("negated class text=%q, want %q", nodes.D(n).Text, gap)
				}
				if n, err := positive.ParseWith("main", gap, ParseOptions{Backend: backend, Unit: unit}); n != nil || err == nil || errors.Is(err, runtime.ErrLimit) {
					t.Fatalf("unlimited positive miss: node=%v err=%v", n, err)
				}

				// The membership of a class with many ranges counts no unit; the limit stops the parse
				// at the event that follows it.
				requireClassBudgetLimit(t, positive, late, backend, unit)
				requireClassBudgetLimit(t, positive, gap, backend, unit)
				requireClassBudgetLimit(t, negative, gap, backend, unit)
			})
		}
	}
}

func TestBudgetClassRangeCancellationAndPoolRetry(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	// Membership counts no unit, so the parse scans enough characters to count past a check interval.
	const count = int(state.BudgetCheckInterval) + 64
	input := strings.Repeat(string(ranges[len(ranges)-1].Lo), count)
	p := classMembershipProgram(t, &ast.Repeat{Expr: &ast.CharClass{Ranges: ranges}, Min: 1, Max: -1})

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				var observed *state.Parser
				var control *state.BudgetControl
				var startSteps uint64
				exits := 0
				opts := ParseOptions{
					Backend: backend,
					Unit:    unit,
					Limits:  runtime.Limits{Compute: 1 << 30},
					Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							observed, control = state.TraceParser(e), state.TraceParser(e).Control
							startSteps = control.Used
							// Trace emit polls once after this callback; cancel at the next
							// check, which the input that the scan advances over reaches.
							control.Ctx = statetest.NewPollingContext(2)
						} else if e.Kind == TraceExit {
							exits++
						}
					},
				}
				n, err := p.ParseWith("main", input, opts)
				if n != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("node=%v err=%v, want cancellation during the class scan", n, err)
				}
				if observed == nil || control == nil || control.Used-startSteps < 128 {
					t.Fatalf("cancellation occurred before a substantial scan: parser observed=%v steps=%v", observed != nil, control)
				}
				if exits != 0 {
					t.Fatalf("trace exits after fatal interruption=%d, want 0", exits)
				}
				if observed.Scratch != nil || observed.Control != nil {
					t.Fatalf("parser retained spend state after interruption: scratch=%v control=%v", observed.Scratch, observed.Control)
				}
				if n := budgetClassNode(t, p, input, ParseOptions{Backend: backend, Unit: unit}); len(nodes.D(n).Children) != count {
					t.Fatalf("retry elements=%d, want %d", len(nodes.D(n).Children), count)
				}
			})
		}
	}
}

// TestBudgetClassInterruptionBypassesChoiceAndRecovery pins "Nothing absorbs an interruption" of
// "Checks and Interruptions" in spec/runtime/parsing.md.
func TestBudgetClassInterruptionBypassesChoiceAndRecovery(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	// The class with #recover(skip=.*).
	recoverClass := &ast.Recovery{
		Expr: &ast.CharClass{Ranges: ranges},
		Skip: &ast.Repeat{Expr: &ast.Any{}, Min: 0, Max: -1},
	}
	p := classMembershipProgram(t, &ast.Choice{Alts: []ast.Expr{
		recoverClass,
		&ast.Literal{Value: gap},
	}})

	// Without an active control, the class miss is recovered and the grammar
	// completes. The controlled attempt must terminate before recovery/choice.
	n, err := p.ParseWith("main", gap, ParseOptions{})
	if n == nil || errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("uncontrolled recovery baseline: node=%v err=%v", n, err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if observed := requireClassBudgetLimit(t, p, gap, backend, sxirinput.CodePoints); observed == nil {
			t.Fatalf("%v: trace did not observe start rule", backend)
		}
	}
}

func TestBudgetClassFastPathParity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		class *ast.CharClass
		input string
	}{
		{"single-range", &ast.CharClass{Ranges: []ast.CharRange{{Lo: 'a', Hi: 'z'}}}, "m"},
		{"two-ranges", &ast.CharClass{Ranges: []ast.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10000}}}, string(rune(0x10000))},
		{"ascii-bitmap-and-slow-unicode", &ast.CharClass{Ranges: []ast.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10000}, {Lo: 0x10002, Hi: 0x10002}}}, "m"},
		{"negated-ascii-bitmap", &ast.CharClass{Negated: true, Ranges: []ast.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10000}, {Lo: 0x10002, Hi: 0x10002}}}, "!"},
	}
	for _, tc := range cases {
		p := classMembershipProgram(t, tc.class)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				t.Run(tc.name+"/"+backend.String()+"/"+unit.String(), func(t *testing.T) {
					base, baseErr := p.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
					controlled, controlledErr := p.ParseWith("main", tc.input, ParseOptions{
						Backend: backend, Unit: unit,
						Limits: runtime.Limits{Compute: 10000},
					})
					if !reflect.DeepEqual(controlled, base) || !reflect.DeepEqual(controlledErr, baseErr) {
						t.Fatalf("controlled result differs: got %#v/%#v, want %#v/%#v", controlled, controlledErr, base, baseErr)
					}
				})
			}
		}
	}
}

// Optimized guards can skip the expression matcher; value-free scans bypass
// per-element expression dispatch. Their membership counts no unit, so a limit stops them at the next
// event, and a scan is interrupted by the checks that the input it advances over reaches, while a guard
// over one character reaches none.
func TestBudgetClassOptimizedRoutes(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	late := string(ranges[len(ranges)-1].Lo)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	class := &ast.CharClass{Ranges: ranges}
	calling := func(e ast.Expr) ast.Expr {
		return &ast.Seq{Items: []ast.Expr{e, &ast.Ref{Name: "e"}}}
	}
	cases := []struct {
		name  string
		expr  ast.Expr
		input string
		op    bytecode.FlowOp
		scans bool
	}{
		{"guard-hit", &ast.Choice{Alts: []ast.Expr{class, &ast.Literal{Value: gap}}}, late, bytecode.FlowGuardJump, false},
		{"guard-miss-fallback", &ast.Choice{Alts: []ast.Expr{class, &ast.Literal{Value: gap}}}, gap, bytecode.FlowGuardJump, false},
		// A repetition in a rule that calls no rule is a scan site; with the call of e it is a plain
		// repetition of the class.
		{"scan", calling(&ast.Atomic{Expr: &ast.Repeat{Expr: class, Min: 1, Max: -1}}), late, bytecode.FlowScanClassCount, true},
		{"negated-scan", calling(&ast.Atomic{Expr: &ast.Repeat{Expr: &ast.CharClass{Ranges: ranges, Negated: true}, Min: 1, Max: -1}}), gap, bytecode.FlowScanClassCount, true},
		{"scan-site", &ast.Atomic{Expr: &ast.Repeat{Expr: class, Min: 1, Max: -1}}, late, bytecode.FlowScanClassAt, true},
		{"negated-scan-site", &ast.Atomic{Expr: &ast.Repeat{Expr: &ast.CharClass{Ranges: ranges, Negated: true}, Min: 1, Max: -1}}, gap, bytecode.FlowScanClassAt, true},
	}
	if strconv.IntSize == 64 {
		// Construct a source-sized bound without a constant-to-int overflow on 32bit.
		wide := uint64(1) << 32
		cases = append(cases, struct {
			name  string
			expr  ast.Expr
			input string
			op    bytecode.FlowOp
			scans bool
		}{
			"wide-scan", &ast.Atomic{Expr: &ast.Repeat{Expr: class, Min: 1, Max: int(wide)}}, late, bytecode.FlowMatchClass, true,
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := classMembershipProgram(t, tc.expr)
			found := false
			for _, f := range p.Module().Flows {
				for _, in := range f.Code {
					found = found || in.Op == tc.op
				}
			}
			if !found {
				t.Fatalf("instruction %v was not selected:\n%s", tc.op, p.Module().Disassemble())
			}
			// Exercise the same VM paths after loading the module; the checks below trace
			// the parse, which a compiled grammar serves in document mode.
			loaded := saveLoad(t, p, "main", CompiledDocument)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					for _, program := range []*Program{p, loaded} {
						if program == loaded && backend == Closure {
							continue
						}
						budgetClassNode(t, program, tc.input, servedBy(program.mode, ParseOptions{Backend: backend, Unit: unit}))
						requireClassBudgetLimit(t, program, tc.input, backend, unit)
						polled := tc.input
						if tc.scans {
							polled = strings.Repeat(tc.input, 2*int(state.BudgetCheckInterval))
						}
						var c *state.BudgetControl
						var start uint64
						n, err := program.ParseWith("main", polled, ParseOptions{
							Backend: backend, Unit: unit, Limits: runtime.Limits{Compute: 1 << 30},
							Trace: func(e TraceEvent) {
								if e.Kind == TraceEnter && e.Rule == "main" {
									c = state.TraceParser(e).Control
									start = c.Used
									c.Ctx = statetest.NewPollingContext(2)
								}
							},
						})
						// The event of the exit of main observes the context too, so a guard over one
						// character is cancelled there, before it has counted a check interval; a scan is
						// cancelled by the check of the counts of the input it advances over, before its end.
						if n != nil || !errors.Is(err, context.Canceled) || c == nil {
							t.Fatalf("%v/%v: cancellation did not interrupt the parse: node=%v error type=%T", backend, unit, n, err)
						}
						if counted := c.Used - start; !tc.scans && counted >= state.BudgetCheckInterval || tc.scans && (counted < 128 || counted >= 2*state.BudgetCheckInterval) {
							t.Fatalf("%v/%v: cancelled after %d units, want a guard before a check interval and a scan within its input", backend, unit, counted)
						}
						budgetClassNode(t, program, tc.input, servedBy(program.mode, ParseOptions{Backend: backend, Unit: unit}))
					}
				}
			}
		})
	}
}
