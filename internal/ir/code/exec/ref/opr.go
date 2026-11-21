package ref

import (
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// The reference model of the Operators Rule descriptors, the matches of operators and the records of
// the split forms of the loop (the opr operations and the oprLongest, oprNud and oprExpr records).
// The rules are those of Config.Oprs; the model keeps the handles of their lists, operators and
// lines, one for each, so that two calls that return the same one return the same handle. The grammar
// code of a rule, its trivia expression and the expressions of its operators' lines, is a script for
// each, as a rule's body is (RuleSpec.Body): its k-th evaluation in the parse does the k-th entry of the
// script, or the last once k passes it, and an empty script fails at once.

// OprOp is an operator of a rule of Config.Oprs.
type OprOp struct {
	// Kind is "prefix", "infix" or "postfix"; Assoc is "left", "right" or "none".
	Kind, Assoc string
	Level       int64
	Line        OprLineSpec
}

// OprLineSpec is a line of a rule of Config.Oprs, an operand or the expression of an operator: the
// number of its capture slots and the script of its expression (opr.match).
type OprLineSpec struct {
	Scope int64
	Expr  []OprExpr
}

// OprExpr is what one evaluation of the expression of a line does: what an evaluation of a body does
// (Eval), and whether the expression passes a cut, which sets the cut register whether or not it
// matches. The captures it sets are slots of the current frame, each set as frame.set sets it, to a
// value no other evaluation made, after the errors and before the position moves. A line that matches
// makes a value no other evaluation made.
type OprExpr struct {
	Eval
	Cut bool
}

// OprRule is an Operators Rule of Config.Oprs, the rule of the index in RuleRef.
type OprRule struct {
	Prefix, Led []OprOp
	// Operands are the operand lines.
	Operands []OprLineSpec
	// Skip is whether the rule skips trivia between its operands and operators, and Trivia the script
	// of its trivia expression (opr.skip), which leaves the cut register as it is and sets no captures.
	Skip   bool
	Trivia []Eval
}

// OprList is the model's Operators handle: a list of operators of a rule. Every list of no operators
// is one handle.
type OprList struct{ ops []*OprOperator }

// OprOperator is the model's Operator handle.
type OprOperator struct {
	op   OprOp
	line *OprLine
}

// OprLine is the model's Line handle: its capture slots, the script of its expression and the number
// of its evaluations so far.
type OprLine struct {
	scope  int64
	script []OprExpr
	evals  int64
}

// OprAttempt is the model's Attempt handle: a match of an operator. The null attempt is nil.
type OprAttempt struct {
	op         *OprOperator
	frame, v   any
	start, end int64
	env        *Scope
	cut        bool
	errs       any
}

// Opaque is the model's handle that a conformance case makes of a frame, a backtracking point, a value
// or a record of the split forms of the loop; it is only compared by identity.
type Opaque struct{ kind string }

// OprRecord is the model's handle of a record of a split form of the loop (oprLongest, oprNud and
// oprExpr). It holds the fields set so far; the others have the zero value of their type.
type OprRecord struct {
	prefix string
	fields map[string]any
}

type oprRule struct {
	prefix, led *OprList
	// trivia is the number of the evaluations of the rule's trivia expression so far.
	trivia int64
}

// OprRecords are the records of the split forms of the loop: the prefix of the operations of each, and
// the names of its fields.
var OprRecords = map[string][]string{
	"oprLongest": {"l", "i", "m0", "best", "cutFailed", "prevFrame", "prevCut", "f"},
	"oprNud":     {"m0", "a", "i", "prevFrame", "prevCut", "f"},
	"oprExpr":    {"min", "lhs", "lastNone", "bound", "guard", "m0", "a"},
}

// OprHandle returns a new handle of the model for the conformance operation name (conform.OprFrameOp
// and the others that make an opaque handle or a record).
func (m *Model) OprHandle(name string) any {
	if rec, ok := strings.CutPrefix(name, "test.oprRecord."); ok {
		return &OprRecord{prefix: rec, fields: map[string]any{}}
	}
	return &Opaque{kind: name}
}

func (m *Model) oprRule(i int64) *oprRule {
	for int64(len(m.oprs)) <= i {
		m.oprs = append(m.oprs, nil)
	}
	if m.oprs[i] == nil {
		r := m.cfg.Oprs[i]
		list := func(ops []OprOp) *OprList {
			if len(ops) == 0 {
				return &m.noOprs
			}
			l := &OprList{}
			for _, o := range ops {
				l.ops = append(l.ops, &OprOperator{op: o, line: newOprLine(o.Line)})
			}
			return l
		}
		m.oprs[i] = &oprRule{prefix: list(r.Prefix), led: list(r.Led)}
	}
	return m.oprs[i]
}

func newOprLine(s OprLineSpec) *OprLine { return &OprLine{scope: s.Scope, script: s.Expr} }

// oprOperands returns the lines of the operands of the rule i.
func (m *Model) oprOperands(i int64) []*OprLine {
	for int64(len(m.operands)) <= i {
		m.operands = append(m.operands, nil)
	}
	if m.operands[i] == nil {
		m.operands[i] = []*OprLine{}
		for _, s := range m.cfg.Oprs[i].Operands {
			m.operands[i] = append(m.operands[i], newOprLine(s))
		}
	}
	return m.operands[i]
}

// IsOprOp reports whether the model has the operation o of the Operators Rule descriptors, the
// matches and the records.
func IsOprOp(o *code.Op) bool { return isOpr(o.Name) }

func isOpr(name string) bool {
	if strings.HasPrefix(name, "opr.") {
		return true
	}
	rec, _, ok := strings.Cut(name, ".")
	return ok && OprRecords[rec] != nil
}

// recordField splits an operation of a record into the record's prefix, the field and whether it
// sets it.
func recordField(name string) (rec, field string, set bool) {
	rec, f, _ := strings.Cut(name, ".")
	if x, ok := strings.CutPrefix(f, "set"); ok && len(x) > 0 && x[0] >= 'A' && x[0] <= 'Z' {
		return rec, strings.ToLower(x[:1]) + x[1:], true
	}
	return rec, f, false
}

// allowsOpr reports whether a call of an operation of the Operators Rule family meets its precondition.
func (m *Model) allowsOpr(o *code.Op, args []any) bool {
	switch o.Name {
	case "opr.led", "opr.prefix", "opr.operands", "opr.hasSkip":
		return m.oprRuleOK(args[0])
	case "opr.skip":
		return m.oprRuleOK(args[0]) && m.cfg.Oprs[args[0].(RuleRef).Index].Skip
	case "opr.match":
		// The captures of the next evaluation are slots of the current frame.
		l := args[0].(*OprLine)
		e := scriptAt(l.script, l.evals)
		cur := m.frm.current
		for _, k := range e.Captures {
			if !liveFrame(cur) || k < 0 || k >= int64(len(cur.vals)) {
				return false
			}
		}
		return l != nil
	case "opr.operand":
		return m.oprRuleOK(args[0]) && args[1].(int64) >= 0 && args[1].(int64) < int64(len(m.cfg.Oprs[args[0].(RuleRef).Index].Operands))
	case "opr.at":
		l, i := args[0].(*OprList), args[1].(int64)
		return i >= 0 && i < int64(len(l.ops))
	case "opr.op", "opr.end", "opr.cut", "opr.env", "opr.errs":
		return args[0] != nil
	case "opr.attempt":
		return args[0] != nil
	}
	return true
}

// oprZero returns the value of the field of a record before it is set: the zero value of its type, 0,
// false, the list of no operators, the zero backtracking point, or the null handle.
func (m *Model) oprZero(field string) any {
	switch field {
	case "i", "min", "lastNone", "bound", "guard":
		return int64(0)
	case "cutFailed", "prevCut":
		return false
	case "l":
		return &m.noOprs
	case "m0":
		if m.zeroMark == nil {
			m.zeroMark = &Opaque{kind: "zero mark"}
		}
		return m.zeroMark
	}
	return nil
}

// scriptAt returns the k-th evaluation of a script: its k-th entry, the last once k passes it, and the
// zero entry, which fails at once, for an empty script.
func scriptAt[E any](script []E, k int64) E {
	var e E
	if len(script) > 0 {
		e = script[min(k, int64(len(script)-1))]
	}
	return e
}

func (m *Model) oprRuleOK(r any) bool {
	i := r.(RuleRef).Index
	return i >= 0 && i < int64(len(m.cfg.Oprs))
}

// callOpr runs an operation of the Operators Rule family, and reports false for any other.
func (m *Model) callOpr(o *code.Op, args []any) ([]any, bool) {
	if rec, field, set := recordField(o.Name); OprRecords[rec] != nil {
		r := args[0].(*OprRecord)
		if set {
			r.fields[field] = args[1]
			return nil, true
		}
		if v, ok := r.fields[field]; ok {
			return []any{v}, true
		}
		return []any{m.oprZero(field)}, true
	}
	switch o.Name {
	case "opr.led":
		return []any{m.oprRule(args[0].(RuleRef).Index).led}, true
	case "opr.prefix":
		return []any{m.oprRule(args[0].(RuleRef).Index).prefix}, true
	case "opr.count":
		return []any{int64(len(args[0].(*OprList).ops))}, true
	case "opr.at":
		return []any{args[0].(*OprList).ops[args[1].(int64)]}, true
	case "opr.operands":
		return []any{int64(len(m.cfg.Oprs[args[0].(RuleRef).Index].Operands))}, true
	case "opr.operand":
		return []any{m.oprOperands(args[0].(RuleRef).Index)[args[1].(int64)]}, true
	case "opr.line":
		return []any{args[0].(*OprOperator).line}, true
	case "opr.level":
		return []any{args[0].(*OprOperator).op.Level}, true
	case "opr.infix":
		return []any{args[0].(*OprOperator).op.Kind == "infix"}, true
	case "opr.postfix":
		return []any{args[0].(*OprOperator).op.Kind == "postfix"}, true
	case "opr.right":
		return []any{args[0].(*OprOperator).op.Assoc == "right"}, true
	case "opr.nonAssoc":
		return []any{args[0].(*OprOperator).op.Assoc == "none"}, true
	case "opr.scope":
		return []any{args[0].(*OprLine).scope}, true
	case "opr.hasSkip":
		return []any{m.cfg.Oprs[args[0].(RuleRef).Index].Skip}, true
	case "opr.skip":
		i := args[0].(RuleRef).Index
		r := m.oprRule(i)
		e := scriptAt(m.cfg.Oprs[i].Trivia, r.trivia)
		r.trivia++
		m.perform(e, m.regs["pos"].(int64), nil)
		return []any{e.OK}, true
	case "opr.match":
		l := args[0].(*OprLine)
		e := scriptAt(l.script, l.evals)
		l.evals++
		start := m.regs["pos"].(int64)
		m.perform(e.Eval, start, func() {
			for _, k := range e.Captures {
				m.frameSet(k, &Opaque{kind: "a capture"})
			}
		})
		if e.Cut {
			m.regs[exec.Cut] = true
		}
		if !e.OK {
			return []any{nil, false}, true
		}
		return []any{&Opaque{kind: "the value of a line"}, true}, true
	case "opr.attempt":
		return []any{&OprAttempt{op: args[0].(*OprOperator), frame: args[1], v: args[2], start: args[3].(int64),
			end: m.regs["pos"].(int64), env: m.env, cut: args[4].(bool), errs: args[5]}}, true
	case "opr.found":
		return []any{args[0] != nil}, true
	case "opr.op":
		return []any{args[0].(*OprAttempt).op}, true
	case "opr.end":
		return []any{args[0].(*OprAttempt).end}, true
	case "opr.cut":
		return []any{args[0].(*OprAttempt).cut}, true
	case "opr.env":
		return []any{args[0].(*OprAttempt).env}, true
	case "opr.errs":
		return []any{args[0].(*OprAttempt).errs}, true
	case "opr.setCutFailed":
		m.cutFailed = args[0].(bool)
		return nil, true
	case "opr.cutFailed":
		return []any{m.cutFailed}, true
	case "opr.setNudBound":
		m.nudBound = args[0].(int64)
		return nil, true
	case "opr.nudBound":
		return []any{m.nudBound}, true
	case "opr.setAfterEmpty":
		m.afterEmpty = args[0].(bool)
		return nil, true
	case "opr.afterEmpty":
		return []any{m.afterEmpty}, true
	}
	return nil, false
}
