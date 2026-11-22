// Package flowplan decides, for one rule table, which functions are lowered to Flow IR and in which
// form, before any backend prints or selects them. Every backend that runs the Node or recognizer
// table from Flow IR plans it here, so the generated Go parser and the bytecode VM run the same
// functions: a value-free flat rule where its body allows one, and otherwise the framed body that
// runs inside the rule frame its runtime owns.
//
// A plan lists functions (plandata.FuncSpec) in the order a backend lowers them, keyed by their use
// in the table, and fixes the capture-slot layout of each rule's frame, from which both the Go
// table rows and the VM's rule scopes derive. The route facts are analyses of Core IR and of the
// rules of the table (plandata.Rule, RecEligible, NodeFrameOK); the refusal of a flat function (Refused) is
// an analysis of its Flow IR as the lowering writes it, before any pass, which the plan makes once
// for every backend while the table is lowered (Unit).
package flowplan

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Plan plans the Node or recognizer table t, whose rules, value-free twins included, are rules by
// ID. live reports whether a rule is emitted; nil emits every rule. Each rule's route follows from its
// facts (route). Whether the table runs a flat function or its framed fallback is decided when the
// table is lowered (Unit), so that every backend that lowers the table from the plan runs the same
// functions.
func Plan(t *plandata.TableIR, rules []*plandata.Rule, live func(rule int) bool) *plandata.Plan {
	return route(t.M, rules, live)
}

// PlanTyped plans the typed table t, whose rules, value-free twins included, are rules by ID. live
// reports whether a rule is emitted; nil emits every rule. A rule that flowlower.LocalRule accepts is a
// flat function, which returns the rule's typed value and is never refused. A rule that TypedFrameOK
// accepts, whose Core body is a framed body and whose frame has the slots frameSlots returns for it, the
// layout of the frame that the typed runtime shares with the Node table, is a framed body. The lines of
// an Operators Rule are planned as in the Node table. A rule left out of the plan is one that Flow IR
// does not print, which the backend reports.
func PlanTyped(t *plandata.TableIR, rules []*plandata.Rule, live func(rule int) bool, frameSlots func(rule int) []string) *plandata.Plan {
	m := t.M
	p := &plandata.Plan{Typed: true}
	names := funcNames(rules)
	for i, r := range rules {
		cr := m.Rules[r.ID]
		if live != nil && !live(r.ID) {
			continue
		}
		if flowlower.LocalRule(cr) {
			p.Add(&plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: r.ID}, Func: lowerdata.Func{Form: lowerdata.Flat, Rule: cr, Name: names[i]}})
			continue
		}
		if _, ok := r.Body.(*ast.OperatorsRule); ok {
			continue
		}
		if TypedFrameOK(r) && flowlower.FramedBody(cr.Body) && slices.Equal(core.SlotNames(cr.Scope), frameSlots(r.ID)) {
			p.Add(&plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: r.ID}, Func: lowerdata.Func{Form: lowerdata.Frame, Rule: cr, Name: names[i], Body: cr.Body, Slots: cr.Scope, Build: cr.Want}})
		}
	}
	operatorsRules(p, m, rules, live, names)
	return p
}

// TypedFrameOK reports whether the body of the typed rule r runs in place under the frame the typed
// runtime owns: the rule is a left-recursion leader or recovers, and its body is one that a framed body
// may have.
func TypedFrameOK(r *plandata.Rule) bool {
	return (r.Leader || Recovers(r.Body)) && flowlower.BodyOK(r.Body, true, true)
}

// Recovers reports whether the expression e has a #recover attribute.
func Recovers(e ast.Expr) bool {
	found := false
	astinspect.WalkExpr(e, func(e ast.Expr) {
		if e, ok := e.(*ast.Attributed); ok {
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					found = true
				}
			}
		}
	})
	return found
}

// Stream adds to the plan p of the Node table t the body of its start rule r that ParseStream runs
// (plandata.StreamBody), and reports whether it did: the rule is lowered as a framed body (NodeFrameOK), with the
// capture layout of the rule's body in the table, which the action addresses by slot. The function comes
// after the others of the plan.
func Stream(p *plandata.Plan, t *plandata.TableIR, r *plandata.Rule) bool {
	cr := t.M.Rules[r.ID]
	if p.Typed || !t.Opts.Node || !NodeFrameOK(r, cr) {
		return false
	}
	p.Add(&plandata.FuncSpec{Key: plandata.Key{Use: plandata.StreamBody, Rule: r.ID}, Func: lowerdata.Func{Form: lowerdata.Frame, Rule: cr, Name: r.Name + "/stream", Body: cr.Body, Slots: cr.Scope, Build: !r.Lean, Stream: true}})
	return true
}

// Unit returns the planned table that the lowering of the table takes (flowlower.Lowering): the
// plan p, with the Core IR and the options of the table t, and the decision on each flat function of
// a plan of the Node or recognizer table, right after the lowering writes it and before any pass: a
// function that Refused rejects in its Flow IR, or that refuse rejects by its key (a test's hook; nil
// rejects none), is replaced by its framed fallback, or left out if the rule has none
// (plandata.Plan.Refusing). The decision reads the function as the lowering writes it, before any pass
// makes a form of one target, so it is one for every backend. The flat functions of the typed table
// are never refused.
func Unit(p *plandata.Plan, t *plandata.TableIR, refuse func(plandata.Key) bool) plandata.Planned {
	u := plandata.Planned{Plan: p, IR: t}
	if !p.Typed {
		u.Refuse = func(k plandata.Key, f *code.Func) bool { return Refused(f) || refuse != nil && refuse(k) }
	}
	return u
}

// route plans the routes of the table whose Core IR, after the Core passes of the table, is m, from
// the facts of its rules: a flat function where the rule may run flat, with its framed body as the
// fallback, and the framed body otherwise.
func route(m *core.Module, rules []*plandata.Rule, live func(rule int) bool) *plandata.Plan {
	t := &plandata.Plan{Layouts: make([][]string, len(rules))}
	add := t.Add
	names := funcNames(rules)
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
		var frame *plandata.FuncSpec
		if NodeFrameOK(r, cr) {
			frame = &plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: r.ID}, Func: lowerdata.Func{Form: lowerdata.Frame, Rule: cr, Name: name, Body: cr.Body, Slots: cr.Scope, Build: !r.Lean}}
		}
		if RecEligible(r) && flowlower.LocalRule(cr) {
			add(&plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: r.ID}, Func: lowerdata.Func{Form: lowerdata.Flat, Rule: cr, Name: name}, Fallback: frame})
		} else if frame != nil {
			add(frame)
		}
	}
	operatorsRules(t, m, rules, live, names)
	return t
}

// funcNames returns the names of the functions of the rules in the module of a table, which names its
// functions apart: a value-free twin, which has the name of its rule and comes after the rules, is
// named by the name of the rule and "/value-free".
func funcNames(rules []*plandata.Rule) []string {
	names := make([]string, len(rules))
	seen := map[string]bool{}
	for i, r := range rules {
		names[i] = r.Name
		if seen[r.Name] {
			names[i] += "/value-free"
		}
		seen[r.Name] = true
	}
	return names
}

// operatorsRules adds to the plan t the skips and the lines of the live Operators Rules of the rules, in
// the order of the rules, whose Core module is m and whose functions are named by names.
func operatorsRules(t *plandata.Plan, m *core.Module, rules []*plandata.Rule, live func(rule int) bool, names []string) {
	for i, r := range rules {
		e, ok := r.Body.(*ast.OperatorsRule)
		if !ok || live != nil && !live(r.ID) {
			continue
		}
		operators(m.Rules[r.ID], r, names[i], e, t.Add)
	}
}

// operators plans the skip and the lines of the Operators Rule e of the rule r, whose Core rule is
// cr and whose functions are named after rule. A line that Flow IR cannot lower inside the rule's
// frame is left out.
func operators(cr *core.Rule, r *plandata.Rule, rule string, e *ast.OperatorsRule, add func(*plandata.FuncSpec)) {
	co, _ := cr.Body.(*core.OperatorsRule)
	if co == nil {
		return
	}
	// A line is named by its rule and its use.
	line := func(k plandata.Key, l *core.OpLine, x ast.Expr, build bool) {
		if l != nil && flowlower.LineOK(x) && flowlower.FramedBody(l.Body) {
			name := fmt.Sprintf("%s/line%d", rule, k.Line)
			if k.Use == plandata.OperatorsSkip {
				name = rule + "/skip"
			}
			add(&plandata.FuncSpec{Key: k, Func: lowerdata.Func{Form: lowerdata.Frame, Rule: cr, Name: name, Body: l.Body, Slots: l.Scope, Build: build}})
		}
	}
	if e.Skip != nil {
		line(plandata.Key{Use: plandata.OperatorsSkip, Rule: r.ID}, co.Skip, e.Skip, false)
	}
	// Lines whose value is unused are matched without values; so are all lines of a rule without a
	// value.
	i := 0
	for _, o := range e.Operands {
		line(plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: i}, co.Operands[i], o.Expr, !r.Novalue && !check.LeanLine(o.Action, nil))
		i++
	}
	id := 0
	for _, lv := range e.Levels {
		for _, op := range lv.Operators {
			line(plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: i}, co.Ops[id].Line, op.Expr, !r.Novalue && !check.LeanLine(op.Action, []string{"lhs", "rhs", "op"}))
			i++
			id++
		}
	}
}

// Refused reports whether the table runs the framed fallback instead of the flat function f, which
// the lowering of a flat plandata.FuncSpec wrote: a reachable statement of f reads its entry position or its
// context, which a flat rule has no parameters for, or a variable of f holds a value, which a
// value-free flat rule does not keep (codepass.RefusesFlat). The plan decides on f as the lowering
// writes it (Unit); a backend that prints a flat function in forms of its own asserts that they keep
// the decision by asking codepass.RefusesFlat again on the function at the Exec stage.
func Refused(f *code.Func) bool {
	a := flow.AttrsOf(f)
	return codepass.RefusesFlat(f, a.Start, a.Ctx, flow.VarsOf(f))
}
