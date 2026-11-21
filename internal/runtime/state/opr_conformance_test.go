package state

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// The engine's Operators Rule descriptors, matches of operators and records of the split forms of the
// loop under conformance: every operation runs through its binding in opr_bind_exec_test.go, which the
// engine's printer prints from the operation, so that the suite runs the code that the engine's
// compositions run.

// oprMachine binds the Operators Rule operations of the engine, and the rest through engineMachine.
// The trivia of a rule and the lines of its operators run their scripts (ref.OprExpr) with the
// engine's own operations.
type oprMachine struct {
	*engineMachine
	oprRules []*Rule
	lists    map[listKey]*listBox
	regs     map[string]any
	zeroMark *markBox
	binder   conform.Binder
}

// listBox is the handle of a list of operators; every empty list is one, as the model has it.
type listBox struct{ s []*OperatorsRuleOp }

type listKey struct {
	first *OperatorsRuleOp
	n     int
}

type attemptBox struct{ a OperatorsRuleAttempt }

type markBox struct{ m Mark }

func newOprMachine(cfg ref.Config) conform.Impl {
	m := &oprMachine{engineMachine: newEngineMachine(cfg, false, false).(*engineMachine), lists: map[listKey]*listBox{}}
	id := 0
	line := func(s ref.OprLineSpec) *OperatorsRuleLine {
		return &OperatorsRuleLine{Scope: &program.Scope{Names: make([]string, s.Scope)}, M: m.scripted(s.Expr)}
	}
	ops := func(os []ref.OprOp) []*OperatorsRuleOp {
		var l []*OperatorsRuleOp
		for _, o := range os {
			id++
			l = append(l, &OperatorsRuleOp{Id: id, Kind: o.Kind, Assoc: o.Assoc, Level: int(o.Level), Line: line(o.Line)})
		}
		return l
	}
	for _, r := range cfg.Oprs {
		or := &OperatorsRule{Prefix: ops(r.Prefix), Led: ops(r.Led)}
		for _, s := range r.Operands {
			or.Operands = append(or.Operands, line(s))
		}
		if r.Skip {
			trivia := make([]ref.OprExpr, len(r.Trivia))
			for k, e := range r.Trivia {
				trivia[k].Eval = e
			}
			or.Skip = m.scripted(trivia)
		}
		m.oprRules = append(m.oprRules, &Rule{OperatorsRule: or})
	}
	m.zeroMark = &markBox{}
	m.binder = conform.Binder{Methods: confOprBindings(m.p), Conv: oprConv{m}}
	return m
}

// scripted returns the grammar code of a line or of the trivia that runs script: its k-th evaluation
// does the k-th entry, or the last once k passes it, and an empty script fails at once.
func (m *oprMachine) scripted(script []ref.OprExpr) Matcher {
	evals := 0
	return func(p *Parser) (*value.Node, bool) {
		var e ref.OprExpr
		if len(script) > 0 {
			e = script[min(evals, len(script)-1)]
		}
		evals++
		if len(e.Captures) > 0 || len(e.Missing) > 0 || e.Touch > 0 {
			panic("engine binding: the scripts of the Operators Rules set no captures, insert no tokens and examine no input")
		}
		start := p.Pos
		if e.Charge > 0 {
			m.engineMachine.Call(oprOps.Op("budget.charge"), []any{e.Charge})
		}
		for _, id := range e.Errs {
			m.engineMachine.Call(oprOps.Op("rec.push"), []any{m.Err(id)})
		}
		if e.Cut {
			p.Cut = true
		}
		p.Pos = start + int(e.Len)
		if !e.OK {
			return nil, false
		}
		return nodes.Ptr(nodes.Data{Fresh: true}), true
	}
}

var oprOps = conform.OprModule()

func (m *oprMachine) Rule(index int64) any { return m.oprRules[index] }

// UseRegisters hands the machine the registers of the run: opr.attempt reads the position.
func (m *oprMachine) UseRegisters(regs map[string]any) { m.regs = regs }

func (m *oprMachine) Handle(name string) any {
	switch name {
	case conform.OprFrameOp:
		return &Frame{}
	case conform.OprMarkOp:
		return &markBox{m.p.Mark()}
	case conform.OprNodeOp:
		return new(value.Node)
	case conform.OprRecordOp("oprLongest"):
		return &LongestState{}
	case conform.OprRecordOp("oprNud"):
		return &NudState{}
	case conform.OprRecordOp("oprExpr"):
		return &ExprState{}
	}
	panic("engine binding: no handle " + name)
}

func (m *oprMachine) Call(o *code.Op, args []any) []any {
	if strings.HasPrefix(o.Name, "opr") {
		if pos, ok := m.regs[exec.Pos].(int64); ok {
			m.p.Pos = int(pos)
		}
		if cut, ok := m.regs[exec.Cut].(bool); ok {
			m.p.Cut = cut
		}
		if silent, ok := m.regs[exec.Silent].(int64); ok {
			m.p.Silent = int(silent)
		}
		res := m.binder.Call(o, args)
		// The operations that run grammar code write the registers.
		if slices.Contains(conform.OprMatchOps, o.Name) {
			m.regs[exec.Pos], m.regs[exec.Cut], m.regs[exec.Silent] = int64(m.p.Pos), m.p.Cut, int64(m.p.Silent)
		}
		return res
	}
	return m.engineMachine.Call(o, args)
}

// oprConv converts the handles of the Operators Rule operations of the engine.
type oprConv struct{ m *oprMachine }

func (c oprConv) ToImpl(arg any, t reflect.Type) (reflect.Value, bool) {
	switch a := arg.(type) {
	case *listBox:
		return reflect.ValueOf(a.s), true
	case *attemptBox:
		return reflect.ValueOf(a.a), true
	case *markBox:
		return reflect.ValueOf(a.m), true
	case *errorList:
		return reflect.ValueOf(a.e), true
	}
	return reflect.Value{}, false
}

func (c oprConv) FromImpl(v reflect.Value) (any, bool) {
	switch x := v.Interface().(type) {
	case []*OperatorsRuleOp:
		k := listKey{n: len(x)}
		if len(x) > 0 {
			k.first = x[0]
		}
		b := c.m.lists[k]
		if b == nil {
			b = &listBox{x}
			c.m.lists[k] = b
		}
		return b, true
	case OperatorsRuleAttempt:
		if x.Op == nil {
			// The null attempt is no handle.
			return nil, true
		}
		return &attemptBox{x}, true
	case Mark:
		if x == (Mark{}) {
			return c.m.zeroMark, true
		}
		return &markBox{x}, true
	case RecEntry:
		return c.m.handle(x), true
	}
	return nil, false
}

// oprSuite is the suite of the engine's Operators Rule operations.
func oprSuite() conform.Suite {
	return conform.Suite{Name: "engine opr", Ops: conform.OprSuiteOps(), New: newOprMachine}
}

// oprConformanceFailures runs the handwritten cases of the Operators Rule family and seeds randomized
// cases of n calls on s, and returns the failures and the number of cases that ran.
func oprConformanceFailures(s conform.Suite, seeds, n int) (fails []error, ran int) {
	run := func(c conform.Case) {
		skipped, err := conform.Run(s, c)
		if !skipped {
			ran++
		}
		if err != nil {
			fails = append(fails, err)
		}
	}
	for _, c := range conform.OprCases() {
		run(c)
	}
	for seed := range seeds {
		rng := rand.New(rand.NewPCG(uint64(seed), 66))
		run(conform.RandomOpr(fmt.Sprint("random_", seed), rng, conform.OprOps, n, conform.OprConfig()))
	}
	return fails, ran
}

func TestEngineOprConformance(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	fails, ran := oprConformanceFailures(oprSuite(), seeds, 150)
	if ran < seeds {
		t.Fatalf("only %d cases ran", ran)
	}
	for _, err := range fails {
		t.Error(err)
	}
}

// brokenOpr is the engine with one operation of the Operators Rule family replaced.
type brokenOpr struct {
	*oprMachine
	op   string
	edit func(call func(op string, args ...any) []any, args []any) []any
}

func (b brokenOpr) Call(o *code.Op, args []any) []any {
	if o.Name != b.op {
		return b.oprMachine.Call(o, args)
	}
	mod := conform.OprModule()
	return b.edit(func(op string, a ...any) []any { return b.oprMachine.Call(mod.Op(op), a) }, args)
}

// TestEngineOprConformanceDetectsBrokenEngine is the negative control: the engine with one operation
// of the family broken, as a wrong line of its printed code would break it, fails the suite.
func TestEngineOprConformanceDetectsBrokenEngine(t *testing.T) {
	for _, br := range append(conform.OprBrokenOps(), conform.OprMatchBrokenOps()...) {
		t.Run(br.Name, func(t *testing.T) {
			s := oprSuite()
			s.New = func(c ref.Config) conform.Impl { return brokenOpr{newOprMachine(c).(*oprMachine), br.Op, br.Edit} }
			if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
				t.Fatal("no case detected the broken engine")
			}
		})
	}
}
