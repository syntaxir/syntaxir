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
)

// oprRecordOpPrefix starts the name of the operation that makes a record of the split form with the
// prefix that follows it (OprRecordOp).
const oprRecordOpPrefix = "test.oprRecord."

// OprRecordOp returns the operation that makes a record of the split form with the operations that
// start with prefix (oprLongest, oprNud or oprExpr).
func OprRecordOp(prefix string) string { return oprRecordOpPrefix + prefix }

// isOprTestOp reports whether name is an operation that makes a handle of the Operators Rule family.
func isOprTestOp(name string) bool {
	return name == OprFrameOp || name == OprMarkOp || name == OprNodeOp || strings.HasPrefix(name, oprRecordOpPrefix)
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
var OprOps = slices.Concat(OprDescriptorOps, OprMatchOps, OprRecordOps)

// OprSupportOps are the operations that cases of the Operators Rule family call to make the errors and
// the environments they pass: the implementation provides them besides OprOps.
var OprSupportOps = []string{"rec.push", "rec.since", "env.bind", "env.scope"}

// OprFrameOps are the operations of the capture frames that the cases of OprMatchOps call: a line
// sets its captures in the current frame, and a case opens and uses frames, reads their slots and
// undoes their writes.
var OprFrameOps = []string{"frame.open", "frame.use", "frame.current", "frame.slots", "frame.mark", "frame.markTrail",
	"frame.undo", ref.FrameSlotOp}

// OprSuiteOps returns OprOps, OprSupportOps and OprFrameOps, the operations a suite of the family
// provides.
func OprSuiteOps() []string { return slices.Concat(OprOps, OprSupportOps, OprFrameOps) }

// OprModule returns the declarations of Module with the operations that make the handles of the
// cases of the Operators Rule family.
func OprModule() *code.Module {
	m := Module()
	test := func(name string, handle string) {
		m.AddOp(&code.Op{Name: name, Component: exec.Rules, Results: []code.Type{code.THandle(handle)},
			Contract: "Returns a new handle for a conformance case."})
	}
	test(OprFrameOp, exec.Frame)
	test(OprMarkOp, exec.Backtrack)
	test(OprNodeOp, exec.Value)
	for rec, h := range oprRecordHandle {
		test(OprRecordOp(rec), h)
	}
	m.AddOp(&code.Op{Name: ref.FrameSlotOp, Component: exec.Frames, Reads: []string{exec.Frames},
		Params:   []code.Param{code.P("f", code.THandle(exec.Frame)), code.P("slot", code.TInt)},
		Results:  []code.Type{code.THandle(exec.Value)},
		Contract: `Returns the value in the slot of f, the null value for an empty slot.`,
		Requires: `slot is a slot of f.`})
	return m
}

// OprConfig returns the Operators Rules of the cases, with variables for the environment: a rule of
// each kind of operator that skips trivia, a rule without prefix operators, and one with no operator.
// The scripts of the trivia and of the lines of the first rule match and fail, move the position by
// different lengths, record errors and pass cuts; the other lines have none and fail at once.
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
	return ref.Config{
		Vars: []int64{1, 2, 3},
		Oprs: []ref.OprRule{
			{Prefix: []ref.OprOp{op("prefix", "left", 9, 1, x(true, 2, false)), op("prefix", "left", 8, 0)},
				Led: []ref.OprOp{op("infix", "left", 1, 0, x(true, 1, false), x(false, 2, true), x(true, 3, true, 6), x(false, 0, false)),
					op("infix", "left", 2, 2, captures(true, 1, 0, 1), captures(false, 2, 1), captures(true, 1)),
					op("infix", "right", 3, 1, x(true, 0, true)),
					op("postfix", "left", 4, 0, x(false, 1, false, 7), x(true, 4, false)), op("infix", "none", 5, 3)},
				Operands: []ref.OprLineSpec{{Scope: 0, Expr: []ref.OprExpr{x(true, 3, false), x(false, 1, true)}},
					{Scope: 2, Expr: []ref.OprExpr{captures(true, 2, 1)}}}, Skip: true,
				Trivia: []ref.Eval{{OK: true, Len: 2}, {OK: false, Len: 1}, {OK: true}, {OK: true, Len: 1, Errs: []int64{9}}}},
			{Led: []ref.OprOp{op("infix", "right", 1, 0), op("postfix", "none", 2, 1)}, Operands: []ref.OprLineSpec{{Scope: 1}}},
			{},
		},
	}
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
		b.Assert(eq(code.C("opr.operands", r), i(2)), "the operand lines of the rule")
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
	return cs
}

// randomOprPool holds the handles a randomized case has made, by handle type, with the model's
// mirror of each so that the case can tell what a call does to them.
type oprEntry struct {
	expr code.Expr
	h    any
}

// RandomOpr returns a randomized case of n calls of the operations of the Operators Rule family from
// opNames, drawn with rng, that meets every precondition. Handles come from the descriptors of the
// rules of cfg and from the operations that make them, and the positions from a small domain.
func RandomOpr(name string, rng *rand.Rand, opNames []string, n int, cfg ref.Config) Case {
	m := OprModule()
	mirror := ref.New(cfg)
	regs := map[string]any{"pos": int64(0)}
	mirror.UseRegisters(regs)
	pool := map[string][]oprEntry{}
	f := m.AddFunc(name, nil, nil, []string{"budget", "stream", "document"}, func(b *code.Builder) {
		count := 0
		fresh := func() string { count++; return fmt.Sprintf("x%d", count) }
		keep := func(t code.Type, expr code.Expr, h any) {
			pool[t.Handle] = append(pool[t.Handle], oprEntry{expr, h})
		}
		call := func(opName string, args []any, exprs []code.Expr) {
			o := m.Op(opName)
			if isOprTestOp(opName) {
				h := mirror.OprHandle(opName)
				keep(o.Results[0], b.Let(fresh(), code.C(opName)), h)
				return
			}
			res := mirror.Call(o, args)
			switch len(o.Results) {
			case 0:
				b.Do(opName, exprs...)
			case 1:
				v := b.Let(fresh(), code.C(opName, exprs...))
				if o.Results[0].Kind == code.KHandle {
					keep(o.Results[0], v, res[0])
				}
			case 2:
				// A value and its success (opr.match): the value is a handle only if it matched.
				vs := b.LetN([]string{fresh(), fresh()}, code.C(opName, exprs...))
				if res[1].(bool) {
					keep(o.Results[0], vs[0], res[0])
				}
			}
		}
		// The handles that the cases cannot get from the descriptors.
		for range 3 {
			call(OprFrameOp, nil, nil)
			call(OprMarkOp, nil, nil)
			call(OprNodeOp, nil, nil)
		}
		if slices.ContainsFunc(opNames, func(n string) bool { return slices.Contains(OprRecordOps, n) }) {
			for rec := range oprRecordHandle {
				call(OprRecordOp(rec), nil, nil)
				call(OprRecordOp(rec), nil, nil)
			}
		}
		// A frame for the captures of the lines, as wide as the widest line, whose slots the case reads
		// after each match.
		var captures oprEntry
		if slices.Contains(opNames, "opr.match") {
			call("frame.open", []any{int64(3)}, []code.Expr{code.Int(3)})
			captures = pool[exec.Frame][len(pool[exec.Frame])-1]
			call("frame.use", []any{captures.h}, []code.Expr{captures.expr})
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
			}
			es := pool[p.Type.Handle]
			if len(es) == 0 {
				return nil, nil, false
			}
			e := es[rng.IntN(len(es))]
			return e.h, e.expr, true
		}
		for i := 0; count < n && i < 80*n; i++ {
			o := m.Op(opNames[rng.IntN(len(opNames))])
			args := make([]any, len(o.Params))
			exprs := make([]code.Expr, len(o.Params))
			ok := true
			for k, p := range o.Params {
				if args[k], exprs[k], ok = draw(p, o); !ok {
					break
				}
			}
			if !ok || !mirror.Allows(o, args) {
				continue
			}
			if o.Name == "opr.attempt" || slices.Contains(OprMatchOps, o.Name) {
				pos := rng.Int64N(12)
				regs["pos"] = pos
				b.SetReg(exec.Pos, code.Int(pos))
			}
			if slices.Contains(OprMatchOps, o.Name) {
				cut := rng.IntN(2) == 0
				regs[exec.Cut] = cut
				b.SetReg(exec.Cut, code.BoolC(cut))
			}
			call(o.Name, args, exprs)
			if o.Name == "opr.match" {
				for k := range int64(3) {
					call(ref.FrameSlotOp, []any{captures.h, k}, []code.Expr{captures.expr, code.Int(k)})
				}
			}
		}
	})
	return Case{Module: m, Func: f, Config: cfg}
}
