package libgo

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The Operators Rules of the node and the typed library under conformance: the descriptors, the matches
// of operators, the grammar code of a rule (opr.skip, opr.match) and the values of its lines and
// applications (opr.lineValue, and opr.build or opr.buildTyped). Every operation runs through its binding
// in opr_bind_exec_test.go or topr_bind_exec_test.go, which the test that copies the library prints with
// the library's printer, so that the suite runs the code that the library's compositions run. The trivia
// of a rule and the lines of its operators run their scripts (ref.OprExpr) with the library's own
// operations, and the action of a line is the probe of ref.OprLineSpec. The library has neither the
// records of the split forms of the loop nor the missing-token mode.

// oprMachine binds the Operators Rule operations of the node or the typed library, the frame operations
// that the cases call, and the rest through the machine of the parse state.
type oprMachine struct {
	conform.Impl
	p *Parser
	// tp is the typed library's parser, nil for the node library.
	tp *TParser
	// rules are the rules of the cases: *Rule for the node library, *TRule for the typed one.
	rules    []any
	lists    map[oprListKey]*oprListBox
	regs     map[string]any
	zeroMark *oprMarkBox
	binder   conform.Binder
	used     func() uint64
}

type meteredOpr struct{ *oprMachine }

func (m meteredOpr) Used() uint64 { return m.used() }

// oprListBox is the handle of a list of operators; every empty list is one, as the model has it.
type oprListBox struct{ s any }

// oprListKey identifies a list of operators by its first operator and its length.
type oprListKey struct {
	first any
	n     int
}

// oprAttemptBox is the handle of a match of an operator: an operatorsRuleAttempt or a
// toperatorsRuleAttempt.
type oprAttemptBox struct{ a any }

type oprMarkBox struct{ m Mark }

// oprAtom is a typed value that is not a node (conform.OprAtomOp), such as the value of an action.
type oprAtom struct{ _ byte }

// oprTypeName is the named node type of the operators of the rule i, if they have one.
func oprTypeName(i int) string { return fmt.Sprint("Op", i) }

// oprTypeIndexes are the indexes of the named node types oprTypeName(i) in the typed library's
// registry, which is global: they are registered once.
var oprTypeIndexes = sync.OnceValue(func() []uint16 {
	var nts []uint16
	for i := range 8 {
		nts = append(nts, TRegisterNodeType(oprTypeName(i), nil))
	}
	return nts
})

// oprShaper is the shaper of the value of a line (ref.OprShape), as the printer makes it from the static
// shape of the line's expression.
func oprShaper(s ref.OprShape) Shaper {
	switch s {
	case ref.OprTuple:
		return Shaper{Elems: true, Tuple: true, Mode: singleTuple, K: 2}
	case ref.OprShared:
		return Shaper{Elems: true, Tuple: true, Shared: true, Mode: singleTuple, K: 2}
	case ref.OprNone:
		return Shaper{Mode: singleNone}
	case ref.OprWrap:
		return Shaper{Tuple: true, Mode: singlePass, K: 1}
	case ref.OprUnit:
		return Shaper{Tuple: true, Mode: singleNone}
	}
	return Shaper{Mode: singlePass}
}

func newOprMachine(cfg ref.Config, typed, controlled bool) conform.Impl {
	m := &oprMachine{lists: map[oprListKey]*oprListBox{}, zeroMark: &oprMarkBox{}}
	if typed {
		tm := newTypedMachine(cfg, controlled)
		var x *typedMachine
		switch y := tm.(type) {
		case *typedMachine:
			x = y
		case meteredTyped:
			x = y.typedMachine
		}
		m.Impl, m.p, m.tp = tm, x.p.Parser, x.p
	} else {
		rm := newRTMachine(cfg, controlled, false, false)
		var x *rtMachine
		switch y := rm.(type) {
		case *rtMachine:
			x = y
		case meteredRT:
			x = y.rtMachine
		}
		m.Impl, m.p = rm, x.p
	}
	m.p.sourceInfo = cfg.SourceInfo
	names := func(n int64) []string {
		s := make([]string, n)
		for k := range s {
			s[k] = fmt.Sprint("c", k)
		}
		return s
	}
	for i, r := range cfg.Oprs {
		rule := &Rule{Name: fmt.Sprint("r", i), Novalue: r.Novalue}
		rule.kinds = valueimpl.NewRuleKinds(rule.Name)
		if r.OpType {
			rule.OpNodeType = oprTypeName(i)
			rule.opKinds = NewNodeTypeKinds(rule.OpNodeType)
		}
		if typed {
			line := func(s ref.OprLineSpec) *TOperatorsRuleLine {
				l := &TOperatorsRuleLine{Scope: names(s.Scope), M: m.tscripted(s.Expr), Value: oprShaper(s.Shape)}
				if s.Action {
					l.Action = oprTProbe
					if s.NullAction {
						l.Action = func(*TActCtx) (any, any) { return nil, nil }
					}
				}
				return l
			}
			ops := func(os []ref.OprOp) []*TOperatorsRuleOp {
				var l []*TOperatorsRuleOp
				for k, o := range os {
					l = append(l, &TOperatorsRuleOp{Id: k + 1, Kind: o.Kind, Assoc: o.Assoc, Level: int(o.Level), Line: line(o.Line)})
				}
				return l
			}
			or := &TOperatorsRule{Prefix: ops(r.Prefix), Led: ops(r.Led)}
			for _, s := range r.Operands {
				or.Operands = append(or.Operands, line(s))
			}
			if r.Skip {
				or.Skip = m.tscripted(trivia(r.Trivia))
			}
			tr := &TRule{Rule: rule, OperatorsRule: or}
			if r.OpType {
				tr.OpNt = oprTypeIndexes()[i]
			}
			m.rules = append(m.rules, tr)
			continue
		}
		line := func(s ref.OprLineSpec) *OperatorsRuleLine {
			l := &OperatorsRuleLine{Scope: names(s.Scope), M: m.scripted(s.Expr), Value: oprShaper(s.Shape)}
			if s.Action {
				l.Action = oprProbe
				if s.NullAction {
					l.Action = func(*ActCtx) (any, any) { return nil, nil }
				}
			}
			return l
		}
		ops := func(os []ref.OprOp) []*OperatorsRuleOp {
			var l []*OperatorsRuleOp
			for k, o := range os {
				l = append(l, &OperatorsRuleOp{Id: k + 1, Kind: o.Kind, Assoc: o.Assoc, Level: int(o.Level), Line: line(o.Line)})
			}
			return l
		}
		or := &OperatorsRule{Prefix: ops(r.Prefix), Led: ops(r.Led)}
		for _, s := range r.Operands {
			or.Operands = append(or.Operands, line(s))
		}
		if r.Skip {
			or.Skip = m.scripted(trivia(r.Trivia))
		}
		rule.OperatorsRule = or
		m.rules = append(m.rules, rule)
	}
	var methods map[string]any
	if typed {
		methods = confTOprBindings(m.tp)
		maps.Copy(methods, confTFrameBindings(m.tp))
	} else {
		methods = confOprBindings(m.p)
		maps.Copy(methods, confFrameBindings(m.p))
	}
	m.binder = conform.Binder{Methods: methods, Conv: oprConv{m}}
	if controlled {
		m.used = func() uint64 { return m.p.UnitsNow() }
		return meteredOpr{m}
	}
	return m
}

// trivia returns the script of the trivia of a rule as the script of a line, which passes no cut.
func trivia(script []ref.Eval) []ref.OprExpr {
	exprs := make([]ref.OprExpr, len(script))
	for k, e := range script {
		if len(e.Captures) > 0 {
			panic("library binding: the trivia of an Operators Rule sets no captures")
		}
		exprs[k].Eval = e
	}
	return exprs
}

// evalAt returns the k-th evaluation of script: its k-th entry, or the last once k passes it, and an
// entry that fails at once for an empty script.
func evalAt(script []ref.OprExpr, k int) ref.OprExpr {
	var e ref.OprExpr
	if len(script) > 0 {
		e = script[min(k, len(script)-1)]
	}
	if len(e.Missing) > 0 || e.Touch > 0 {
		panic("library binding: the scripts of the Operators Rules insert no tokens and examine no input")
	}
	return e
}

// perform does what the evaluation e does before its captures: its charge and the errors it recovers.
func (m *oprMachine) perform(e ref.OprExpr) {
	if e.Charge > 0 {
		m.Impl.Call(oprOps.Op("budget.charge"), []any{e.Charge})
	}
	for _, id := range e.Errs {
		m.Impl.Call(oprOps.Op("rec.push"), []any{m.Err(id)})
	}
}

// scripted returns the grammar code of a line or of the trivia of the node library that runs script.
func (m *oprMachine) scripted(script []ref.OprExpr) matcher {
	evals := 0
	return func(p *Parser) (*sxirvalue.Node, bool) {
		e := evalAt(script, evals)
		evals++
		start := p.Pos
		m.perform(e)
		// A capture of the line is a write to a slot of the current frame, as the grammar code's is.
		for _, k := range e.Captures {
			m.binder.Call(oprOps.Op("frame.set"), []any{k, p.buildNode(Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(start), Fresh: true})})
		}
		if e.Cut {
			p.Cut = true
		}
		p.Pos = start + int(e.Len)
		if !e.OK {
			return nil, false
		}
		return p.buildNode(Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Fresh: true}), true
	}
}

// tscripted is scripted for the typed library.
func (m *oprMachine) tscripted(script []ref.OprExpr) tmatcher {
	evals := 0
	return func(p *TParser) (any, bool) {
		e := evalAt(script, evals)
		evals++
		start := p.Pos
		m.perform(e)
		for _, k := range e.Captures {
			m.binder.Call(oprOps.Op("frame.set"), []any{k, p.NewNode("tuple", start, start, nil)})
		}
		if e.Cut {
			p.Cut = true
		}
		p.Pos = start + int(e.Len)
		if !e.OK {
			return nil, false
		}
		return p.NewNode("tuple", start, p.Pos, nil), true
	}
}

// oprProbe is the action of a line (ref.OprLineSpec): a node of the range of its input whose elements
// are the locals op, lhs and rhs and whose fields are the captures of its frame.
func oprProbe(c *ActCtx) (any, any) {
	local := func(v any) *sxirvalue.Node {
		n, _ := v.(*sxirvalue.Node)
		return n
	}
	d := Data{Kind: valueimpl.SeqKind, Start: int32(c.start), End: int32(c.end),
		Children: []*sxirvalue.Node{local(c.Op), local(c.Lhs), local(c.Rhs)}}
	for k, v := range c.f.Vals {
		if v != nil {
			valueimpl.SetField(&d.Fields, fmt.Sprint("c", k), v)
		}
	}
	return c.P.buildNode(d), nil
}

// oprTProbe is oprProbe for the typed library.
func oprTProbe(c *TActCtx) (any, any) {
	n := c.P.NewNode("tuple", c.Start, c.End, []any{c.Op, c.Lhs, c.Rhs})
	for k, v := range c.F.Vals {
		if v != nil {
			n.fields = append(n.fields, tfield{fmt.Sprint("c", k), v})
		}
	}
	return n, nil
}

var oprOps = conform.OprModule()

func (m *oprMachine) Rule(index int64) any { return m.rules[index] }

func (m *oprMachine) Datum(id int64) any { return m.Impl.(conform.Environment).Datum(id) }

func (m *oprMachine) Err(id int64) any { return m.Impl.(conform.Recovery).Err(id) }

func (m *oprMachine) Recovered() []int64 { return m.Impl.(conform.Recovery).Recovered() }

func (m *oprMachine) Unrolled() int { return m.Impl.(conform.Recovery).Unrolled() }

func (m *oprMachine) List(l any) (visits, errs int) { return m.Impl.(conform.Recovery).List(l) }

// UseRegisters hands the machine the registers of the run: opr.attempt reads the position, and the
// grammar code of a rule reads and writes the position, the cut flag and the silent depth.
func (m *oprMachine) UseRegisters(regs map[string]any) { m.regs = regs }

func (m *oprMachine) Handle(name string) any {
	switch name {
	case conform.OprFrameOp:
		if m.tp != nil {
			return &TCaptureFrame{}
		}
		return &CaptureFrame{}
	case conform.OprMarkOp:
		return &oprMarkBox{m.p.Mark()}
	case conform.OprNodeOp:
		// A value that a rule has taken: an empty node at the start of the input.
		if m.tp != nil {
			n := m.tp.NewNode("tuple", 0, 0, nil)
			n.fresh = false
			return n
		}
		return m.p.buildNode(Data{Kind: valueimpl.SeqKind})
	case conform.OprAtomOp:
		return &oprAtom{}
	}
	panic("library binding: no handle " + name)
}

func (m *oprMachine) Call(o *code.Op, args []any) []any {
	if slices.Contains(ref.OprValueOps, o.Name) {
		return []any{m.valueOp(o.Name, args)}
	}
	if o.Name == ref.FrameSlotOp {
		var v any
		switch f := args[0].(type) {
		case *CaptureFrame:
			if x := f.Vals[args[1].(int64)]; x != nil {
				v = x
			}
		case *TCaptureFrame:
			v = f.Vals[args[1].(int64)]
		}
		return []any{v}
	}
	if !strings.HasPrefix(o.Name, "opr") && !strings.HasPrefix(o.Name, "frame.") {
		return m.Impl.Call(o, args)
	}
	defer interrupted()
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

// valueOp takes a value apart (ref.OprValueOps).
func (m *oprMachine) valueOp(name string, args []any) any {
	if m.tp != nil {
		return m.tvalueOp(name, args)
	}
	d := D(args[0].(*sxirvalue.Node))
	switch name {
	case ref.OprValueStartOp:
		return int64(d.Start)
	case ref.OprValueEndOp:
		return int64(d.End)
	case ref.OprValueKidsOp:
		return int64(len(d.Children))
	case ref.OprValueKidOp:
		if n := d.Children[args[1].(int64)]; n != nil {
			return n
		}
		return nil
	case ref.OprValueFieldOp:
		v, _ := d.Fields.Get(ref.OprFieldNames[args[1].(int64)])
		if n, ok := v.(*sxirvalue.Node); ok && n == nil {
			return nil
		}
		return v
	case ref.OprValueRuleOp:
		for i, r := range m.rules {
			if d.Kind != nil && d.Kind.Rule != "" && d.Kind.Rule == r.(*Rule).Name {
				return int64(i)
			}
		}
		return int64(-1)
	case ref.OprValueTypeOp:
		for i := range m.rules {
			if d.Kind.IsNodeType() && d.Kind.Type == oprTypeName(i) {
				return int64(i)
			}
		}
		return int64(-1)
	}
	panic("library binding: no value operation " + name)
}

// tvalueOp is valueOp for the typed library, whose nodes carry no rule's name.
func (m *oprMachine) tvalueOp(name string, args []any) any {
	n := AsTval(args[0])
	switch name {
	case ref.OprValueStartOp:
		start, _ := n.TSpan()
		return int64(start)
	case ref.OprValueEndOp:
		_, end := n.TSpan()
		return int64(end)
	case ref.OprValueKidsOp:
		return int64(len(n.TKids()))
	case ref.OprValueKidOp:
		return n.TKids()[args[1].(int64)]
	case ref.OprValueFieldOp:
		if s, ok := n.(*TSeq); ok {
			for _, f := range s.fields {
				if f.name == ref.OprFieldNames[args[1].(int64)] {
					return f.val
				}
			}
		}
		return nil
	case ref.OprValueRuleOp:
		return int64(-1)
	case ref.OprValueTypeOp:
		if s, ok := n.(*TSeq); ok && s.nt != 0 {
			if i := slices.Index(oprTypeIndexes(), s.nt); i < len(m.rules) {
				return int64(i)
			}
		}
		return int64(-1)
	}
	panic("typed library binding: no value operation " + name)
}

// oprConv converts the handles of the Operators Rule operations of the library.
type oprConv struct{ m *oprMachine }

func (c oprConv) ToImpl(arg any, t reflect.Type) (reflect.Value, bool) {
	switch a := arg.(type) {
	case *oprListBox:
		return reflect.ValueOf(a.s), true
	case *oprAttemptBox:
		return reflect.ValueOf(a.a), true
	case *oprMarkBox:
		return reflect.ValueOf(a.m), true
	case *frameMarkBox:
		return reflect.ValueOf(a.m), true
	case *errorList:
		return reflect.ValueOf(a.e), true
	}
	return reflect.Value{}, false
}

func (c oprConv) FromImpl(v reflect.Value) (any, bool) {
	list := func(first any, n int, s any) any {
		k := oprListKey{first: first, n: n}
		b := c.m.lists[k]
		if b == nil {
			b = &oprListBox{s}
			c.m.lists[k] = b
		}
		return b
	}
	switch x := v.Interface().(type) {
	case []*OperatorsRuleOp:
		var first any
		if len(x) > 0 {
			first = x[0]
		}
		return list(first, len(x), x), true
	case []*TOperatorsRuleOp:
		var first any
		if len(x) > 0 {
			first = x[0]
		}
		return list(first, len(x), x), true
	case operatorsRuleAttempt:
		if x.op == nil {
			// The null attempt is no handle.
			return nil, true
		}
		return &oprAttemptBox{x}, true
	case toperatorsRuleAttempt:
		if x.op == nil {
			return nil, true
		}
		return &oprAttemptBox{x}, true
	case Mark:
		if x == (Mark{}) {
			return c.m.zeroMark, true
		}
		return &frameMarkBox{x}, true
	case recEntry:
		return c.m.recHandles().handle(x), true
	}
	return nil, false
}

// recHandles returns the handles of the lists of recovered errors of the machine of the parse state.
func (m *oprMachine) recHandles() recHandles {
	switch x := m.Impl.(type) {
	case *rtMachine:
		return x.lists
	case meteredRT:
		return x.lists
	case *typedMachine:
		return x.lists
	case meteredTyped:
		return x.lists
	}
	panic("library binding: no lists of recovered errors")
}

// oprSuiteOps are the operations of the family that the node or the typed library binds, and the
// budget's if controlled.
func oprSuiteOps(typed, controlled bool) []string {
	build := conform.OprValueBuildOps
	if typed {
		build = []string{"opr.lineValue", "opr.buildTyped"}
	}
	ops := slices.Concat(conform.OprDescriptorOps, conform.OprMatchOps, build, conform.OprSupportOps, conform.OprFrameOps, ref.OprValueOps)
	if controlled {
		ops = append(ops, "budget.charge", "budget.used")
	}
	return ops
}

// oprSuite is the suite of the Operators Rule operations of the node or the typed library, metered if
// controlled. The typed library's values carry no rule's name.
func oprSuite(typed, controlled bool) conform.Suite {
	name := "runtime opr"
	if typed {
		name = "typed library opr"
	}
	if controlled {
		name += " (budget)"
	}
	return conform.Suite{Name: name, Ops: oprSuiteOps(typed, controlled), Typed: typed,
		New: func(c ref.Config) conform.Impl { return newOprMachine(c, typed, controlled) }}
}

// oprConformanceFailures runs the handwritten cases of the Operators Rule family and seeds randomized
// cases of n calls on s, and returns the failures and the number of cases that ran. The cases of the
// records of the split forms and of the missing-token mode are skipped.
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
	random := slices.Concat(conform.OprDescriptorOps, conform.OprMatchOps, conform.OprValueBuildOps)
	if s.Typed {
		random = slices.Concat(conform.OprDescriptorOps, conform.OprMatchOps, []string{"opr.lineValue"}, conform.OprTypedOps)
	}
	for seed := range seeds {
		rng := rand.New(rand.NewPCG(uint64(seed), 66))
		run(conform.RandomOpr(fmt.Sprint("random_", seed), rng, random, n, conform.OprConfig()))
	}
	return fails, ran
}

func TestExecConformanceOpr(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, typed := range []bool{false, true} {
		for _, controlled := range []bool{false, true} {
			s := oprSuite(typed, controlled)
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
}

// brokenOpr is the library with one operation of the Operators Rule family replaced.
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

// TestExecConformanceOprDetectsBrokenLib is the negative control: the node and the typed library with
// one operation of the family broken, as a wrong line of its printed code would break it, fail the suite.
func TestExecConformanceOprDetectsBrokenLib(t *testing.T) {
	for _, typed := range []bool{false, true} {
		for _, br := range slices.Concat(conform.OprBrokenOps(), conform.OprMatchBrokenOps(), conform.OprValueBrokenOps(), conform.OprTypedBrokenOps()) {
			// The typed library has no opr.build for a mutant to call in place of opr.buildTyped.
			if !slices.Contains(oprSuiteOps(typed, false), br.Op) || !strings.HasPrefix(br.Op, "opr.") || br.Name == "opr.buildTyped is opr.build" {
				continue
			}
			t.Run(fmt.Sprint(br.Name, " typed=", typed), func(t *testing.T) {
				s := oprSuite(typed, false)
				s.New = func(c ref.Config) conform.Impl {
					return brokenOpr{newOprMachine(c, typed, false).(*oprMachine), br.Op, br.Edit}
				}
				if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
					t.Fatal("no case detected the broken library")
				}
			})
		}
	}
}

// TestExecConformanceOprOperatorTypeIsChecked is the negative control of the operators' named node type:
// the library whose opr.build or opr.buildTyped leaves the applications, or only the values of the
// actions, without the type fails the suite.
func TestExecConformanceOprOperatorTypeIsChecked(t *testing.T) {
	for _, typed := range []bool{false, true} {
		op := "opr.build"
		if typed {
			op = "opr.buildTyped"
		}
		for name, untyped := range map[string]func(action bool) bool{
			"ignores the operators' type":    func(bool) bool { return true },
			"converts no action's value":     func(action bool) bool { return action },
			"types only the actions' values": func(action bool) bool { return !action },
		} {
			t.Run(fmt.Sprint(op, " ", name), func(t *testing.T) {
				s := oprSuite(typed, false)
				s.New = func(c ref.Config) conform.Impl {
					m := newOprMachine(c, typed, false).(*oprMachine)
					edit := func(call func(string, ...any) []any, args []any) []any {
						switch r := args[0].(type) {
						case *Rule:
							if a := args[1].(*oprAttemptBox).a.(operatorsRuleAttempt); untyped(a.op.Line.Action != nil) && r.opKinds != nil {
								kinds := r.opKinds
								r.opKinds = nil
								defer func() { r.opKinds = kinds }()
							}
						case *TRule:
							if a := args[1].(*oprAttemptBox).a.(toperatorsRuleAttempt); untyped(a.op.Line.Action != nil) && r.OpNt != 0 {
								nt := r.OpNt
								r.OpNt = 0
								defer func() { r.OpNt = nt }()
							}
						}
						return call(op, args...)
					}
					return brokenOpr{m, op, edit}
				}
				if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
					t.Fatal("no case detected the broken library")
				}
			})
		}
	}
}

// TestExecConformanceOprConversionFailureIsChecked is the negative control of the failure of the
// conversion to the operators' named node type: the library whose opr.build or opr.buildTyped keeps a
// value that does not convert, or ends the parse with another interruption than the evaluation error,
// fails the suite.
func TestExecConformanceOprConversionFailureIsChecked(t *testing.T) {
	for _, typed := range []bool{false, true} {
		op := "opr.build"
		if typed {
			op = "opr.buildTyped"
		}
		for name, recovered := range map[string]func() []any{
			"keeps the value":         func() []any { return []any{nil, int64(0)}[:len(oprOps.Op(op).Results)] },
			"ends the parse on limit": func() []any { panic(code.Interrupt{Reason: "limit"}) },
		} {
			t.Run(fmt.Sprint(op, " ", name), func(t *testing.T) {
				s := oprSuite(typed, false)
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
						return call(op, args...)
					}
					return brokenOpr{newOprMachine(c, typed, false).(*oprMachine), op, edit}
				}
				if fails, _ := oprConformanceFailures(s, 60, 250); len(fails) == 0 {
					t.Fatal("no case detected the broken library")
				}
			})
		}
	}
}
