package ref

import (
	"fmt"
	"slices"
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
// number of its capture slots, the script of its expression (opr.match), how its value comes from the
// value of its expression (Shape) and whether an action makes it instead (Action).
//
// The action of a line is a probe that shows what it was given: a node of the range of its input,
// whose elements are the locals op, lhs and rhs (null where the action has none) and whose fields
// c0, c1, ... are the captures in the slots of its frame that are not empty. With NullAction it
// returns the null value instead, which no named node type converts.
type OprLineSpec struct {
	Scope      int64
	Expr       []OprExpr
	Shape      OprShape
	Action     bool
	NullAction bool
}

// OprShape is how the value of a line without an action comes from the value of its expression, as
// the grammar's static shape of the expression decides.
type OprShape int

const (
	// OprPass is an expression of one element: its value is the line's.
	OprPass OprShape = iota
	// OprTuple is a sequence, whose value is the node of its tuple: that node is the line's value, and
	// for an operator's line also takes the line's captures.
	OprTuple
	// OprShared is OprTuple where the tuple node is the result of a splice rule that a memo entry
	// shares: the line's value is a fresh node of the range and the elements of it.
	OprShared
	// OprNone is an operator's line whose expression has no value: the operator's value is a fresh
	// node of no elements of the range of its match. An operand line has a value.
	OprNone
	// OprWrap is an expression of one element that is not a sequence but whose scope's value is a
	// tuple node, as an element with captures: an operand line's value is a fresh node of the range of
	// its match with the element, and an operator's value is the element, as for OprPass.
	OprWrap
	// OprUnit is an expression of no valued element that is not a sequence: an operand line's value is
	// a fresh node of the range of its match with no elements, and an operator's value is one of the
	// range of its match, as for OprNone.
	OprUnit
)

// OprFieldNames are the names of the fields that a case reads (OprValueFieldOp), by index: those of
// the node of an operator's application, then the captures of the slots 0, 1 and 2.
var OprFieldNames = []string{"lhs", "op", "rhs", "c0", "c1", "c2"}

// The conformance operations that take a value of the Operators Rule family apart. They are not
// operations of the parser VM.
const (
	OprValueStartOp = "test.valueStart"
	OprValueEndOp   = "test.valueEnd"
	OprValueKidsOp  = "test.valueKids"
	OprValueKidOp   = "test.valueKid"
	OprValueFieldOp = "test.valueField"
	OprValueRuleOp  = "test.valueRule"
	OprValueTypeOp  = "test.valueType"
)

// OprValueOps are the operations that take a value apart.
var OprValueOps = []string{OprValueStartOp, OprValueEndOp, OprValueKidsOp, OprValueKidOp, OprValueFieldOp, OprValueRuleOp, OprValueTypeOp}

// OprValue is the model's Value handle of the Operators Rule family, a node: its range, its elements,
// its fields in the order they were first set, the index of the rule whose name it carries (-1 for
// none), the rule whose operators' named node type it has (OprRule.OpType) and whether it is fresh, a
// value that the grammar code has just made and no rule has taken.
type OprValue struct {
	start, end int64
	kids       []any
	fields     []oprField
	rule       int64
	// opType is one more than the index of the rule whose operators' named node type the node has,
	// 0 for none.
	opType int64
	fresh  bool
}

type oprField struct {
	name string
	v    any
}

// setField sets the field name of n to v, in place of the value it had.
func (n *OprValue) setField(name string, v any) {
	for k := range n.fields {
		if n.fields[k].name == name {
			n.fields[k].v = v
			return
		}
	}
	n.fields = append(n.fields, oprField{name, v})
}

func (n *OprValue) field(name string) any {
	for _, f := range n.fields {
		if f.name == name {
			return f.v
		}
	}
	return nil
}

// isOprNode reports whether v is a value of the family that is not null.
func isOprNode(v any) bool {
	n, ok := v.(*OprValue)
	return ok && n != nil
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
	// Novalue is a rule whose value nothing uses: its lines and applications have the null value.
	Novalue bool
	// OpType is whether the rule's operators have a named node type: an application is a node of the
	// type, and the value of an operator's action is converted to it.
	OpType      bool
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

// OprLine is the model's Line handle: its spec and the number of the evaluations of its expression so
// far.
type OprLine struct {
	spec  OprLineSpec
	evals int64
}

// OprAttempt is the model's Attempt handle: a match of an operator. The null attempt is nil.
type OprAttempt struct {
	op         *OprOperator
	frame, v   any
	start, end int64
	env        *Scope
	cut        bool
	errs       any
	// missing holds the tokens inserted after the mark of opr.attemptMissing, nil for opr.attempt.
	missing []missingToken
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
	if name == "test.node" {
		// A value that a rule has taken: an empty node at the start of the input.
		return &OprValue{rule: -1}
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

func newOprLine(s OprLineSpec) *OprLine { return &OprLine{spec: s} }

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
	if strings.HasPrefix(name, "opr.") || strings.HasPrefix(name, "test.value") {
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
		e := scriptAt(l.spec.Expr, l.evals)
		cur := m.frm.current
		for _, k := range e.Captures {
			if !liveFrame(cur) || k < 0 || k >= int64(len(cur.vals)) {
				return false
			}
		}
		return l != nil
	case "opr.lineValue":
		// l is an operand line of r, and f a frame of its slots.
		if !m.oprRuleOK(args[0]) {
			return false
		}
		l := args[1].(*OprLine)
		return l != nil && slices.Contains(m.oprOperands(args[0].(RuleRef).Index), l) && oprFrameOf(args[2], l)
	case "opr.build", "opr.buildTyped":
		// The operator of a is one of r, its frame is a frame of its line's slots, and a prefix
		// operator has no left operand and a postfix one no right operand.
		if !m.oprRuleOK(args[0]) || args[1] == nil {
			return false
		}
		a := args[1].(*OprAttempt)
		r := m.oprRule(args[0].(RuleRef).Index)
		if !slices.Contains(r.prefix.ops, a.op) && !slices.Contains(r.led.ops, a.op) || !oprFrameOf(a.frame, a.op.line) {
			return false
		}
		rhs := args[len(args)-1]
		return (a.op.op.Kind != "prefix" || args[2] == nil) && (a.op.op.Kind != "postfix" || rhs == nil)
	case OprValueStartOp, OprValueEndOp, OprValueKidsOp, OprValueRuleOp, OprValueTypeOp:
		return isOprNode(args[0])
	case OprValueKidOp:
		return isOprNode(args[0]) && args[1].(int64) >= 0 && args[1].(int64) < int64(len(args[0].(*OprValue).kids))
	case OprValueFieldOp:
		return isOprNode(args[0]) && args[1].(int64) >= 0 && args[1].(int64) < int64(len(OprFieldNames))
	case "opr.operand":
		return m.oprRuleOK(args[0]) && args[1].(int64) >= 0 && args[1].(int64) < int64(len(m.cfg.Oprs[args[0].(RuleRef).Index].Operands))
	case "opr.at":
		l, i := args[0].(*OprList), args[1].(int64)
		return i >= 0 && i < int64(len(l.ops))
	case "opr.op", "opr.end", "opr.cut", "opr.env", "opr.errs":
		return args[0] != nil
	case "opr.attempt":
		return args[0] != nil
	case "opr.attemptMissing":
		mark := args[6].(int64)
		return args[0] != nil && mark >= 0 && mark <= int64(len(m.missing.path))
	case "opr.replay":
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

// oprFrameOf reports whether f is a frame of the slots of the line l.
func oprFrameOf(f any, l *OprLine) bool {
	return liveFrame(f) && int64(len(f.(*Frame).vals)) == l.spec.Scope
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
		return []any{args[0].(*OprLine).spec.Scope}, true
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
		e := scriptAt(l.spec.Expr, l.evals)
		l.evals++
		start := m.regs["pos"].(int64)
		m.perform(e.Eval, start, func() {
			for _, k := range e.Captures {
				// A capture is a fresh node; its range is not what a case reads.
				m.frameSet(k, &OprValue{start: start, end: start, rule: -1, fresh: true})
			}
		})
		if e.Cut {
			m.regs[exec.Cut] = true
		}
		if !e.OK {
			return []any{nil, false}, true
		}
		return []any{&OprValue{start: start, end: m.regs["pos"].(int64), rule: -1, fresh: true}, true}, true
	case "opr.lineValue":
		return []any{m.oprLineValue(args[0].(RuleRef).Index, args[1].(*OprLine), args[2].(*Frame), args[3], args[4].(int64))}, true
	case "opr.build":
		v, _ := m.oprBuild(args[0].(RuleRef).Index, args[1].(*OprAttempt), args[2], 0, args[3], false)
		return []any{v}, true
	case "opr.buildTyped":
		v, start := m.oprBuild(args[0].(RuleRef).Index, args[1].(*OprAttempt), args[2], args[3].(int64), args[4], true)
		return []any{v, start}, true
	case OprValueStartOp:
		return []any{args[0].(*OprValue).start}, true
	case OprValueEndOp:
		return []any{args[0].(*OprValue).end}, true
	case OprValueKidsOp:
		return []any{int64(len(args[0].(*OprValue).kids))}, true
	case OprValueKidOp:
		return []any{args[0].(*OprValue).kids[args[1].(int64)]}, true
	case OprValueFieldOp:
		return []any{args[0].(*OprValue).field(OprFieldNames[args[1].(int64)])}, true
	case OprValueRuleOp:
		return []any{args[0].(*OprValue).rule}, true
	case OprValueTypeOp:
		return []any{args[0].(*OprValue).opType - 1}, true
	case "opr.attempt":
		return []any{&OprAttempt{op: args[0].(*OprOperator), frame: args[1], v: args[2], start: args[3].(int64),
			end: m.regs["pos"].(int64), env: m.env, cut: args[4].(bool), errs: args[5]}}, true
	case "opr.attemptMissing":
		return []any{&OprAttempt{op: args[0].(*OprOperator), frame: args[1], v: args[2], start: args[3].(int64),
			end: m.regs["pos"].(int64), env: m.env, cut: args[4].(bool), errs: args[5],
			missing: slices.Clone(m.missing.path[args[6].(int64):])}}, true
	case "opr.replay":
		a := args[0].(*OprAttempt)
		m.replayMissing(a.missing, a.errs)
		return nil, true
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

// oprLineValue is opr.lineValue: the value of the operand line l of the rule r that matched from
// start to the position with the value v, in the frame f.
func (m *Model) oprLineValue(r int64, l *OprLine, f *Frame, v any, start int64) any {
	if m.cfg.Oprs[r].Novalue {
		return nil
	}
	pos := m.regs["pos"].(int64)
	if l.spec.Action {
		return m.oprAction(l.spec, f, start, pos, nil, nil, nil)
	}
	switch l.spec.Shape {
	case OprShared:
		v = sharedCopy(v)
	case OprWrap:
		v = &OprValue{start: start, end: pos, kids: []any{v}, rule: -1, fresh: true}
	case OprUnit:
		v = &OprValue{start: start, end: pos, rule: -1, fresh: true}
	}
	if l.spec.Scope > 0 {
		v = withCaptures(v, f, start, pos)
	}
	if n, ok := v.(*OprValue); ok && n != nil && n.fresh {
		// The value takes the rule's name, unless it is typed, and is final.
		if m.cfg.SourceInfo && !m.cfg.Typed && n.rule < 0 {
			n.rule = r
		}
		n.fresh = false
	}
	return v
}

// oprBuild is opr.build, and opr.buildTyped if typed: the value of the application of the operator of a
// to lhs, which starts at lstart if it is not a node, and rhs, and where the value starts. A typed value,
// and any value of a parser of typed values, carries no rule's name.
func (m *Model) oprBuild(r int64, a *OprAttempt, lhs any, lstart int64, rhs any, typed bool) (any, int64) {
	if m.cfg.Oprs[r].Novalue {
		return nil, a.start
	}
	start, end := a.start, m.regs["pos"].(int64)
	if n, ok := lhs.(*OprValue); ok && n != nil {
		start = n.start
	} else if lhs != nil {
		start = lstart
	}
	kind := a.op.op.Kind
	if kind == "postfix" {
		end = a.end
	}
	l := a.op.line.spec
	f := a.frame.(*Frame)
	// The operator's value: the value of its line's expression, or a fresh node of its match.
	var opv any
	switch l.Shape {
	case OprNone, OprUnit:
		opv = &OprValue{start: a.start, end: a.end, rule: -1}
	case OprShared:
		opv = sharedCopy(a.v)
	default:
		opv = a.v
	}
	if (l.Shape == OprTuple || l.Shape == OprShared) && l.Scope > 0 {
		opv = withCaptures(opv, f, a.start, a.end)
	}
	if n, ok := opv.(*OprValue); ok && n != nil {
		n.fresh = false
	}
	var left, right any
	if kind != "prefix" {
		left = lhs
	}
	if kind != "postfix" {
		right = rhs
	}
	if l.Action {
		v := m.oprAction(l, f, start, end, opv, left, right)
		if m.cfg.Oprs[r].OpType {
			v = retype(v, r)
		}
		return v, start
	}
	n := &OprValue{start: start, end: end, rule: -1}
	if m.cfg.Oprs[r].OpType {
		// The node of the operators' named node type, which carries no rule name.
		n.opType = r + 1
	} else if m.cfg.SourceInfo && !typed && !m.cfg.Typed {
		n.rule = r
	}
	if kind != "prefix" {
		n.kids = append(n.kids, lhs)
		n.setField("lhs", lhs)
	}
	n.kids = append(n.kids, opv)
	n.setField("op", opv)
	if kind != "postfix" {
		n.kids = append(n.kids, rhs)
		n.setField("rhs", rhs)
	}
	return n, start
}

// oprAction is the value of the action of a line (OprLineSpec), given its frame, the range of its
// input and its locals.
func (m *Model) oprAction(l OprLineSpec, f *Frame, start, end int64, op, lhs, rhs any) any {
	if l.NullAction {
		return nil
	}
	n := &OprValue{start: start, end: end, kids: []any{op, lhs, rhs}, rule: -1}
	for k, c := range f.vals {
		if c != nil {
			n.setField(fmt.Sprint("c", k), c)
		}
	}
	return n
}

// retype converts v, the value of an operator's action, to the named node type of the operators of the
// rule r: v itself if it has the type, or else a copy of v of the type, with its range, elements and
// fields, which carries no rule name and is not fresh. A value that is not a node does not convert:
// the parse ends with an evaluation error, which the harness sees as the interruption "error".
func retype(v any, r int64) any {
	n, ok := v.(*OprValue)
	if !ok || n == nil {
		panic(code.Interrupt{Reason: "error"})
	}
	if n.opType == r+1 {
		return n
	}
	return &OprValue{start: n.start, end: n.end, kids: slices.Clone(n.kids), fields: slices.Clone(n.fields), rule: -1, opType: r + 1}
}

// sharedCopy is the fresh node of the range and the elements of the tuple node v that a memo entry
// shares.
func sharedCopy(v any) any {
	n, ok := v.(*OprValue)
	if !ok || n == nil {
		return v
	}
	return &OprValue{start: n.start, end: n.end, kids: n.kids, rule: -1, fresh: true}
}

// withCaptures makes the captures in the slots of f fields of the value v of the range [start, end),
// by the names of the slots; none if every slot is empty. A value that is not fresh, the null value
// and one that is itself a capture are first wrapped in a fresh node of the range whose one element
// is v.
func withCaptures(v any, f *Frame, start, end int64) any {
	found := false
	n, _ := v.(*OprValue)
	wrap := n == nil || !n.fresh
	for _, c := range f.vals {
		if c != nil {
			found = true
			if c == v {
				wrap = true
			}
		}
	}
	if !found {
		return v
	}
	if wrap {
		n = &OprValue{start: start, end: end, kids: []any{v}, rule: -1, fresh: true}
	}
	for k, c := range f.vals {
		if c != nil {
			n.setField(fmt.Sprint("c", k), c)
		}
	}
	return n
}
