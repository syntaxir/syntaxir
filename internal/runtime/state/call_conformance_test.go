package state

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// The engine's traced call path under conformance: call.tracked and call.track run through their
// bindings in call_bind_exec_test.go, which the engine's printer prints from the operations, so that the
// suite runs the code that the engine's compositions run. The traced parse delivers its events to a
// trace function that lists them as the model does, and the rules are the scripted rules of
// rule_conformance_test.go.

// callMachine binds the traced call path of the engine on a parse that is traced if the configuration
// says so, and the rules, the recovered errors and the missing-token mode through ruleMachine.
type callMachine struct {
	*ruleMachine
	events []string
}

func newCallMachine(cfg ref.Config) conform.Impl {
	m := &callMachine{ruleMachine: newRuleMachine(cfg, false).(*ruleMachine)}
	if cfg.Traced {
		m.p.SetTrace(m.trace, nil)
	}
	return m
}

// trace lists a trace event as the model's events list it.
func (m *callMachine) trace(e TraceEvent) {
	switch e.Kind {
	case TraceEnter:
		m.events = append(m.events, fmt.Sprintf("enter %s level=%d pos=%d depth=%d lookahead=%v", e.Rule, e.Level, e.Pos, e.Depth, e.Lookahead))
	case TraceExit:
		m.events = append(m.events, fmt.Sprintf("exit %s level=%d pos=%d depth=%d lookahead=%v matched=%v end=%d", e.Rule, e.Level, e.Pos, e.Depth, e.Lookahead, e.Matched, e.End))
	}
}

func (m *callMachine) Observe(component string) []string {
	if component == exec.Call {
		return slices.Clone(m.events)
	}
	return m.ruleMachine.Observe(component)
}

func (m *callMachine) Call(o *code.Op, args []any) []any {
	defer interrupt()
	p := m.p
	switch o.Name {
	case "call.tracked":
		return []any{p.confCallTracked()}
	case "call.track":
		p.Pos = int(m.regs[exec.Pos].(int64))
		p.Silent = int(m.regs[exec.Silent].(int64))
		defer func() { m.regs[exec.Pos] = int64(p.Pos) }()
		v, ok := p.confCallTrack(args[0].(*Rule), int(args[1].(int64)))
		return []any{v, ok}
	}
	return m.ruleMachine.Call(o, args)
}

// callEngineOps are the operations of the traced call path that the engine provides, with those that its
// cases call besides.
var callEngineOps = conform.CallOps

func callSuite() conform.Suite {
	return conform.Suite{Name: "engine traced calls", Ops: callEngineOps, New: newCallMachine}
}

// callConformanceFailures runs the handwritten cases of the traced call path and seeds randomized cases
// of n calls on s, and returns the failures and the number of cases that ran.
func callConformanceFailures(s conform.Suite, seeds, n int) (fails []error, ran int) {
	run := func(c conform.Case) {
		skipped, err := conform.Run(s, c)
		if !skipped {
			ran++
		}
		if err != nil {
			fails = append(fails, err)
		}
	}
	for _, c := range conform.CallCases() {
		run(c)
	}
	for seed := range seeds {
		run(conform.RandomCall(fmt.Sprint("random_", seed), rand.New(rand.NewPCG(uint64(seed), 71)), s.Ops, n))
	}
	return fails, ran
}

// TestEngineCallCasesRun checks that every handwritten case of the traced call path runs on the engine.
func TestEngineCallCasesRun(t *testing.T) {
	for _, c := range conform.CallCases() {
		if skipped, err := conform.Run(callSuite(), c); skipped || err != nil {
			t.Errorf("%s: skipped %v, %v", c.Func.Name, skipped, err)
		}
	}
}

func TestEngineCallConformance(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	fails, ran := callConformanceFailures(callSuite(), seeds, 60)
	if ran < seeds {
		t.Fatalf("only %d cases ran", ran)
	}
	for _, err := range fails {
		t.Error(err)
	}
}

// brokenCall is a machine of the traced call path with one operation replaced.
type brokenCall struct {
	*callMachine
	op string
	f  func(m *callMachine, args []any) []any
}

func (b brokenCall) Call(o *code.Op, args []any) []any {
	if o.Name == b.op {
		return b.f(b.callMachine, args)
	}
	return b.callMachine.Call(o, args)
}

// TestEngineCallConformanceDetectsBrokenEngine is the negative control: the engine with one operation of
// the traced call path broken, as a wrong line of its printed code would break it, fails the suite.
func TestEngineCallConformanceDetectsBrokenEngine(t *testing.T) {
	track := func(m *callMachine, args []any, f func(p *Parser, r *Rule, min int) (any, bool)) []any {
		p := m.p
		p.Pos = int(m.regs[exec.Pos].(int64))
		p.Silent = int(m.regs[exec.Silent].(int64))
		defer func() { m.regs[exec.Pos] = int64(p.Pos) }()
		v, ok := f(p, args[0].(*Rule), int(args[1].(int64)))
		return []any{v, ok}
	}
	variants := map[string]func(m *callMachine, args []any) []any{
		"call.tracked is always false":    func(m *callMachine, args []any) []any { return []any{false} },
		"call.tracked ignores the tracer": func(m *callMachine, args []any) []any { return []any{true} },
		"call.track leaves the mark that makes the next call direct": func(m *callMachine, args []any) []any {
			res := track(m, args, func(p *Parser, r *Rule, min int) (any, bool) { return p.tracedCall(r, min) })
			if m.p.Tr != nil {
				m.p.Tr.direct = true
			}
			return res
		},
		"call.track reports no event": func(m *callMachine, args []any) []any {
			return track(m, args, func(p *Parser, r *Rule, min int) (any, bool) {
				if p.Tr != nil {
					p.Tr.direct = true
				}
				return p.Call(r, min)
			})
		},
		"call.track reports the end of a call that failed at its start": func(m *callMachine, args []any) []any {
			return track(m, args, func(p *Parser, r *Rule, min int) (any, bool) {
				p.TraceEnter(r, min)
				p.Tr.direct = true
				start := p.Pos
				v, ok := p.Call(r, min)
				if !ok {
					p.Pos = start + 1
				}
				p.TraceExit(v, ok)
				return v, ok
			})
		},
		"call.track reports every call at level 0": func(m *callMachine, args []any) []any {
			return track(m, args, func(p *Parser, r *Rule, min int) (any, bool) {
				p.TraceEnter(r, 0)
				p.Tr.direct = true
				v, ok := p.Call(r, min)
				p.TraceExit(v, ok)
				return v, ok
			})
		},
	}
	for name, v := range variants {
		t.Run(name, func(t *testing.T) {
			op := strings.Fields(name)[0]
			s := callSuite()
			s.Name = name
			s.New = func(c ref.Config) conform.Impl { return brokenCall{newCallMachine(c).(*callMachine), op, v} }
			if fails, _ := callConformanceFailures(s, 100, 60); len(fails) == 0 {
				t.Fatal("no case detected the broken engine")
			}
		})
	}
}
