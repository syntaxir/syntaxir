// Package flowplan decides, for one rule table, which functions are lowered to Flow IR and in which
// form, before any backend prints or selects them. Every backend that runs the Node or recognizer
// table from Flow IR plans it here, so the generated Go parser and the bytecode VM run the same
// functions: a value-free flat rule where its body allows one, and otherwise the framed body that
// runs inside the rule frame its runtime owns.
//
// A plan lists FuncSpecs in the order a backend lowers them, keyed by their use in the table, and
// fixes the capture-slot layout of each rule's frame, from which both the Go table rows and the
// VM's rule scopes derive. The route facts are analyses of Core IR and of the rules of the table
// (Rule, RecEligible, NodeFrameOK); the refusal of a flat function (Refused) is an analysis of its Flow IR
// as the lowering writes it, before any pass, which the plan makes once for every backend while the
// table is lowered (Table.Unit).
package flowplan

import (
	"fmt"
	"slices"

	"zeroparse.dev/zp/internal/frontend/check"
	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/codepass"
	"zeroparse.dev/zp/internal/middleend/execlower"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/pkg/grammar/ast"
)

// Use is what a planned function is in its table.
type Use uint8

const (
	// RuleBody is the body of a rule's table row: a flat rule or a framed body.
	RuleBody Use = iota
	// OperatorsSkip is the skip line of an Operators Rule.
	OperatorsSkip
	// OperatorsLine is a line of an Operators Rule: its operands in order, then its operators in the
	// order of their levels.
	OperatorsLine
)

// Key names a planned function by its use, its rule's ID and, for an Operators Rule line, the line's
// index.
type Key struct {
	Use  Use
	Rule int
	Line int
}

// FuncSpec is one function of a plan: its use in the table and exactly what its lowering takes
// (flowlower.Func), a flat function or a framed body.
type FuncSpec struct {
	Key Key
	flowlower.Func
	// Fallback is the framed body of the rule of a flat function, or nil if the rule has none. The plan
	// holds it in place of a flat function that Refused rejects.
	Fallback *FuncSpec
}

// Table is the plan of one rule table.
type Table struct {
	// Funcs holds the functions in the order a backend lowers them: the bodies of the rules by ID,
	// then the skip and the lines of each Operators Rule.
	Funcs []*FuncSpec
	// Layouts holds the capture-slot layout of each rule's frame by rule ID.
	Layouts [][]string
	byKey   map[Key]*FuncSpec
	// module is the module of the functions of the table, bound to the plan after the lowering
	// (Bind), and index the index of the function of each key in it.
	module *code.Module
	index  map[Key]int
}

// Func returns the function planned for the use k, or false if the table has none: the rule is left
// out, or Flow IR cannot lower it, which a backend reports.
func (t *Table) Func(k Key) (*FuncSpec, bool) {
	if t == nil {
		return nil, false
	}
	s, ok := t.byKey[k]
	return s, ok
}

// CheckLayout reports an error if the frame of rule has slots other than its planned layout. A
// backend calls it with the slots of each table row it emits.
func (t *Table) CheckLayout(rule int, name string, slots []string) error {
	if !slices.Equal(t.Layouts[rule], slots) && (len(t.Layouts[rule]) != 0 || len(slots) != 0) {
		return fmt.Errorf("internal error: the frame of rule %s has slots %v, not the planned %v", name, slots, t.Layouts[rule])
	}
	return nil
}

// Plan plans the Node or recognizer table t, whose rules, value-free twins included, are rules by
// ID. live reports whether a rule is emitted; nil emits every rule. Each rule's route follows from its
// facts (route). Whether the table runs a flat function or its framed fallback is decided when the
// table is lowered (Unit), so that every backend that lowers the table from the plan runs the same
// functions.
func Plan(t *TableIR, rules []*Rule, live func(rule int) bool) *Table {
	return route(t.M, rules, live)
}

// Unit returns what the lowering of the functions of the plan p takes (flowlower.Unit): the functions
// in the order of p, with the Core IR and the options of the table t, and the decision on each flat
// function, right after the lowering writes it and before any pass: a function that Refused rejects
// in its Flow IR, or that refuse rejects by its key (a test's hook; nil rejects none), is replaced in
// p and in the module by its framed fallback, or left out of both if the rule has none. The decision
// reads the function as the lowering writes it, before any pass makes a form of one target, so it is
// one for every backend. After the lowering, the functions of p are those of the module, in order.
func (p *Table) Unit(t *TableIR, refuse func(Key) bool) flowlower.Unit {
	specs := slices.Clone(p.Funcs)
	funcs := make([]flowlower.Func, len(specs))
	for i, s := range specs {
		funcs[i] = s.Func
		if s.Fallback != nil {
			fb := s.Fallback.Func
			funcs[i].Fallback = &fb
		}
	}
	return flowlower.Unit{Module: t.M, Opts: t.Opts, Funcs: funcs, Refuse: func(i int, f *code.Func) bool {
		s := specs[i]
		if !Refused(f) && (refuse == nil || !refuse(s.Key)) {
			return false
		}
		p.refuse(s)
		return true
	}}
}

// refuse replaces the flat function s of the plan by its fallback, or leaves it out if it has none.
func (p *Table) refuse(s *FuncSpec) {
	i := slices.Index(p.Funcs, s)
	delete(p.byKey, s.Key)
	if s.Fallback == nil {
		p.Funcs = slices.Delete(p.Funcs, i, i+1)
		return
	}
	p.Funcs[i] = s.Fallback
	p.byKey[s.Key] = s.Fallback
}

// Bind binds to the plan the module m of its functions, which the lowering of Unit made and the passes
// after it ran on: the function of each key is then the function at its place in m (Lowered,
// FlowFunc). A module that does not hold the functions of the plan in order is an internal error.
func (p *Table) Bind(m *code.Module) error {
	if len(m.Funcs) != len(p.Funcs) {
		return fmt.Errorf("internal error: the module of the table holds %d functions, not the %d of its plan", len(m.Funcs), len(p.Funcs))
	}
	p.index = make(map[Key]int, len(p.Funcs))
	for i, s := range p.Funcs {
		if m.Funcs[i].Name != s.Name {
			return fmt.Errorf("internal error: function %d of the module of the table is %s, not %s", i, m.Funcs[i].Name, s.Name)
		}
		p.index[s.Key] = i
	}
	p.module = m
	return nil
}

// Lowered returns the function of the use k in the Exec module bound to the plan (Bind), with the
// module and its variables (execlower.View), or false if the table has none.
func (p *Table) Lowered(k Key) (*execlower.Lowered, bool) {
	i, ok := p.index[k]
	if !ok {
		return nil, false
	}
	return execlower.View(p.module, i), true
}

// FlowFunc returns a module of the function of the use k alone (code.Module.FuncUnit), in the Flow
// module bound to the plan (Bind), or false if the table has none.
func (p *Table) FlowFunc(k Key) (*code.Module, bool) {
	i, ok := p.index[k]
	if !ok {
		return nil, false
	}
	return p.module.FuncUnit(i), true
}

// route plans the routes of the table whose Core IR, after the Core passes of the table, is m, from
// the facts of its rules: a flat function where the rule may run flat, with its framed body as the
// fallback, and the framed body otherwise.
func route(m *core.Module, rules []*Rule, live func(rule int) bool) *Table {
	t := &Table{Layouts: make([][]string, len(rules)), byKey: map[Key]*FuncSpec{}}
	add := func(s *FuncSpec) {
		t.Funcs = append(t.Funcs, s)
		t.byKey[s.Key] = s
	}
	// The functions of a table are named apart in its module: a value-free twin, which has the name of
	// its rule and comes after the rules, by the name of the rule and "/value-free".
	names := make([]string, len(rules))
	seen := map[string]bool{}
	for i, r := range rules {
		names[i] = r.Name
		if seen[r.Name] {
			names[i] += "/value-free"
		}
		seen[r.Name] = true
	}
	for i, r := range rules {
		cr := m.Rules[r.ID]
		t.Layouts[r.ID] = core.SlotNames(cr.Scope)
		name := names[i]
		if live != nil && !live(r.ID) {
			continue
		}
		if _, ok := r.Body.(*ast.OperatorsRule); ok {
			continue
		}
		var frame *FuncSpec
		if NodeFrameOK(r, cr) {
			frame = &FuncSpec{Key: Key{RuleBody, r.ID, 0}, Func: flowlower.Func{Form: flowlower.Frame, Rule: cr, Name: name, Body: cr.Body, Slots: cr.Scope, Build: !r.Lean}}
		}
		if RecEligible(r) && flowlower.LocalRule(cr) {
			add(&FuncSpec{Key: Key{RuleBody, r.ID, 0}, Func: flowlower.Func{Form: flowlower.Flat, Rule: cr, Name: name}, Fallback: frame})
		} else if frame != nil {
			add(frame)
		}
	}
	for i, r := range rules {
		e, ok := r.Body.(*ast.OperatorsRule)
		if !ok || live != nil && !live(r.ID) {
			continue
		}
		t.operators(m.Rules[r.ID], r, names[i], e, add)
	}
	return t
}

// operators plans the skip and the lines of the Operators Rule e of the rule r, whose Core rule is
// cr and whose functions are named after rule. A line that Flow IR cannot lower inside the rule's
// frame is left out.
func (t *Table) operators(cr *core.Rule, r *Rule, rule string, e *ast.OperatorsRule, add func(*FuncSpec)) {
	co, _ := cr.Body.(*core.OperatorsRule)
	if co == nil {
		return
	}
	// A line is named by its rule and its use.
	line := func(k Key, l *core.OpLine, x ast.Expr, build bool) {
		if l != nil && flowlower.LineOK(x) && flowlower.FramedBody(l.Body) {
			name := fmt.Sprintf("%s/line%d", rule, k.Line)
			if k.Use == OperatorsSkip {
				name = rule + "/skip"
			}
			add(&FuncSpec{Key: k, Func: flowlower.Func{Form: flowlower.Frame, Rule: cr, Name: name, Body: l.Body, Slots: l.Scope, Build: build}})
		}
	}
	if e.Skip != nil {
		line(Key{OperatorsSkip, r.ID, 0}, co.Skip, e.Skip, false)
	}
	// Lines whose value is unused are matched without values; so are all lines of a rule without a
	// value.
	i := 0
	for _, o := range e.Operands {
		line(Key{OperatorsLine, r.ID, i}, co.Operands[i], o.Expr, !r.Novalue && !check.LeanLine(o.Action, nil))
		i++
	}
	id := 0
	for _, lv := range e.Levels {
		for _, op := range lv.Operators {
			line(Key{OperatorsLine, r.ID, i}, co.Ops[id].Line, op.Expr, !r.Novalue && !check.LeanLine(op.Action, []string{"lhs", "rhs", "op"}))
			i++
			id++
		}
	}
}

// Refused reports whether the table runs the framed fallback instead of the flat function f, which
// the lowering of a Flat FuncSpec wrote: a reachable statement of f reads its entry position or its
// context, which a flat rule has no parameters for, or a variable of f holds a value, which a
// value-free flat rule does not keep. Reachability is that of the printers (codepass.DeadStores). The
// plan decides on f as the lowering writes it (Unit); a backend that prints a flat function in forms of
// its own asserts that they keep the decision by asking it again.
func Refused(f *code.Func) bool {
	a := flow.AttrsOf(f)
	reads := codepass.DeadStores(f, nil, func(code.Expr) bool { return false }).Named
	if a.Start != "" && reads[a.Start] || a.Ctx != "" && reads[a.Ctx] {
		return true
	}
	for _, v := range flow.VarsOf(f) {
		switch flow.TypeOf(v.Type) {
		case flow.TypeInt, flow.TypeBool, flow.TypeRune, flow.TypeMark, flow.TypeExpMark, flow.TypeExpIDs:
		default:
			return true
		}
	}
	return false
}
