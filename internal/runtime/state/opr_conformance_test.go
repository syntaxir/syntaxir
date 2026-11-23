package state

import (
	"fmt"
	"maps"
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
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
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

// attemptBox is an Attempt handle. It keeps the Errs handle that the match was made with: an entry
// of no errors has no identity of its own (engineMachine.handle), so opr.errs returns that handle for it.
type attemptBox struct {
	a    OperatorsRuleAttempt
	errs *errorList
}

type markBox struct{ m Mark }

// meteredOpr is the machine of a suite that controls charges.
type meteredOpr struct{ *oprMachine }

func (m meteredOpr) Used() uint64 { return m.p.Control.Used }

func newOprMachine(cfg ref.Config) conform.Impl { return newOprMachineControlled(cfg, false) }

// newOprMachineControlled returns the machine of cfg, which reports its charges if controlled.
func newOprMachineControlled(cfg ref.Config, controlled bool) conform.Impl {
	var em *engineMachine
	switch x := newEngineMachine(cfg, controlled, false).(type) {
	case *engineMachine:
		em = x
	case meteredEngine:
		em = x.engineMachine
	}
	m := &oprMachine{engineMachine: em, lists: map[listKey]*listBox{}}
	if m.p.Prog != nil {
		panic("engine binding: the machine has a program")
	}
	if cfg.Missing {
		m.p.Missing = &MissingTokenState{Limit: 1 << 30}
	}
	m.p.Prog = &SharedProgram{staticProgram: &program.Program{RecordsSourceInfo: cfg.SourceInfo}}
	id := 0
	line := func(s ref.OprLineSpec) *OperatorsRuleLine {
		l := &OperatorsRuleLine{Scope: &program.Scope{}, M: m.scripted(s.Expr)}
		for k := range s.Scope {
			l.Scope.Names = append(l.Scope.Names, fmt.Sprint("c", k))
		}
		// The shape of the value of the line, as the closure backend's builder makes it from the static
		// shape of its expression.
		switch s.Shape {
		case ref.OprPass:
			l.Single = program.SinglePass
		case ref.OprTuple:
			l.Shape, l.Single = program.Shaper{Elems: true, K: 2}, program.SingleTuple
		case ref.OprShared:
			l.Shape, l.Single = program.Shaper{Elems: true, Shared: true, K: 2}, program.SingleTuple
		case ref.OprNone:
			l.Single = program.SingleNone
		case ref.OprWrap:
			l.Shape, l.Single = program.Shaper{K: 1}, program.SinglePass
		case ref.OprUnit:
			l.Single = program.SingleNone
		}
		// The value of an operand line is the node of its tuple when the closure backend's scopeValueOf
		// makes it so (check.ScopeTuple).
		tuple := s.Shape == ref.OprTuple || s.Shape == ref.OprShared || s.Shape == ref.OprWrap || s.Shape == ref.OprUnit
		l.Value = func(p *Parser, v *value.Node, start int) *value.Node {
			if s.Shape == ref.OprNone {
				panic("engine binding: an operand line has a value")
			}
			if tuple {
				return ShapeTuple(p, l.Shape, v, start)
			}
			return l.Shape.One(v)
		}
		if s.Action {
			l.Act = oprProbe
			if s.NullAction {
				l.Act = oprNullProbe
			}
		}
		return l
	}
	ops := func(os []ref.OprOp) []*OperatorsRuleOp {
		var l []*OperatorsRuleOp
		for _, o := range os {
			id++
			l = append(l, &OperatorsRuleOp{Id: id, Kind: o.Kind, Assoc: o.Assoc, Level: int(o.Level), Line: line(o.Line)})
		}
		return l
	}
	for i, r := range cfg.Oprs {
		or := &OperatorsRule{Prefix: ops(r.Prefix), Led: ops(r.Led)}
		for _, s := range r.Operands {
			or.Operands = append(or.Operands, line(s))
		}
		if r.Skip {
			trivia := make([]ref.OprExpr, len(r.Trivia))
			for k, e := range r.Trivia {
				if len(e.Captures) > 0 {
					panic("engine binding: the trivia of an Operators Rule sets no captures")
				}
				trivia[k].Eval = e
			}
			or.Skip = m.scripted(trivia)
		}
		rule := &Rule{OperatorsRule: or, Kinds: valueimpl.NewRuleKinds(fmt.Sprint("r", i))}
		rule.Name, rule.Novalue = fmt.Sprint("r", i), r.Novalue
		if r.OpType {
			rule.OpNodeType = oprTypeName(i)
			rule.OpKinds = nodes.NewNodeTypeKinds(rule.OpNodeType)
		}
		m.oprRules = append(m.oprRules, rule)
	}
	m.zeroMark = &markBox{}
	// The frame operations that the cases call run through the engine's frame bindings.
	methods := confOprBindings(m.p)
	maps.Copy(methods, confFrameBindings(m.p))
	m.binder = conform.Binder{Methods: methods, Conv: oprConv{m}}
	if controlled {
		return meteredOpr{m}
	}
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
		if len(e.Missing) > 0 || e.Touch > 0 {
			panic("engine binding: the scripts of the Operators Rules insert no tokens and examine no input")
		}
		start := p.Pos
		if e.Charge > 0 {
			m.engineMachine.Call(oprOps.Op("budget.charge"), []any{e.Charge})
		}
		for _, id := range e.Errs {
			m.engineMachine.Call(oprOps.Op("rec.push"), []any{m.Err(id)})
		}
		// A capture of the line is a write to a slot of the current frame, as the grammar code's is.
		for _, k := range e.Captures {
			m.binder.Call(oprOps.Op("frame.set"), []any{k, p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(start), Fresh: true})})
		}
		if e.Cut {
			p.Cut = true
		}
		p.Pos = start + int(e.Len)
		if !e.OK {
			return nil, false
		}
		return p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Fresh: true}), true
	}
}

// oprProbe is the action of a line (ref.OprLineSpec): a node of the range of its input whose elements
// are the locals op, lhs and rhs and whose fields are the captures of its frame.
func oprProbe(c *EvalCtx) (any, error) {
	local := func(name string) *value.Node {
		for l := c.Locals; l != nil; l = l.Next {
			if l.Name == name {
				v, _ := l.Val.(*value.Node)
				return v
			}
		}
		return nil
	}
	d := nodes.Data{Kind: valueimpl.SeqKind, Start: int32(c.Start), End: int32(c.End),
		Children: []*value.Node{local("op"), local("lhs"), local("rhs")}}
	for k, v := range c.Frame.Vals {
		if v != nil {
			valueimpl.SetField(&d.Fields, fmt.Sprint("c", k), v)
		}
	}
	return nodes.Ptr(d), nil
}

// oprNullProbe is the action of a line with ref.OprLineSpec.NullAction: it returns the null value.
func oprNullProbe(*EvalCtx) (any, error) { return nil, nil }

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
	defer interrupt()
	if slices.Contains(ref.OprValueOps, o.Name) {
		return []any{m.valueOp(o.Name, args)}
	}
	if o.Name == ref.FrameSlotOp {
		// An empty slot is the null value, which has one identity.
		if v := args[0].(*Frame).Vals[args[1].(int64)]; v != nil {
			return []any{v}
		}
		return []any{nil}
	}
	// The tokens that a case inserts and removes go through the engine's missing-token mode.
	if o.Name == conform.MissingInsertOp || strings.HasPrefix(o.Name, "missing.") {
		return (&missingMachine{m.engineMachine}).Call(o, args)
	}
	if strings.HasPrefix(o.Name, "opr") || strings.HasPrefix(o.Name, "frame.") {
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
		switch o.Name {
		case "opr.attempt", "opr.attemptMissing":
			if b, ok := res[0].(*attemptBox); ok {
				b.errs = args[5].(*errorList)
			}
		case "opr.errs":
			if h := res[0].(*errorList); h.e.None() && args[0].(*attemptBox).errs.e.None() {
				res[0] = args[0].(*attemptBox).errs
			}
		}
		// The operations that run grammar code write the registers.
		if slices.Contains(conform.OprMatchOps, o.Name) {
			m.regs[exec.Pos], m.regs[exec.Cut], m.regs[exec.Silent] = int64(m.p.Pos), m.p.Cut, int64(m.p.Silent)
		}
		return res
	}
	return m.engineMachine.Call(o, args)
}

// valueOp takes a value apart (ref.OprValueOps).
func (m *oprMachine) valueOp(name string, args []any) any {
	d := nodes.D(args[0].(*value.Node))
	switch name {
	case ref.OprValueStartOp:
		return int64(d.Start)
	case ref.OprValueEndOp:
		return int64(d.End)
	case ref.OprValueKidsOp:
		return int64(len(d.Children))
	case ref.OprValueKidOp:
		return nodeOrNil(d.Children[args[1].(int64)])
	case ref.OprValueFieldOp:
		v, _ := d.Fields.Get(ref.OprFieldNames[args[1].(int64)])
		if n, ok := v.(*value.Node); ok {
			return nodeOrNil(n)
		}
		return v
	case ref.OprValueRuleOp:
		if d.Kind != nil {
			for i, r := range m.oprRules {
				if d.Kind.Rule == r.Name {
					return int64(i)
				}
			}
		}
		return int64(-1)
	case ref.OprValueTypeOp:
		for i := range m.oprRules {
			if d.Kind != nil && d.Kind.Reserved < 0 && d.Kind.Type == oprTypeName(i) {
				return int64(i)
			}
		}
		return int64(-1)
	}
	panic("engine binding: no value operation " + name)
}

// oprTypeName is the named node type of the operators of the rule i, if they have one.
func oprTypeName(i int) string { return fmt.Sprint("Op", i) }

// nodeOrNil is the handle of a node: the null value, which has one identity, for none.
func nodeOrNil(n *value.Node) any {
	if n == nil {
		return nil
	}
	return n
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
		return &attemptBox{a: x}, true
	case Mark:
		if x == (Mark{}) {
			return c.m.zeroMark, true
		}
		return &markBox{x}, true
	case RecEntry:
		return c.m.handle(x), true
	case *value.Node:
		return nodeOrNil(x), true
	}
	return nil, false
}

// oprExact are the operations of the family that count exactly their cost: copying the tokens of an
// attempt counts one unit for each, and recording again those of a reused one counts one for each
// and one for each error that it records one by one (spec/runtime/parsing.md#compute-units).
var oprExact = []string{"opr.attemptMissing", "opr.replay"}

// oprSuiteOps are the operations of the family that the engine binds, with those of the missing-token mode,
// and the budget's if controlled.
func oprSuiteOps(controlled bool) []string {
	ops := slices.Concat(conform.OprSuiteOps(), conform.OprMissingOps, conform.OprMissingSupportOps)
	if controlled {
		ops = append(ops, engineBudgetOps...)
	}
	return ops
}

// oprSuite is the suite of the engine's Operators Rule operations.
func oprSuite() conform.Suite {
	return conform.Suite{Name: "engine opr", Ops: oprSuiteOps(false), New: newOprMachine}
}

// oprControlledSuite is oprSuite in a parse that controls charges.
func oprControlledSuite() conform.Suite {
	return conform.Suite{Name: "engine opr (controlled)", Ops: oprSuiteOps(true), Exact: exactCharges(oprExact),
		New: func(c ref.Config) conform.Impl { return newOprMachineControlled(c, true) }}
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
		// Three seeds of four run in the missing-token mode.
		cfg := conform.OprMissingConfig()
		if seed%4 == 3 {
			cfg = conform.OprConfig()
		}
		run(conform.RandomOpr(fmt.Sprint("random_", seed), rng, slices.Concat(conform.OprOps, conform.OprMissingOps), n, cfg))
	}
	return fails, ran
}

func TestEngineOprConformance(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, s := range []conform.Suite{oprSuite(), oprControlledSuite()} {
		t.Run(s.Name, func(t *testing.T) {
			fails, ran := oprConformanceFailures(s, seeds, 150)
			if ran < seeds {
				t.Fatalf("only %d cases ran", ran)
			}
			for _, err := range fails {
				t.Error(err)
			}
		})
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
	for _, br := range slices.Concat(conform.OprBrokenOps(), conform.OprMatchBrokenOps(), conform.OprValueBrokenOps(), conform.OprMissingBrokenOps()) {
		t.Run(br.Name, func(t *testing.T) {
			s := oprSuite()
			s.New = func(c ref.Config) conform.Impl { return brokenOpr{newOprMachine(c).(*oprMachine), br.Op, br.Edit} }
			if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
				t.Fatal("no case detected the broken engine")
			}
		})
	}
}

type meteredBrokenOpr struct{ brokenOpr }

func (m meteredBrokenOpr) Used() uint64 { return m.p.Control.Used }

// TestEngineOprChargesArePinned is the negative control of the charges: the engine whose
// opr.attemptMissing or opr.replay charges one unit more or nothing at all fails the controlled suite,
// so that a change of what they charge for the tokens and the errors fails a test. The cases copy and
// record tokens again, so charging nothing changes what they count.
func TestEngineOprChargesArePinned(t *testing.T) {
	if !BudgetControlsEnabled {
		t.Skip("the build controls no charges")
	}
	for _, op := range oprExact {
		for _, change := range []string{"charges nothing", "charges one more"} {
			t.Run(op+" "+change, func(t *testing.T) {
				s := oprControlledSuite()
				s.New = func(c ref.Config) conform.Impl {
					m := newOprMachineControlled(c, true).(meteredOpr).oprMachine
					edit := func(call func(string, ...any) []any, args []any) []any {
						if change == "charges nothing" {
							ctl := m.p.Control
							m.p.Control = nil
							defer func() { m.p.Control = ctl }()
							return call(op, args...)
						}
						res := call(op, args...)
						m.p.Spend()
						return res
					}
					return meteredBrokenOpr{brokenOpr{m, op, edit}}
				}
				if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
					t.Fatalf("%s: %d failures", change, len(fails))
				}
			})
		}
	}
}

// TestEngineOprOperatorTypeIsChecked is the negative control of the operators' named node type: the
// engine whose opr.build leaves the applications, or only the values of the actions, without the type
// fails the suite.
func TestEngineOprOperatorTypeIsChecked(t *testing.T) {
	for name, untyped := range map[string]func(a *attemptBox) bool{
		"opr.build ignores the operators' type":    func(*attemptBox) bool { return true },
		"opr.build converts no action's value":     func(a *attemptBox) bool { return a.a.Op.Line.Act != nil },
		"opr.build types only the actions' values": func(a *attemptBox) bool { return a.a.Op.Line.Act == nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := oprSuite()
			s.New = func(c ref.Config) conform.Impl {
				m := newOprMachine(c).(*oprMachine)
				edit := func(call func(string, ...any) []any, args []any) []any {
					if r := args[0].(*Rule); untyped(args[1].(*attemptBox)) && r.OpKinds != nil {
						kinds := r.OpKinds
						r.OpKinds = nil
						defer func() { r.OpKinds = kinds }()
					}
					return call("opr.build", args...)
				}
				return brokenOpr{m, "opr.build", edit}
			}
			if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
				t.Fatal("no case detected the broken engine")
			}
		})
	}
}

// TestEngineOprConversionFailureIsChecked is the negative control of the failure of the conversion to
// the operators' named node type: the engine whose opr.build keeps a value that does not convert, or
// ends the parse with another interruption than the evaluation error, fails the suite.
func TestEngineOprConversionFailureIsChecked(t *testing.T) {
	for name, recovered := range map[string]func() []any{
		"opr.build keeps the value":         func() []any { return []any{nil} },
		"opr.build ends the parse on limit": func() []any { panic(code.Interrupt{Reason: "limit"}) },
	} {
		t.Run(name, func(t *testing.T) {
			s := oprSuite()
			s.New = func(c ref.Config) conform.Impl {
				edit := func(call func(string, ...any) []any, args []any) (res []any) {
					defer func() {
						if x := recover(); x != nil {
							if x != (code.Interrupt{Reason: "error"}) {
								panic(x)
							}
							res = recovered()
						}
					}()
					return call("opr.build", args...)
				}
				return brokenOpr{newOprMachine(c).(*oprMachine), "opr.build", edit}
			}
			if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
				t.Fatal("no case detected the broken engine")
			}
		})
	}
}
