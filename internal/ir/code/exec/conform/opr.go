package conform

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// Operators is an Impl that provides the operations of the Operators Rule descriptors, matches and
// records. Handle returns a new implementation handle for the conformance operation name of the
// frames, backtracking points, values and records that the cases make (OprFrameOp, OprMarkOp,
// OprNodeOp and the operations of OprRecordOps), and Rule (Environment) the Operators Rule of the
// index in ref.Config.Oprs.
type Operators interface {
	Handle(name string) any
}

// The operations that conformance cases use to make a frame, a backtracking point, a value and a record
// of a split form of the Operators Rule loop. They are not operations of the parser VM.
const (
	OprFrameOp = "test.frame"
	OprMarkOp  = "test.mark"
	OprNodeOp  = "test.node"
	// OprAtomOp makes a typed value that is not a node, such as the value of an action.
	OprAtomOp = "test.atom"
)

// oprRecordOpPrefix starts the name of the operation that makes a record of the split form with the
// prefix that follows it (OprRecordOp).
const oprRecordOpPrefix = "test.oprRecord."

// OprRecordOp returns the operation that makes a record of the split form with the operations that
// start with prefix (oprLongest, oprNud or oprExpr).
func OprRecordOp(prefix string) string { return oprRecordOpPrefix + prefix }

// isOprTestOp reports whether name is an operation that makes a handle of the Operators Rule family.
func isOprTestOp(name string) bool {
	return name == OprFrameOp || name == OprMarkOp || name == OprNodeOp || name == OprAtomOp ||
		strings.HasPrefix(name, oprRecordOpPrefix)
}

// oprRecordHandle is the handle type of the record with the prefix.
var oprRecordHandle = map[string]string{"oprLongest": exec.LongestState, "oprNud": exec.NudState, "oprExpr": exec.ExprState}

// OprDescriptorOps are the operations of the descriptors of an Operators Rule and the matches of
// operators.
var OprDescriptorOps = []string{
	"opr.led", "opr.prefix", "opr.count", "opr.at", "opr.operands", "opr.operand", "opr.line", "opr.level",
	"opr.infix", "opr.postfix", "opr.right", "opr.nonAssoc", "opr.scope", "opr.hasSkip",
	"opr.attempt", "opr.found", "opr.op", "opr.end", "opr.cut", "opr.env", "opr.errs",
	"opr.setCutFailed", "opr.cutFailed", "opr.setNudBound", "opr.nudBound", "opr.setAfterEmpty", "opr.afterEmpty",
}

// OprMatchOps are the operations that run the grammar code of an Operators Rule: its trivia expression
// and the expression of a line.
var OprMatchOps = []string{"opr.skip", "opr.match"}

// OprValueBuildOps are the operations that make the values of an Operators Rule: the value of an
// operand line and that of an operator's application. OprTypedOps are those of a parser of typed
// values.
var (
	OprValueBuildOps = []string{"opr.lineValue", "opr.build"}
	OprTypedOps      = []string{"opr.buildTyped"}
)

// OprMissingOps are the operations of the matches of operators in a parse that can insert missing
// tokens. OprMissingSupportOps are those that their cases call besides, to insert and remove tokens and
// to record and drop errors; the implementation provides them besides OprOps.
var (
	OprMissingOps        = []string{"opr.attemptMissing", "opr.replay"}
	OprMissingSupportOps = []string{"missing.mark", "missing.reset", MissingInsertOp, "rec.mark", "rec.truncate", "rec.add"}
)

// OprRecordOps are the operations of the records of the split forms of the loop: for each field, the
// getter and the setter.
var OprRecordOps = func() []string {
	var names []string
	for _, rec := range []string{"oprLongest", "oprNud", "oprExpr"} {
		for _, f := range ref.OprRecords[rec] {
			names = append(names, rec+"."+f, rec+".set"+strings.ToUpper(f[:1])+f[1:])
		}
	}
	return names
}()

// OprOps are all the operations of the Operators Rule family that cases for the reference model cover.
var OprOps = slices.Concat(OprDescriptorOps, OprMatchOps, OprValueBuildOps, OprRecordOps)

// OprSupportOps are the operations that cases of the Operators Rule family call to make the errors and
// the environments they pass: the implementation provides them besides OprOps.
var OprSupportOps = []string{"rec.push", "rec.since", "env.bind", "env.scope"}

// OprFrameOps are the operations of the capture frames that the cases of OprMatchOps and
// OprValueBuildOps call: a line sets its captures in the current frame, and a case opens and uses
// frames, sets and reads their slots and undoes their writes.
var OprFrameOps = []string{"frame.open", "frame.use", "frame.current", "frame.slots", "frame.set", "frame.mark",
	"frame.markTrail", "frame.undo", ref.FrameSlotOp}

// OprSuiteOps returns OprOps, OprSupportOps, OprFrameOps and ref.OprValueOps, the operations a suite of
// the family provides.
func OprSuiteOps() []string {
	return slices.Concat(OprOps, OprSupportOps, OprFrameOps, ref.OprValueOps)
}

// OprModule returns the declarations of Module with the operations that make the handles of the
// cases of the Operators Rule family.
func OprModule() *code.Module {
	m := MissingModule()
	test := func(name string, handle string) {
		m.AddOp(&code.Op{Name: name, Component: exec.Rules, Results: []code.Type{code.THandle(handle)},
			Contract: "Returns a new handle for a conformance case."})
	}
	test(OprFrameOp, exec.Frame)
	test(OprMarkOp, exec.Backtrack)
	test(OprNodeOp, exec.Value)
	test(OprAtomOp, exec.Value)
	for rec, h := range oprRecordHandle {
		test(OprRecordOp(rec), h)
	}
	m.AddOp(&code.Op{Name: ref.FrameSlotOp, Component: exec.Frames, Reads: []string{exec.Frames},
		Params:   []code.Param{code.P("f", code.THandle(exec.Frame)), code.P("slot", code.TInt)},
		Results:  []code.Type{code.THandle(exec.Value)},
		Contract: `Returns the value in the slot of f, the null value for an empty slot.`,
		Requires: `slot is a slot of f.`})
	value := func(name, contract, requires string, res code.Type, extra ...code.Param) {
		m.AddOp(&code.Op{Name: name, Component: exec.Host, Params: append([]code.Param{code.P("v", code.THandle(exec.Value))}, extra...),
			Results: []code.Type{res}, Contract: contract, Requires: requires})
	}
	node := `v is a node.`
	value(ref.OprValueStartOp, `Returns where the node v starts.`, node, code.TInt)
	value(ref.OprValueEndOp, `Returns where the node v ends.`, node, code.TInt)
	value(ref.OprValueKidsOp, `Returns the number of the elements of the node v.`, node, code.TInt)
	value(ref.OprValueKidOp, `Returns element i of the node v.`, `v is a node and 0 <= i < test.valueKids(v).`,
		code.THandle(exec.Value), code.P("i", code.TInt))
	value(ref.OprValueFieldOp, `Returns the field of the node v of the name ref.OprFieldNames[k], the null value
if it has none.`, `v is a node and k is an index of ref.OprFieldNames.`, code.THandle(exec.Value), code.P("k", code.TInt))
	value(ref.OprValueRuleOp, `Returns the index of the rule whose name the node v carries, -1 if none.`, node, code.TInt)
	value(ref.OprValueTypeOp, `Returns the index of the rule whose operators' named node type the node v has, -1 if none.`, node, code.TInt)
	return m
}

// OprConfig returns the Operators Rules of the cases, with variables for the environment, in a parse
// that records source information: a rule of each kind of operator that skips trivia, a rule without
// prefix operators whose value nothing uses, one with no operator, and one whose operators have a named
// node type. The scripts of the trivia and of the lines of the first rule match and fail, move the
// position by different lengths, record errors, pass cuts and capture; those of the last rule match;
// the other lines have none and fail at once. The lines of the first rule have each shape of value of a
// sequence or of one element, with and without an action; the last rule has an operator with an action,
// one whose action returns the null value, which does not convert to the type, and some without an
// action, and the lines of an element that is not a sequence but makes a node of its range.
func OprConfig() ref.Config {
	op := func(kind, assoc string, level, scope int64, expr ...ref.OprExpr) ref.OprOp {
		return ref.OprOp{Kind: kind, Assoc: assoc, Level: level, Line: ref.OprLineSpec{Scope: scope, Expr: expr}}
	}
	x := func(ok bool, n int64, cut bool, errs ...int64) ref.OprExpr {
		return ref.OprExpr{Eval: ref.Eval{OK: ok, Len: n, Errs: errs}, Cut: cut}
	}
	// captures is an evaluation that sets the captures of the slots.
	captures := func(ok bool, n int64, slots ...int64) ref.OprExpr {
		return ref.OprExpr{Eval: ref.Eval{OK: ok, Len: n, Captures: slots}}
	}
	// shaped gives an operator's line the shape of its value, and an action.
	shaped := func(o ref.OprOp, shape ref.OprShape, action bool) ref.OprOp {
		o.Line.Shape, o.Line.Action = shape, action
		return o
	}
	// null gives an operator's line an action that returns the null value.
	null := func(o ref.OprOp) ref.OprOp {
		o.Line.Action, o.Line.NullAction = true, true
		return o
	}
	return ref.Config{
		Vars:       []int64{1, 2, 3},
		SourceInfo: true,
		Oprs: []ref.OprRule{
			{Prefix: []ref.OprOp{shaped(op("prefix", "left", 9, 1, x(true, 2, false)), ref.OprTuple, false),
				shaped(op("prefix", "left", 8, 0), ref.OprNone, true)},
				Led: []ref.OprOp{op("infix", "left", 1, 0, x(true, 1, false), x(false, 2, true), x(true, 3, true, 6), x(false, 0, false)),
					shaped(op("infix", "left", 2, 2, captures(true, 1, 0, 1), captures(false, 2, 1), captures(true, 1)), ref.OprTuple, false),
					shaped(op("infix", "right", 3, 1, x(true, 0, true)), ref.OprShared, true),
					shaped(op("postfix", "left", 4, 0, x(false, 1, false, 7), x(true, 4, false)), ref.OprNone, false),
					op("infix", "none", 5, 3)},
				Operands: []ref.OprLineSpec{{Scope: 0, Expr: []ref.OprExpr{x(true, 3, false), x(false, 1, true)}},
					{Scope: 2, Expr: []ref.OprExpr{captures(true, 2, 1)}, Shape: ref.OprTuple},
					{Scope: 1, Expr: []ref.OprExpr{captures(true, 1, 0)}, Shape: ref.OprShared},
					{Scope: 1, Expr: []ref.OprExpr{captures(true, 1, 0)}, Action: true}},
				Skip:   true,
				Trivia: []ref.Eval{{OK: true, Len: 2}, {OK: false, Len: 1}, {OK: true}, {OK: true, Len: 1, Errs: []int64{9}}}},
			{Novalue: true, Led: []ref.OprOp{op("infix", "right", 1, 0), op("postfix", "none", 2, 1)},
				Operands: []ref.OprLineSpec{{Scope: 1}}},
			{},
			{OpType: true, Prefix: []ref.OprOp{shaped(op("prefix", "left", 4, 0, x(true, 1, false)), ref.OprTuple, false),
				shaped(op("prefix", "left", 5, 0, x(true, 2, false)), ref.OprUnit, false)},
				Led: []ref.OprOp{shaped(op("infix", "left", 1, 1, captures(true, 1, 0)), ref.OprTuple, true),
					op("infix", "left", 2, 0, x(true, 2, false)),
					shaped(op("postfix", "left", 3, 0, x(true, 1, false)), ref.OprNone, false),
					shaped(op("infix", "left", 4, 1, captures(true, 1, 0)), ref.OprWrap, false),
					null(op("infix", "left", 5, 0, x(true, 1, false)))},
				Operands: []ref.OprLineSpec{{Scope: 0, Expr: []ref.OprExpr{x(true, 2, false)}},
					{Scope: 1, Expr: []ref.OprExpr{captures(true, 1, 0)}, Shape: ref.OprWrap},
					{Scope: 0, Expr: []ref.OprExpr{x(true, 3, false)}, Shape: ref.OprUnit}}},
		},
	}
}

// OprMissingConfig returns OprConfig in a parse in the missing-token mode.
func OprMissingConfig() ref.Config {
	cfg := OprConfig()
	cfg.Missing = true
	return cfg
}

// OprCases returns the handwritten conformance cases of the Operators Rule family, in one module.
func OprCases() []Case {
	m := OprModule()
	var cs []Case
	cfg := OprConfig()
	add := func(name string, body func(b *code.Builder)) {
		cs = append(cs, Case{Module: m, Func: m.AddFunc(name, nil, nil, []string{"budget", "stream", "document"}, body), Config: cfg})
	}
	i := code.Int
	rule := func(n int64) code.Expr { return code.C(RuleOp, i(n)) }
	eq := code.Eq
	nullAttempt := code.Null(code.THandle(exec.Attempt))

	add("opr_descriptors", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		led, prefix := b.Let("led", code.C("opr.led", r)), b.Let("prefix", code.C("opr.prefix", r))
		b.Assert(eq(code.C("opr.count", led), i(5)), "the rule has five infix and postfix operators")
		b.Assert(eq(code.C("opr.count", prefix), i(2)), "and two prefix operators")
		b.Let("again", code.C("opr.led", r))
		for k, want := range []struct {
			level          int64
			infix, postfix bool
			right, none    bool
			scope          int64
		}{{1, true, false, false, false, 0}, {2, true, false, false, false, 2}, {3, true, false, true, false, 1},
			{4, false, true, false, false, 0}, {5, true, false, false, true, 3}} {
			o := b.Let(fmt.Sprint("o", k), code.C("opr.at", led, i(int64(k))))
			b.Assert(eq(code.C("opr.level", o), i(want.level)), "the binding power of an operator")
			b.Assert(eq(code.C("opr.infix", o), code.BoolC(want.infix)), "infix")
			b.Assert(eq(code.C("opr.postfix", o), code.BoolC(want.postfix)), "postfix")
			b.Assert(eq(code.C("opr.right", o), code.BoolC(want.right)), "right")
			b.Assert(eq(code.C("opr.nonAssoc", o), code.BoolC(want.none)), "non-associative")
			l := b.Let(fmt.Sprint("l", k), code.C("opr.line", o))
			b.Assert(eq(code.C("opr.scope", l), i(want.scope)), "the capture slots of an operator's line")
		}
		p0 := b.Let("p0", code.C("opr.at", prefix, i(0)))
		b.Assert(eq(code.C("opr.level", p0), i(9)), "a prefix operator's binding power")
		b.Assert(code.Not(code.C("opr.infix", p0)), "a prefix operator is not infix")
		b.Assert(eq(code.C("opr.operands", r), i(4)), "the operand lines of the rule")
		ol := b.Let("ol", code.C("opr.operand", r, i(1)))
		b.Assert(eq(code.C("opr.scope", ol), i(2)), "the capture slots of an operand line")
		b.Assert(code.C("opr.hasSkip", r), "the rule skips trivia")
	})
	add("opr_descriptors_other_rules", func(b *code.Builder) {
		r1, r2 := b.Let("r1", rule(1)), b.Let("r2", rule(2))
		b.Assert(eq(code.C("opr.count", code.C("opr.led", r1)), i(2)), "the second rule's operators")
		b.Assert(eq(code.C("opr.count", code.C("opr.prefix", r1)), i(0)), "has no prefix operator")
		b.Assert(code.Not(code.C("opr.hasSkip", r1)), "and skips nothing")
		b.Assert(eq(code.C("opr.operands", r1), i(1)), "with one operand line")
		b.Assert(eq(code.C("opr.count", code.C("opr.led", r2)), i(0)), "a rule with no operator")
		b.Assert(eq(code.C("opr.operands", r2), i(0)), "has no operand line")
		o := b.Let("o", code.C("opr.at", code.C("opr.led", r1), i(1)))
		b.Assert(code.And(code.C("opr.postfix", o), code.C("opr.nonAssoc", o)), "a postfix operator that does not associate")
	})
	add("opr_attempt", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		o := b.Let("o", code.C("opr.at", code.C("opr.led", r), i(2)))
		f := b.Let("f", code.C(OprFrameOp))
		v := b.Let("v", code.C(OprNodeOp))
		b.Do("rec.push", code.C(ErrOp, i(4)))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		b.Do("env.bind", i(1), code.C(DatumOp, i(7)))
		b.SetReg(exec.Pos, i(11))
		a := b.Let("a", code.C("opr.attempt", o, f, v, i(5), code.BoolC(true), errs))
		b.Do("env.bind", i(1), code.C(DatumOp, i(8)))
		b.SetReg(exec.Pos, i(12))
		a2 := b.Let("a2", code.C("opr.attempt", o, f, v, i(5), code.BoolC(false), errs))
		b.Assert(code.C("opr.found", a), "a match is found")
		b.Assert(code.Not(code.C("opr.found", nullAttempt)), "the null attempt is not")
		b.Assert(eq(code.C("opr.end", a), i(11)), "a match ends at the position it was made at")
		b.Assert(eq(code.C("opr.end", a2), i(12)), "and the next one at the next position")
		b.Assert(code.C("opr.cut", a), "a cut is kept")
		b.Assert(code.Not(code.C("opr.cut", a2)), "and so is no cut")
		b.Let("o2", code.C("opr.op", a))
		b.Let("e1", code.C("opr.env", a))
		b.Let("e2", code.C("opr.env", a2))
		b.Let("er", code.C("opr.errs", a))
	})
	add("opr_skip", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		b.SetReg(exec.Silent, i(1))
		b.SetReg(exec.Cut, code.BoolC(true))
		b.SetReg(exec.Pos, i(3))
		b.Assert(code.C("opr.skip", r), "the trivia matches")
		b.Assert(eq(code.R(exec.Pos), i(5)), "and the position is after it")
		b.SetReg(exec.Pos, i(6))
		b.Assert(code.Not(code.C("opr.skip", r)), "the next evaluation of the trivia fails")
		b.SetReg(exec.Pos, i(6))
		b.Assert(code.C("opr.skip", r), "empty trivia matches")
		b.Assert(eq(code.R(exec.Pos), i(6)), "and leaves the position")
		b.Assert(code.C("opr.skip", r), "trivia that records an error")
		b.Assert(eq(code.R(exec.Pos), i(7)), "and moves the position")
		b.Assert(code.C("opr.skip", r), "the last evaluation of the script repeats")
		b.Assert(eq(code.R(exec.Pos), i(8)), "and moves the position again")
		b.Let("errs", code.C("rec.since", i(0)))
		b.Assert(code.R(exec.Cut), "the trivia leaves the cut register")
		b.Assert(eq(code.R(exec.Silent), i(1)), "and the silent depth")
	})
	add("opr_match", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		led := b.Let("led", code.C("opr.led", r))
		l0 := b.Let("l0", code.C("opr.line", code.C("opr.at", led, i(0))))
		l2 := b.Let("l2", code.C("opr.line", code.C("opr.at", led, i(2))))
		l3 := b.Let("l3", code.C("opr.line", code.C("opr.at", led, i(3))))
		b.SetReg(exec.Silent, i(0))
		b.SetReg(exec.Cut, code.BoolC(false))
		b.SetReg(exec.Pos, i(4))
		ok := b.LetN([]string{"v1", "ok1"}, code.C("opr.match", l0))[1]
		b.Assert(ok, "the line matches")
		b.Assert(eq(code.R(exec.Pos), i(5)), "and the position is after it")
		b.Assert(code.Not(code.R(exec.Cut)), "it passes no cut")
		b.SetReg(exec.Pos, i(2))
		ok = b.LetN([]string{"v2", "ok2"}, code.C("opr.match", l2))[1]
		b.Assert(ok, "another line has a script of its own")
		b.Assert(eq(code.R(exec.Pos), i(2)), "an empty match leaves the position")
		b.Assert(code.R(exec.Cut), "a cut sets the cut register")
		b.SetReg(exec.Cut, code.BoolC(false))
		b.SetReg(exec.Pos, i(7))
		ok = b.LetN([]string{"v3", "ok3"}, code.C("opr.match", l0))[1]
		b.Assert(code.Not(ok), "the second evaluation of the first line fails")
		b.Assert(code.R(exec.Cut), "and a failure sets the cut register too")
		b.SetReg(exec.Cut, code.BoolC(false))
		b.SetReg(exec.Pos, i(7))
		ok = b.LetN([]string{"v4", "ok4"}, code.C("opr.match", l0))[1]
		b.Assert(ok, "the third evaluation matches")
		b.Assert(eq(code.R(exec.Pos), i(10)), "three units long")
		b.Let("errs", code.C("rec.since", i(0)))
		b.SetReg(exec.Cut, code.BoolC(false))
		b.SetReg(exec.Pos, i(1))
		ok = b.LetN([]string{"v5", "ok5"}, code.C("opr.match", l3))[1]
		b.Assert(code.Not(ok), "a failure that records an error")
		b.SetReg(exec.Pos, i(1))
		ok = b.LetN([]string{"v6", "ok6"}, code.C("opr.match", l3))[1]
		b.Assert(ok, "and then a match")
		b.Assert(eq(code.R(exec.Pos), i(5)), "four units long")
		b.Assert(code.Not(code.R(exec.Cut)), "without a cut, the cut register stays")
		b.Let("errs2", code.C("rec.since", i(0)))
		b.Assert(eq(code.R(exec.Silent), i(0)), "the silent depth stays")
		ol := b.Let("ol", code.C("opr.operand", r, i(0)))
		b.SetReg(exec.Pos, i(3))
		b.Assert(b.LetN([]string{"v7", "ok7"}, code.C("opr.match", ol))[1], "an operand line matches")
		b.Assert(eq(code.R(exec.Pos), i(6)), "with its own script")
		b.SetReg(exec.Pos, i(3))
		b.Assert(code.Not(b.LetN([]string{"v9", "ok9"}, code.C("opr.match", ol))[1]), "and then fails")
		b.Assert(code.R(exec.Cut), "after a cut")
		b.SetReg(exec.Pos, i(3))
		ol1 := code.C("opr.operand", rule(1), i(0))
		b.Assert(code.Not(b.LetN([]string{"v10", "ok10"}, code.C("opr.match", ol1))[1]), "a line without a script fails")
		o2 := b.Let("o2", code.C("opr.at", led, i(2)))
		b.SetReg(exec.Pos, i(8))
		v := b.LetN([]string{"v8", "ok8"}, code.C("opr.match", l2))[0]
		a := b.Let("a", code.C("opr.attempt", o2, code.C(OprFrameOp), v, i(8), code.R(exec.Cut), code.C("rec.since", i(0))))
		b.Assert(eq(code.C("opr.end", a), i(8)), "a match of an empty line ends where it started")
		b.Assert(code.C("opr.cut", a), "with its cut")
	})
	add("opr_match_captures", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		l1 := b.Let("l1", code.C("opr.line", code.C("opr.at", code.C("opr.led", r), i(1))))
		ol := b.Let("ol", code.C("opr.operand", r, i(1)))
		f := b.Let("f", code.C("frame.open", code.C("opr.scope", l1)))
		b.Do("frame.use", f)
		b.SetReg(exec.Pos, i(2))
		m := b.Let("m", code.C("frame.mark"))
		b.Assert(b.LetN([]string{"v1", "ok1"}, code.C("opr.match", l1))[1], "a line that captures matches")
		c0 := b.Let("c0", code.C(ref.FrameSlotOp, f, i(0)))
		c1 := b.Let("c1", code.C(ref.FrameSlotOp, f, i(1)))
		b.Assert(code.Not(eq(c0, c1)), "each capture is a value of its own")
		b.Assert(code.Not(b.LetN([]string{"v2", "ok2"}, code.C("opr.match", l1))[1]), "a failure that captures")
		b.Assert(code.Not(eq(code.C(ref.FrameSlotOp, f, i(1)), c1)), "sets the slot again")
		b.Assert(eq(code.C(ref.FrameSlotOp, f, i(0)), c0), "and leaves the other")
		b.Do("frame.undo", code.C("frame.markTrail", m))
		b.Let("e0", code.C(ref.FrameSlotOp, f, i(0)))
		b.Let("e1", code.C(ref.FrameSlotOp, f, i(1)))
		b.SetReg(exec.Pos, i(2))
		b.Assert(b.LetN([]string{"v3", "ok3"}, code.C("opr.match", l1))[1], "a match that captures nothing")
		e2 := b.Let("e2", code.C(ref.FrameSlotOp, f, i(1)))
		g := b.Let("g", code.C("frame.open", code.C("opr.scope", ol)))
		b.Do("frame.use", g)
		b.SetReg(exec.Pos, i(4))
		b.Assert(b.LetN([]string{"v4", "ok4"}, code.C("opr.match", ol))[1], "an operand line captures")
		b.Assert(eq(code.R(exec.Pos), i(6)), "and moves the position")
		b.Let("d1", code.C(ref.FrameSlotOp, g, i(1)))
		b.Assert(eq(code.C(ref.FrameSlotOp, f, i(1)), e2), "into the current frame only")
	})
	nullValue := code.Null(code.THandle(exec.Value))
	all := func(xs ...code.Expr) code.Expr {
		x := xs[0]
		for _, y := range xs[1:] {
			x = code.And(x, y)
		}
		return x
	}
	field := func(v code.Expr, name string) code.Expr {
		return code.C(ref.OprValueFieldOp, v, i(int64(slices.Index(ref.OprFieldNames, name))))
	}
	start := func(v code.Expr) code.Expr { return code.C(ref.OprValueStartOp, v) }
	end := func(v code.Expr) code.Expr { return code.C(ref.OprValueEndOp, v) }
	kids := func(v code.Expr) code.Expr { return code.C(ref.OprValueKidsOp, v) }
	kid := func(v code.Expr, k int64) code.Expr { return code.C(ref.OprValueKidOp, v, i(k)) }
	ruleOf := func(v code.Expr) code.Expr { return code.C(ref.OprValueRuleOp, v) }
	// frame opens a frame of n slots and makes it the current one.
	frame := func(b *code.Builder, name string, n int64) code.Expr {
		f := b.Let(name, code.C("frame.open", i(n)))
		b.Do("frame.use", f)
		return f
	}
	// match runs the expression of the line l from the position at, and returns its value.
	match := func(b *code.Builder, name string, l code.Expr, at int64) code.Expr {
		b.SetReg(exec.Pos, i(at))
		vs := b.LetN([]string{name, name + "ok"}, code.C("opr.match", l))
		b.Assert(vs[1], "the line matches")
		return vs[0]
	}
	add("opr_line_value", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		ol := func(k int64) code.Expr { return b.Let(fmt.Sprint("ol", k), code.C("opr.operand", r, i(k))) }
		ol0, ol1, ol2, ol3 := ol(0), ol(1), ol(2), ol(3)
		f0 := frame(b, "f0", 0)
		v0 := match(b, "v0", ol0, 3)
		lv0 := b.Let("lv0", code.C("opr.lineValue", r, ol0, f0, v0, i(3)))
		b.Assert(eq(lv0, v0), "the value of a line of one element is its expression's")
		b.Assert(eq(ruleOf(lv0), i(0)), "and takes the rule's name")
		b.Assert(all(eq(start(lv0), i(3)), eq(end(lv0), i(6))), "with the range of the match")
		f2 := frame(b, "f2", 2)
		v1 := match(b, "v1", ol1, 1)
		c1 := b.Let("c1", code.C(ref.FrameSlotOp, f2, i(1)))
		lv1 := b.Let("lv1", code.C("opr.lineValue", r, ol1, f2, v1, i(1)))
		b.Assert(eq(lv1, v1), "the node of a sequence's tuple is the line's value")
		b.Assert(eq(field(lv1, "c1"), c1), "and takes the capture as a field")
		b.Assert(eq(field(lv1, "c0"), nullValue), "but not an empty slot")
		b.Assert(eq(kids(lv1), i(0)), "and keeps its elements")
		b.SetReg(exec.Pos, i(5))
		lv1b := b.Let("lv1b", code.C("opr.lineValue", r, ol1, f2, lv1, i(2)))
		b.Assert(code.Not(eq(lv1b, lv1)), "a value that a rule has taken is wrapped")
		b.Assert(all(eq(kids(lv1b), i(1)), eq(kid(lv1b, 0), lv1)), "in a node whose one element it is")
		b.Assert(all(eq(start(lv1b), i(2)), eq(end(lv1b), i(5))), "of the range of the line")
		b.Assert(all(eq(field(lv1b, "c1"), c1), eq(ruleOf(lv1b), i(0))), "which takes the captures and the name")
		b.Do("frame.set", i(0), lv1b)
		lv1c := b.Let("lv1c", code.C("opr.lineValue", r, ol1, f2, lv1b, i(2)))
		b.Assert(code.Not(eq(lv1c, lv1b)), "and so is a value that is itself a capture")
		b.Assert(eq(field(lv1c, "c0"), lv1b), "which is a field of the node that wraps it")
		f1 := frame(b, "f1", 1)
		v2 := match(b, "v2", ol2, 4)
		lv2 := b.Let("lv2", code.C("opr.lineValue", r, ol2, f1, v2, i(4)))
		b.Assert(code.Not(eq(lv2, v2)), "a tuple node that a memo entry shares is copied")
		b.Assert(all(eq(start(lv2), i(4)), eq(end(lv2), i(5))), "with its range")
		b.Assert(all(eq(field(lv2, "c0"), code.C(ref.FrameSlotOp, f1, i(0))), eq(field(v2, "c0"), nullValue)),
			"and the copy takes the captures")
		g1 := frame(b, "g1", 1)
		v3 := match(b, "v3", ol3, 7)
		lv3 := b.Let("lv3", code.C("opr.lineValue", r, ol3, g1, v3, i(6)))
		b.Assert(all(eq(start(lv3), i(6)), eq(end(lv3), i(8))), "an action reads the range of the line")
		b.Assert(eq(field(lv3, "c0"), code.C(ref.FrameSlotOp, g1, i(0))), "and the captures of its frame")
		b.Assert(all(eq(kids(lv3), i(3)), eq(kid(lv3, 0), nullValue), eq(kid(lv3, 1), nullValue), eq(kid(lv3, 2), nullValue)),
			"and has no operator or operand")
		b.Assert(eq(ruleOf(lv3), i(-1)), "and its value takes no name")
		r1 := b.Let("r1", rule(1))
		h1 := b.Let("h1", code.C("frame.open", i(1)))
		b.Assert(eq(code.C("opr.lineValue", r1, code.C("opr.operand", r1, i(0)), h1, code.C(OprNodeOp), i(0)), nullValue),
			"a line of a rule whose value nothing uses has none")
	})
	add("opr_build", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		led, prefix := b.Let("led", code.C("opr.led", r)), b.Let("prefix", code.C("opr.prefix", r))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		attempt := func(name string, o, f, v code.Expr, from, to int64) code.Expr {
			b.SetReg(exec.Pos, i(to))
			return b.Let(name, code.C("opr.attempt", o, f, v, i(from), code.BoolC(false), errs))
		}
		f0 := frame(b, "f0", 0)
		lhs := match(b, "lhs", code.C("opr.operand", r, i(0)), 2)
		o1 := b.Let("o1", code.C("opr.at", led, i(1)))
		f2 := frame(b, "f2", 2)
		v1 := match(b, "v1", code.C("opr.line", o1), 5)
		a1 := attempt("a1", o1, f2, v1, 5, 6)
		rhs := b.Let("rhs", code.C(OprNodeOp))
		b.SetReg(exec.Pos, i(9))
		n1 := b.Let("n1", code.C("opr.build", r, a1, lhs, rhs))
		b.Assert(all(eq(start(n1), i(2)), eq(end(n1), i(9))), "an infix application runs from its left operand to the position")
		b.Assert(all(eq(kids(n1), i(3)), eq(kid(n1, 0), lhs), eq(kid(n1, 1), v1), eq(kid(n1, 2), rhs)),
			"its elements are the operands and the operator's tuple node")
		b.Assert(all(eq(field(n1, "lhs"), lhs), eq(field(n1, "op"), v1), eq(field(n1, "rhs"), rhs)), "which are its fields")
		b.Assert(all(eq(field(v1, "c0"), code.C(ref.FrameSlotOp, f2, i(0))), eq(field(v1, "c1"), code.C(ref.FrameSlotOp, f2, i(1)))),
			"the operator's tuple node takes its captures")
		b.Assert(eq(ruleOf(n1), i(0)), "and the application takes the rule's name")
		o3 := b.Let("o3", code.C("opr.at", led, i(3)))
		a3 := attempt("a3", o3, f0, code.C(OprNodeOp), 5, 7)
		b.SetReg(exec.Pos, i(10))
		n3 := b.Let("n3", code.C("opr.build", r, a3, n1, nullValue))
		b.Assert(all(eq(start(n3), i(2)), eq(end(n3), i(7))), "a postfix application ends with its operator")
		b.Assert(all(eq(kids(n3), i(2)), eq(kid(n3, 0), n1), eq(field(n3, "rhs"), nullValue)), "and has no right operand")
		op3 := b.Let("op3", kid(n3, 1))
		b.Assert(all(eq(start(op3), i(5)), eq(end(op3), i(7)), eq(kids(op3), i(0))),
			"an operator without a value is a node of its match")
		p0 := b.Let("p0", code.C("opr.at", prefix, i(0)))
		f1 := frame(b, "f1", 1)
		v0 := match(b, "v0", code.C("opr.line", p0), 3)
		a0 := attempt("a0", p0, f1, v0, 3, 5)
		b.SetReg(exec.Pos, i(8))
		n0 := b.Let("n0", code.C("opr.build", r, a0, nullValue, rhs))
		b.Assert(all(eq(start(n0), i(3)), eq(end(n0), i(8))), "a prefix application starts with its operator")
		b.Assert(all(eq(kids(n0), i(2)), eq(kid(n0, 0), v0), eq(kid(n0, 1), rhs), eq(field(n0, "lhs"), nullValue)),
			"and has no left operand")
		b.Assert(eq(field(v0, "c0"), nullValue), "an operator that captured nothing takes no field")
		p1 := b.Let("p1", code.C("opr.at", prefix, i(1)))
		a4 := attempt("a4", p1, f0, code.C(OprNodeOp), 2, 4)
		b.SetReg(exec.Pos, i(6))
		n4 := b.Let("n4", code.C("opr.build", r, a4, nullValue, rhs))
		b.Assert(all(eq(start(n4), i(2)), eq(end(n4), i(6))), "an action reads the range of the application")
		b.Assert(all(eq(kids(n4), i(3)), eq(kid(n4, 1), nullValue), eq(kid(n4, 2), rhs)), "and the operands it has")
		op4 := b.Let("op4", kid(n4, 0))
		b.Assert(all(eq(start(op4), i(2)), eq(end(op4), i(4))), "and the operator's value")
		b.Assert(eq(ruleOf(n4), i(-1)), "and its value takes no name")
		o2 := b.Let("o2", code.C("opr.at", led, i(2)))
		g1 := frame(b, "g1", 1)
		capture := b.Let("capture", code.C(OprNodeOp))
		b.Do("frame.set", i(0), capture)
		a2 := attempt("a2", o2, g1, n1, 4, 5)
		b.SetReg(exec.Pos, i(11))
		n2 := b.Let("n2", code.C("opr.build", r, a2, lhs, n3))
		op2 := b.Let("op2", kid(n2, 0))
		b.Assert(code.Not(eq(op2, n1)), "a shared tuple node of an operator is copied")
		b.Assert(all(eq(start(op2), i(2)), eq(end(op2), i(9)), eq(kids(op2), i(3)), eq(kid(op2, 0), lhs)),
			"with its range and its elements")
		b.Assert(all(eq(field(op2, "c0"), capture), eq(field(n2, "c0"), capture)), "the copy and the action see the capture")
		b.Assert(all(eq(kid(n2, 1), lhs), eq(kid(n2, 2), n3), eq(start(n2), i(2)), eq(end(n2), i(11))),
			"and the action the operands")
		o0 := b.Let("o0", code.C("opr.at", led, i(0)))
		a5 := attempt("a5", o0, f0, rhs, 9, 10)
		n5 := b.Let("n5", code.C("opr.build", r, a5, rhs, n4))
		b.Assert(all(eq(kid(n5, 1), rhs), eq(start(n5), i(0))), "the operator of one element is its value")
		r1 := b.Let("r1", rule(1))
		a6 := attempt("a6", code.C("opr.at", code.C("opr.led", r1), i(0)), f0, rhs, 3, 4)
		b.Assert(eq(code.C("opr.build", r1, a6, lhs, rhs), nullValue), "a rule whose value nothing uses applies to none")
	})
	addTyped := func(name string, body func(b *code.Builder)) {
		cs = append(cs, Case{Module: m, Func: m.AddFunc(name, nil, nil, []string{"budget", "stream", "document", exec.Typed}, body), Config: cfg})
	}
	addTyped("opr_build_typed", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		led := b.Let("led", code.C("opr.led", r))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		f0 := frame(b, "f0", 0)
		o0 := b.Let("o0", code.C("opr.at", led, i(0)))
		b.SetReg(exec.Pos, i(5))
		a := b.Let("a", code.C("opr.attempt", o0, f0, code.C(OprNodeOp), i(4), code.BoolC(false), errs))
		atom, rhs := b.Let("atom", code.C(OprAtomOp)), b.Let("rhs", code.C(OprNodeOp))
		b.SetReg(exec.Pos, i(9))
		vs := b.LetN([]string{"n", "nstart"}, code.C("opr.buildTyped", r, a, atom, i(2), rhs))
		b.Assert(eq(vs[1], i(2)), "a left operand that is not a node starts where the caller says")
		b.Assert(all(eq(start(vs[0]), i(2)), eq(end(vs[0]), i(9)), eq(kid(vs[0], 0), atom)), "and so does the application")
		b.Assert(eq(ruleOf(vs[0]), i(-1)), "which takes no name")
		lhs := match(b, "lhs", code.C("opr.operand", r, i(0)), 1)
		vs = b.LetN([]string{"m", "mstart"}, code.C("opr.buildTyped", r, a, lhs, i(7), rhs))
		b.Assert(eq(vs[1], i(1)), "a node starts where it starts")
		vs = b.LetN([]string{"k", "kstart"}, code.C("opr.buildTyped", r, a, nullValue, i(7), rhs))
		b.Assert(eq(vs[1], i(4)), "and an application without a left operand with its operator")
		r1 := b.Let("r1", rule(1))
		a1 := b.Let("a1", code.C("opr.attempt", code.C("opr.at", code.C("opr.led", r1), i(0)), f0, rhs, i(3), code.BoolC(false), errs))
		vs = b.LetN([]string{"z", "zstart"}, code.C("opr.buildTyped", r1, a1, lhs, i(0), rhs))
		b.Assert(all(eq(vs[0], nullValue), eq(vs[1], i(3))), "no value starts with the operator")
	})
	add("opr_values_without_source_info", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		ol0 := b.Let("ol0", code.C("opr.operand", r, i(0)))
		f0 := frame(b, "f0", 0)
		v0 := match(b, "v0", ol0, 3)
		b.Assert(eq(ruleOf(code.C("opr.lineValue", r, ol0, f0, v0, i(3))), i(-1)), "a line's value takes no name")
		o0 := b.Let("o0", code.C("opr.at", code.C("opr.led", r), i(0)))
		a := b.Let("a", code.C("opr.attempt", o0, f0, v0, i(6), code.BoolC(false), code.C("rec.since", i(0))))
		b.Assert(eq(ruleOf(code.C("opr.build", r, a, v0, v0)), i(-1)), "and nor does an application")
	})
	cs[len(cs)-1].Config.SourceInfo = false
	typeOf := func(v code.Expr) code.Expr { return code.C(ref.OprValueTypeOp, v) }
	add("opr_values_of_elements", func(b *code.Builder) {
		// The lines of one element that is not a sequence but whose value is a node of its range, and
		// of no valued element.
		r := b.Let("r", rule(3))
		f1 := frame(b, "f1", 1)
		ol1 := b.Let("ol1", code.C("opr.operand", r, i(1)))
		v1 := match(b, "v1", ol1, 2)
		w := b.Let("w", code.C("opr.lineValue", r, ol1, f1, v1, i(2)))
		b.Assert(all(eq(start(w), i(2)), eq(end(w), i(3)), eq(kids(w), i(1)), eq(kid(w, 0), v1)),
			"an element with captures is wrapped in a node of the line's range")
		b.Assert(all(eq(code.C(ref.OprValueFieldOp, w, i(3)), code.C(ref.FrameSlotOp, f1, i(0))), eq(ruleOf(w), i(3))),
			"which takes the captures and the rule's name")
		f0 := frame(b, "f0", 0)
		ol2 := b.Let("ol2", code.C("opr.operand", r, i(2)))
		v2 := match(b, "v2", ol2, 4)
		u := b.Let("u", code.C("opr.lineValue", r, ol2, f0, v2, i(4)))
		b.Assert(all(eq(start(u), i(4)), eq(end(u), i(7)), eq(kids(u), i(0)), eq(ruleOf(u), i(3))),
			"no valued element makes an empty node of the line's range")
		led, prefix := b.Let("led", code.C("opr.led", r)), b.Let("prefix", code.C("opr.prefix", r))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		b.Do("frame.use", f1)
		o3 := b.Let("o3", code.C("opr.at", led, i(3)))
		ov := match(b, "ov", code.C("opr.line", o3), 7)
		a3 := b.Let("a3", code.C("opr.attempt", o3, f1, ov, i(7), code.BoolC(false), errs))
		b.SetReg(exec.Pos, i(9))
		n3 := b.Let("n3", code.C("opr.build", r, a3, w, u))
		b.Assert(all(eq(kid(n3, 1), ov), eq(start(n3), i(2)), eq(end(n3), i(9))), "an operator of such an element is the element")
		b.Do("frame.use", f0)
		p1 := b.Let("p1", code.C("opr.at", prefix, i(1)))
		pv := match(b, "pv", code.C("opr.line", p1), 0)
		ap := b.Let("ap", code.C("opr.attempt", p1, f0, pv, i(0), code.BoolC(false), errs))
		b.SetReg(exec.Pos, i(5))
		np := b.Let("np", code.C("opr.build", r, ap, nullValue, u))
		pop := b.Let("pop", kid(np, 0))
		b.Assert(all(eq(start(pop), i(0)), eq(end(pop), i(2)), eq(kids(pop), i(0))),
			"and one of no valued element is a node of the range of its match")
	})
	add("opr_build_op_type", func(b *code.Builder) {
		r := b.Let("r", rule(3))
		led := b.Let("led", code.C("opr.led", r))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		f0 := frame(b, "f0", 0)
		lhs := match(b, "lhs", code.C("opr.operand", r, i(0)), 1)
		rhs := match(b, "rhs", code.C("opr.operand", r, i(0)), 4)
		attempt := func(name string, o, f code.Expr, from, to int64) code.Expr {
			b.SetReg(exec.Pos, i(from))
			vs := b.LetN([]string{name + "v", name + "ok"}, code.C("opr.match", code.C("opr.line", o)))
			b.Assert(vs[1], "the operator's line matches")
			b.SetReg(exec.Pos, i(to))
			return b.Let(name, code.C("opr.attempt", o, f, vs[0], i(from), code.BoolC(false), errs))
		}
		a1 := attempt("a1", b.Let("o1", code.C("opr.at", led, i(1))), f0, 2, 4)
		n1 := b.Let("n1", code.C("opr.build", r, a1, lhs, rhs))
		b.Assert(all(eq(typeOf(n1), i(3)), eq(ruleOf(n1), i(-1))), "an application has the operators' named node type, without the rule's name")
		b.Assert(all(eq(start(n1), i(1)), eq(kids(n1), i(3)), eq(kid(n1, 0), lhs), eq(kid(n1, 2), rhs)), "with the range and the elements of the tuple")
		b.Assert(eq(typeOf(kid(n1, 1)), i(-1)), "the operator keeps its own type")
		f1 := frame(b, "f1", 1)
		a0 := attempt("a0", b.Let("o0", code.C("opr.at", led, i(0))), f1, 2, 3)
		n0 := b.Let("n0", code.C("opr.build", r, a0, lhs, rhs))
		b.Assert(all(eq(typeOf(n0), i(3)), eq(ruleOf(n0), i(-1))), "an action's value is converted to the type")
		b.Assert(all(eq(start(n0), i(1)), eq(end(n0), i(3)), eq(kids(n0), i(3)), eq(kid(n0, 1), lhs), eq(kid(n0, 2), rhs)),
			"keeping its range and elements")
		b.Let("n0op", kid(n0, 0))
		b.Let("n0c0", code.C(ref.OprValueFieldOp, n0, i(3)))
		b.Do("frame.use", f0)
		p0 := b.Let("p0", code.C("opr.at", code.C("opr.prefix", r), i(0)))
		ap := attempt("ap", p0, f0, 0, 1)
		b.SetReg(exec.Pos, i(5))
		np := b.Let("np", code.C("opr.build", r, ap, nullValue, rhs))
		b.Assert(all(eq(typeOf(np), i(3)), eq(kids(np), i(2))), "and so has a prefix application")
		other := b.Let("other", code.C("opr.build", rule(0), attempt("a00", code.C("opr.at", code.C("opr.led", rule(0)), i(0)), f0, 0, 1), lhs, rhs))
		b.Assert(eq(typeOf(other), i(-1)), "the applications of a rule without the type have none")
		b.Assert(eq(typeOf(lhs), i(-1)), "nor do its operands")
	})
	add("opr_build_not_convertible", func(b *code.Builder) {
		r := b.Let("r", rule(3))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		f0 := frame(b, "f0", 0)
		lhs := match(b, "lhs", code.C("opr.operand", r, i(0)), 1)
		rhs := match(b, "rhs", code.C("opr.operand", r, i(0)), 4)
		o := b.Let("o", code.C("opr.at", code.C("opr.led", r), i(4)))
		b.SetReg(exec.Pos, i(2))
		vs := b.LetN([]string{"ov", "ok"}, code.C("opr.match", code.C("opr.line", o)))
		b.Assert(vs[1], "the operator's line matches")
		a := b.Let("a", code.C("opr.attempt", o, f0, vs[0], i(2), code.BoolC(false), errs))
		b.Let("n", code.C("opr.build", r, a, lhs, rhs)) // ends the parse
		b.Assert(code.BoolC(false), "unreachable: the null value of the action does not convert to the operators' type")
	})
	add("opr_flags", func(b *code.Builder) {
		b.Do("opr.setCutFailed", code.BoolC(true))
		b.Assert(code.C("opr.cutFailed"), "a candidate failed after a cut")
		b.Do("opr.setCutFailed", code.BoolC(false))
		b.Assert(code.Not(code.C("opr.cutFailed")), "and none did")
		b.Do("opr.setNudBound", i(7))
		b.Assert(eq(code.C("opr.nudBound"), i(7)), "the binding level above which no operator follows")
		b.Do("opr.setNudBound", i(2))
		b.Assert(eq(code.C("opr.nudBound"), i(2)), "as the last call set it")
		b.Assert(code.Not(code.C("opr.afterEmpty")), "no empty infix operator before any record of one")
		b.Do("opr.setAfterEmpty", code.BoolC(true))
		b.Assert(code.C("opr.afterEmpty"), "the right operand follows an empty infix operator")
		b.Do("opr.setAfterEmpty", code.BoolC(false))
		b.Assert(code.Not(code.C("opr.afterEmpty")), "and then does not")
	})
	add("opr_records", func(b *code.Builder) {
		r := b.Let("r", rule(0))
		led := b.Let("led", code.C("opr.led", r))
		f1, f2 := b.Let("f1", code.C(OprFrameOp)), b.Let("f2", code.C(OprFrameOp))
		m0, m1 := b.Let("m0", code.C(OprMarkOp)), b.Let("m1", code.C(OprMarkOp))
		o := b.Let("o", code.C("opr.at", led, i(0)))
		b.Do("rec.push", code.C(ErrOp, i(1)))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		b.SetReg(exec.Pos, i(3))
		a := b.Let("a", code.C("opr.attempt", o, f1, code.C(OprNodeOp), i(1), code.BoolC(false), errs))
		longest := b.Let("longest", code.C(OprRecordOp("oprLongest")))
		b.Do("oprLongest.setL", longest, led)
		b.Do("oprLongest.setI", longest, i(4))
		b.Do("oprLongest.setM0", longest, m0)
		b.Do("oprLongest.setBest", longest, a)
		b.Do("oprLongest.setCutFailed", longest, code.BoolC(true))
		b.Do("oprLongest.setPrevFrame", longest, f1)
		b.Do("oprLongest.setPrevCut", longest, code.BoolC(true))
		b.Do("oprLongest.setF", longest, f2)
		b.Let("l", code.C("oprLongest.l", longest))
		b.Assert(eq(code.C("oprLongest.i", longest), i(4)), "the index of the longest match")
		b.Let("lm0", code.C("oprLongest.m0", longest))
		b.Let("lbest", code.C("oprLongest.best", longest))
		b.Assert(code.C("oprLongest.cutFailed", longest), "a failure after a cut")
		b.Let("lpf", code.C("oprLongest.prevFrame", longest))
		b.Assert(code.C("oprLongest.prevCut", longest), "the cut flag before")
		b.Let("lf", code.C("oprLongest.f", longest))
		b.Do("oprLongest.setBest", longest, nullAttempt)
		b.Assert(code.Not(code.C("opr.found", code.C("oprLongest.best", longest))), "the null attempt is a record's best")
		nud := b.Let("nud", code.C(OprRecordOp("oprNud")))
		b.Do("oprNud.setM0", nud, m1)
		b.Do("oprNud.setA", nud, a)
		b.Do("oprNud.setI", nud, i(2))
		b.Do("oprNud.setPrevFrame", nud, f2)
		b.Do("oprNud.setPrevCut", nud, code.BoolC(false))
		b.Do("oprNud.setF", nud, f1)
		b.Let("nm0", code.C("oprNud.m0", nud))
		b.Assert(code.C("opr.found", code.C("oprNud.a", nud)), "the match of the start")
		b.Assert(eq(code.C("oprNud.i", nud), i(2)), "its index")
		b.Let("npf", code.C("oprNud.prevFrame", nud))
		b.Assert(code.Not(code.C("oprNud.prevCut", nud)), "no cut before")
		b.Let("nf", code.C("oprNud.f", nud))
		expr := b.Let("expr", code.C(OprRecordOp("oprExpr")))
		b.Do("oprExpr.setMin", expr, i(3))
		b.Do("oprExpr.setLhs", expr, code.C(OprNodeOp))
		b.Do("oprExpr.setLastNone", expr, i(5))
		b.Do("oprExpr.setBound", expr, i(6))
		b.Do("oprExpr.setGuard", expr, i(8))
		b.Do("oprExpr.setM0", expr, m0)
		b.Do("oprExpr.setA", expr, a)
		b.Assert(eq(code.C("oprExpr.min", expr), i(3)), "the least binding level")
		b.Let("elhs", code.C("oprExpr.lhs", expr))
		b.Assert(eq(code.C("oprExpr.lastNone", expr), i(5)), "the level of the last operator that does not associate")
		b.Assert(eq(code.C("oprExpr.bound", expr), i(6)), "the bound")
		b.Assert(eq(code.C("oprExpr.guard", expr), i(8)), "the guard")
		b.Let("em0", code.C("oprExpr.m0", expr))
		b.Let("ea", code.C("oprExpr.a", expr))
		// Two records of a kind do not share their fields.
		expr2 := b.Let("expr2", code.C(OprRecordOp("oprExpr")))
		b.Do("oprExpr.setMin", expr2, i(9))
		b.Assert(eq(code.C("oprExpr.min", expr), i(3)), "another record leaves the fields of a record as they are")
	})
	addMissing := func(name string, missing bool, body func(b *code.Builder)) {
		cfg := OprConfig()
		cfg.Missing = missing
		cs = append(cs, Case{Module: m, Func: m.AddFunc(name, nil, nil, []string{"budget", "stream", "document", exec.Missing}, body), Config: cfg})
	}
	insert := func(site, pos, err int64) code.Expr {
		return code.C(MissingInsertOp, i(site), i(pos), code.C(ErrOp, i(err)))
	}
	mark := code.C("missing.mark")
	addMissing("opr_attempt_missing", true, func(b *code.Builder) {
		o := b.Let("o", code.C("opr.at", code.C("opr.led", rule(0)), i(0)))
		f, v := b.Let("f", code.C(OprFrameOp)), b.Let("v", code.C(OprNodeOp))
		b.Assert(insert(1, 1, 31), "a token before the operator")
		// The operator's line inserts two tokens and recovers one more error.
		mm, rm := b.Let("mm", mark), b.Let("rm", code.C("rec.mark"))
		b.Assert(insert(2, 3, 32), "the line inserts a token")
		b.Do("rec.push", code.C(ErrOp, i(35)))
		b.Assert(insert(3, 4, 33), "and another")
		errs := b.Let("errs", code.C("rec.since", rm))
		b.SetReg(exec.Pos, i(6))
		a := b.Let("a", code.C("opr.attemptMissing", o, f, v, i(4), code.BoolC(false), errs, mm))
		b.Assert(all(code.C("opr.found", a), eq(code.C("opr.end", a), i(6))), "a match is found where opr.attempt finds it")
		b.Let("none", code.C("opr.attemptMissing", o, f, v, i(4), code.BoolC(false), errs, mark))
		// Another candidate wins, and the match's tokens and errors go.
		b.Do("missing.reset", mm)
		b.Do("rec.truncate", rm)
		b.Do("opr.replay", a)
		b.Assert(eq(mark, i(3)), "applying the match inserts its tokens again, with its errors")
		b.Assert(code.Not(insert(2, 3, 34)), "so that they are there")
		b.Do("opr.replay", a)
		b.Assert(eq(mark, i(3)), "a replay of a match whose tokens are present inserts none, nor their errors")
		b.Do("missing.reset", i(2))
		b.Do("opr.replay", a)
		b.Assert(eq(mark, i(3)), "a replay inserts the tokens that are missing, with their errors alone")
	})
	addMissing("opr_replay_without_tokens", true, func(b *code.Builder) {
		o := b.Let("o", code.C("opr.at", code.C("opr.led", rule(0)), i(1)))
		f, v := b.Let("f", code.C(OprFrameOp)), b.Let("v", code.C(OprNodeOp))
		b.Assert(insert(1, 1, 41), "a token before the operator")
		mm, rm := b.Let("mm", mark), b.Let("rm", code.C("rec.mark"))
		b.Do("rec.push", code.C(ErrOp, i(42)))
		errs := b.Let("errs", code.C("rec.since", rm))
		a := b.Let("a", code.C("opr.attemptMissing", o, f, v, i(1), code.BoolC(true), errs, mm))
		plain := b.Let("plain", code.C("opr.attempt", o, f, v, i(1), code.BoolC(true), errs))
		b.Do("rec.truncate", rm)
		b.Do("opr.replay", a)
		b.Do("opr.replay", plain)
		b.Assert(eq(mark, i(1)), "a match that inserted no token records its errors alone")
		b.Do("missing.reset", i(0))
		b.Do("opr.replay", a)
		b.Assert(eq(mark, i(0)), "and leaves out the tokens before it")
	})
	addMissing("opr_replay_outside_missing", false, func(b *code.Builder) {
		o := b.Let("o", code.C("opr.at", code.C("opr.prefix", rule(0)), i(0)))
		f, v := b.Let("f", code.C(OprFrameOp)), b.Let("v", code.C(OprNodeOp))
		rm := b.Let("rm", code.C("rec.mark"))
		b.Do("rec.push", code.C(ErrOp, i(51)))
		b.Do("rec.push", code.C(ErrOp, i(52)))
		errs := b.Let("errs", code.C("rec.since", rm))
		a := b.Let("a", code.C("opr.attemptMissing", o, f, v, i(2), code.BoolC(false), errs, mark))
		b.Do("rec.truncate", rm)
		b.Do("opr.replay", a)
		b.Do("opr.replay", a)
		b.Assert(eq(mark, i(0)), "an ordinary parse records the errors of the match, as rec.add does, and no token")
	})
	return cs
}

// randomOprPool holds the handles a randomized case has made, by handle type, with the model's
// mirror of each so that the case can tell what a call does to them.
type oprEntry struct {
	expr code.Expr
	h    any
}

// oprEnded stops the generation of a case of RandomOpr at a call that ends the parse.
type oprEnded struct{}

// RandomOpr returns a randomized case of n calls of the operations of the Operators Rule family from
// opNames, drawn with rng, that meets every precondition. Handles come from the descriptors of the
// rules of cfg and from the operations that make them, and the positions from a small domain.
func RandomOpr(name string, rng *rand.Rand, opNames []string, n int, cfg ref.Config) Case {
	m := OprModule()
	mirror := ref.New(cfg)
	regs := map[string]any{"pos": int64(0)}
	mirror.UseRegisters(regs)
	pool := map[string][]oprEntry{}
	features := []string{"budget", "stream", "document"}
	if slices.ContainsFunc(opNames, func(n string) bool { return slices.Contains(OprTypedOps, n) }) {
		features = append(features, exec.Typed)
	}
	// A case of the operations of the missing-token mode also inserts tokens and removes them.
	missing := slices.ContainsFunc(opNames, func(n string) bool { return slices.Contains(OprMissingOps, n) })
	if missing {
		features = append(features, exec.Missing)
		opNames = slices.Concat(opNames, []string{MissingInsertOp, MissingInsertOp, "missing.reset"})
	}
	f := m.AddFunc(name, nil, nil, features, func(b *code.Builder) {
		// A call that ends the parse (an operator's value that does not convert) ends the case.
		defer func() {
			if x := recover(); x != nil {
				if _, ok := x.(oprEnded); !ok {
					panic(x)
				}
			}
		}()
		count := 0
		fresh := func() string { count++; return fmt.Sprintf("x%d", count) }
		keep := func(t code.Type, expr code.Expr, h any) {
			pool[t.Handle] = append(pool[t.Handle], oprEntry{expr, h})
		}
		// call calls the operation and returns the handle it keeps, if any.
		call := func(opName string, args []any, exprs []code.Expr) (kept oprEntry, ok bool) {
			o := m.Op(opName)
			if isOprTestOp(opName) {
				h := mirror.OprHandle(opName)
				keep(o.Results[0], b.Let(fresh(), code.C(opName)), h)
				return pool[o.Results[0].Handle][len(pool[o.Results[0].Handle])-1], true
			}
			res, stop := mirrorCall(mirror, o, args)
			if stop {
				switch len(o.Results) {
				case 0:
					b.Do(opName, exprs...)
				case 1:
					b.Let(fresh(), code.C(opName, exprs...))
				default:
					b.LetN([]string{fresh(), fresh()}, code.C(opName, exprs...))
				}
				panic(oprEnded{})
			}
			switch len(o.Results) {
			case 0:
				b.Do(opName, exprs...)
				return kept, false
			case 1:
				v := b.Let(fresh(), code.C(opName, exprs...))
				if o.Results[0].Kind != code.KHandle {
					return kept, false
				}
				keep(o.Results[0], v, res[0])
			default:
				// A value and its success (opr.match), whose value is a handle only if it matched, or a
				// value and where it starts (opr.buildTyped).
				vs := b.LetN([]string{fresh(), fresh()}, code.C(opName, exprs...))
				if ok, matched := res[1].(bool); matched && !ok {
					return kept, false
				}
				keep(o.Results[0], vs[0], res[0])
			}
			return pool[o.Results[0].Handle][len(pool[o.Results[0].Handle])-1], true
		}
		// The handles that the cases cannot get from the descriptors.
		for range 3 {
			call(OprFrameOp, nil, nil)
			call(OprMarkOp, nil, nil)
			call(OprNodeOp, nil, nil)
		}
		if slices.Contains(features, exec.Typed) {
			call(OprAtomOp, nil, nil)
		}
		if slices.ContainsFunc(opNames, func(n string) bool { return slices.Contains(OprRecordOps, n) }) {
			for rec := range oprRecordHandle {
				call(OprRecordOp(rec), nil, nil)
				call(OprRecordOp(rec), nil, nil)
			}
		}
		// Frames of as many slots as each line has: a line runs in one of its width, whose slots the case
		// reads after the match, and its value and an operator's application take the captures of one.
		frames := make([][]oprEntry, 4)
		if slices.ContainsFunc(opNames, func(n string) bool { return n == "opr.match" || slices.Contains(OprValueBuildOps, n) }) {
			for k := range 2 * len(frames) {
				n := int64(k % len(frames))
				call("frame.open", []any{n}, []code.Expr{code.Int(n)})
				frames[n] = append(frames[n], pool[exec.Frame][len(pool[exec.Frame])-1])
			}
		}
		// The values of the family are taken apart, and a capture can be any value, which frame.set
		// writes to the current frame.
		if slices.ContainsFunc(opNames, func(n string) bool { return slices.Contains(OprValueBuildOps, n) }) {
			opNames = slices.Concat(opNames, ref.OprValueOps, []string{"frame.set"})
		}
		for k := range 2 {
			e := rng.Int64N(20)
			call("rec.push", []any{&ref.Err{ID: e}}, []code.Expr{code.C(ErrOp, code.Int(e))})
			mk := int64(rng.IntN(k + 1))
			call("rec.since", []any{mk}, []code.Expr{code.Int(mk)})
		}
		for range 2 {
			call("env.scope", nil, nil)
			slot := rng.Int64N(3)
			mirror.Call(m.Op("env.bind"), []any{slot, ref.Datum{ID: 5}})
			b.Do("env.bind", code.Int(slot), code.C(DatumOp, code.Int(5)))
		}
		draw := func(p code.Param, o *code.Op) (any, code.Expr, bool) {
			switch p.Type.Kind {
			case code.KInt:
				v := rng.Int64N(8)
				return v, code.Int(v), true
			case code.KBool:
				v := rng.IntN(2) == 0
				return v, code.BoolC(v), true
			}
			switch p.Type.Handle {
			case exec.Rule:
				i := rng.Int64N(int64(len(cfg.Oprs)))
				return ref.RuleRef{Index: i}, code.C(RuleOp, code.Int(i)), true
			case exec.Attempt:
				if rng.IntN(4) == 0 {
					return nil, code.Null(p.Type), true
				}
			case exec.Errs:
				if o.Name == "opr.attempt" && len(pool[exec.Errs]) == 0 {
					return nil, nil, false
				}
			case exec.Err:
				e := rng.Int64N(20)
				return &ref.Err{ID: e}, code.C(ErrOp, code.Int(e)), true
			}
			es := pool[p.Type.Handle]
			if len(es) == 0 {
				return nil, nil, false
			}
			e := es[rng.IntN(len(es))]
			return e.h, e.expr, true
		}
		null := code.Null(code.THandle(exec.Value))
		ask := func(op string, args ...any) any { return mirror.Call(m.Op(op), args)[0] }
		// fit makes most of the calls that make the values of the family meet their preconditions: the
		// line of the value of an operand is one of the rule, an operator's application is made of a
		// match of one of its operators with a frame of its line's slots, and it has the operands of
		// its kind.
		fit := func(o *code.Op, args []any, exprs []code.Expr) {
			frameOf := func(l any, k int) {
				if fs := frames[ask("opr.scope", l).(int64)]; len(fs) > 0 && rng.IntN(8) > 0 {
					e := fs[rng.IntN(len(fs))]
					args[k], exprs[k] = e.h, e.expr
				}
			}
			owner := func() {
				for r := range int64(len(cfg.Oprs)) {
					if args[0] = (ref.RuleRef{Index: r}); mirror.Allows(o, args) {
						exprs[0] = code.C(RuleOp, code.Int(r))
						return
					}
				}
			}
			// A mark of the tokens is one that missing.mark returned and is still in use.
			tokenMark := func(k int) {
				mk := rng.Int64N(ask("missing.mark").(int64) + 1)
				args[k], exprs[k] = mk, code.Int(mk)
			}
			switch o.Name {
			case "opr.attempt":
				frameOf(ask("opr.line", args[0]), 1)
			case "opr.attemptMissing":
				if args[0] != nil {
					frameOf(ask("opr.line", args[0]), 1)
				}
				tokenMark(6)
			case "missing.reset":
				tokenMark(0)
			case MissingInsertOp:
				// Sites and positions from small domains, so that tokens repeat.
				for k := range 2 {
					x := rng.Int64N(3)
					args[k], exprs[k] = x, code.Int(x)
				}
			case "opr.lineValue":
				frameOf(args[1], 2)
				owner()
			case ref.OprValueKidOp:
				if n, ok := args[0].(*ref.OprValue); ok && n != nil {
					if k := ask(ref.OprValueKidsOp, n).(int64); k > 0 {
						x := rng.Int64N(k)
						args[1], exprs[1] = x, code.Int(x)
					}
				}
			case "opr.build", "opr.buildTyped":
				a, ok := args[1].(*ref.OprAttempt)
				if !ok || a == nil {
					return
				}
				op := ask("opr.op", a)
				if !ask("opr.infix", op).(bool) && !ask("opr.postfix", op).(bool) {
					args[2], exprs[2] = nil, null
				}
				if ask("opr.postfix", op).(bool) {
					args[len(args)-1], exprs[len(args)-1] = nil, null
				}
				owner()
			}
		}
		pick := func(es []oprEntry) oprEntry { return es[rng.IntN(len(es))] }
		setPos := func(p int64) {
			regs[exec.Pos] = p
			b.SetReg(exec.Pos, code.Int(p))
		}
		// apart takes the value v apart: its range, its rule, its elements and its fields.
		apart := func(v oprEntry) {
			if n, ok := v.h.(*ref.OprValue); !ok || n == nil {
				return
			}
			for _, op := range []string{ref.OprValueStartOp, ref.OprValueEndOp, ref.OprValueRuleOp, ref.OprValueTypeOp, ref.OprValueKidsOp} {
				call(op, []any{v.h}, []code.Expr{v.expr})
			}
			for k := range ask(ref.OprValueKidsOp, v.h).(int64) {
				call(ref.OprValueKidOp, []any{v.h, k}, []code.Expr{v.expr, code.Int(k)})
			}
			for k := range int64(len(ref.OprFieldNames)) {
				call(ref.OprValueFieldOp, []any{v.h, k}, []code.Expr{v.expr, code.Int(k)})
			}
		}
		// missingFlow makes the match of the operator op whose line, run in the frame fr from start
		// to the value v, inserted tokens and recovered errors, as the grammar code of a line in the
		// missing-token mode does. The match may lose to another candidate, whose tokens and errors go,
		// and win later, which records its tokens and errors again.
		missingFlow := func(op, fr, v oprEntry, start int64) oprEntry {
			mm, rm := ask("missing.mark").(int64), mirror.Entries()
			// Outside the mode a line inserts no token.
			tokens := 0
			if cfg.Missing {
				tokens = rng.IntN(3)
			}
			for range tokens {
				site, pos, e := rng.Int64N(3), rng.Int64N(3), rng.Int64N(20)
				call(MissingInsertOp, []any{site, pos, &ref.Err{ID: e}}, []code.Expr{code.Int(site), code.Int(pos), code.C(ErrOp, code.Int(e))})
			}
			if rng.IntN(2) == 0 {
				e := rng.Int64N(20)
				call("rec.push", []any{&ref.Err{ID: e}}, []code.Expr{code.C(ErrOp, code.Int(e))})
			}
			errs, _ := call("rec.since", []any{rm}, []code.Expr{code.Int(rm)})
			cut := rng.IntN(2) == 0
			a, _ := call("opr.attemptMissing", []any{op.h, fr.h, v.h, start, cut, errs.h, mm},
				[]code.Expr{op.expr, fr.expr, v.expr, code.Int(start), code.BoolC(cut), errs.expr, code.Int(mm)})
			if rng.IntN(3) > 0 {
				mk := mm - rng.Int64N(mm+1)*int64(rng.IntN(2))
				call("missing.reset", []any{mk}, []code.Expr{code.Int(mk)})
				call("rec.truncate", []any{rm}, []code.Expr{code.Int(rm)})
			}
			for range 1 + rng.IntN(2) {
				call("opr.replay", []any{a.h}, []code.Expr{a.expr})
			}
			return a
		}
		// flow runs a line of a rule as the grammar code runs it: in a frame of its slots, from a
		// position, and then makes its value, of an operand or of an application of its operator, and
		// takes the value apart.
		flow := func() {
			r := rng.Int64N(int64(len(cfg.Oprs)))
			rule := oprEntry{code.C(RuleOp, code.Int(r)), ref.RuleRef{Index: r}}
			var line, op oprEntry
			operator := rng.IntN(3) > 0
			if operator {
				list, _ := call([]string{"opr.led", "opr.prefix"}[rng.IntN(2)], []any{rule.h}, []code.Expr{rule.expr})
				k := ask("opr.count", list.h).(int64)
				if k == 0 {
					return
				}
				i := rng.Int64N(k)
				op, _ = call("opr.at", []any{list.h, i}, []code.Expr{list.expr, code.Int(i)})
				line, _ = call("opr.line", []any{op.h}, []code.Expr{op.expr})
			} else {
				k := ask("opr.operands", rule.h).(int64)
				if k == 0 {
					return
				}
				i := rng.Int64N(k)
				line, _ = call("opr.operand", []any{rule.h, i}, []code.Expr{rule.expr, code.Int(i)})
			}
			fr := pick(frames[ask("opr.scope", line.h).(int64)])
			call("frame.use", []any{fr.h}, []code.Expr{fr.expr})
			start := rng.Int64N(8)
			setPos(start)
			v, ok := call("opr.match", []any{line.h}, []code.Expr{line.expr})
			if !ok {
				return
			}
			var value oprEntry
			if !operator {
				value, _ = call("opr.lineValue", []any{rule.h, line.h, fr.h, v.h, start},
					[]code.Expr{rule.expr, line.expr, fr.expr, v.expr, code.Int(start)})
			} else {
				errs := pick(pool[exec.Errs])
				var a oprEntry
				if missing && rng.IntN(2) == 0 {
					a = missingFlow(op, fr, v, start)
				} else {
					a, _ = call("opr.attempt", []any{op.h, fr.h, v.h, start, false, errs.h},
						[]code.Expr{op.expr, fr.expr, v.expr, code.Int(start), code.BoolC(false), errs.expr})
				}
				lhs, rhs := pick(pool[exec.Value]), pick(pool[exec.Value])
				if !ask("opr.infix", op.h).(bool) && !ask("opr.postfix", op.h).(bool) {
					lhs = oprEntry{null, nil}
				}
				if ask("opr.postfix", op.h).(bool) {
					rhs = oprEntry{null, nil}
				}
				setPos(regs[exec.Pos].(int64) + rng.Int64N(4))
				if slices.Contains(opNames, "opr.buildTyped") && rng.IntN(2) == 0 {
					lstart := rng.Int64N(8)
					value, _ = call("opr.buildTyped", []any{rule.h, a.h, lhs.h, lstart, rhs.h},
						[]code.Expr{rule.expr, a.expr, lhs.expr, code.Int(lstart), rhs.expr})
				} else {
					value, _ = call("opr.build", []any{rule.h, a.h, lhs.h, rhs.h}, []code.Expr{rule.expr, a.expr, lhs.expr, rhs.expr})
				}
			}
			apart(value)
		}
		flows := len(frames[0]) > 0 && len(pool[exec.Errs]) > 0 &&
			slices.ContainsFunc(opNames, func(n string) bool { return slices.Contains(OprValueBuildOps, n) })
		for i := 0; count < n && i < 80*n; i++ {
			if flows && rng.IntN(6) == 0 {
				flow()
				continue
			}
			o := m.Op(opNames[rng.IntN(len(opNames))])
			args := make([]any, len(o.Params))
			exprs := make([]code.Expr, len(o.Params))
			ok := true
			for k, p := range o.Params {
				if args[k], exprs[k], ok = draw(p, o); !ok {
					break
				}
			}
			if ok {
				fit(o, args, exprs)
			}
			// opr.match runs in a frame that the case makes current first.
			if !ok || o.Name != "opr.match" && !mirror.Allows(o, args) {
				continue
			}
			if o.Name == "opr.attempt" || o.Name == "opr.attemptMissing" || slices.Contains(OprMatchOps, o.Name) {
				pos := rng.Int64N(12)
				regs["pos"] = pos
				b.SetReg(exec.Pos, code.Int(pos))
			}
			var current oprEntry
			if o.Name == "opr.match" {
				scope := mirror.Call(m.Op("opr.scope"), args)[0].(int64)
				current = frames[scope][rng.IntN(len(frames[scope]))]
				call("frame.use", []any{current.h}, []code.Expr{current.expr})
				if !mirror.Allows(o, args) {
					continue
				}
			}
			if slices.Contains(OprMatchOps, o.Name) {
				cut := rng.IntN(2) == 0
				regs[exec.Cut] = cut
				b.SetReg(exec.Cut, code.BoolC(cut))
			}
			call(o.Name, args, exprs)
			if o.Name == "opr.match" {
				for k := range mirror.Call(m.Op("frame.slots"), []any{current.h})[0].(int64) {
					call(ref.FrameSlotOp, []any{current.h, k}, []code.Expr{current.expr, code.Int(k)})
				}
			}
		}
	})
	return Case{Module: m, Func: f, Config: cfg}
}
