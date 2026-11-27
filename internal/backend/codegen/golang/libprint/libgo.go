package libprint

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// libGoFile is the file of the printed compositions in the Go library of generated parsers,
// relative to the root of the module, which go generate in this directory writes.
const libGoFile = "pkg/runtime/libgo/call_exec.go"

// goStreamFile is the file of the printed stream compositions in the Go library, relative
// to the root of the module.
const goStreamFile = "pkg/runtime/libgo/stream_exec.go"

// goOperatorsFile is the file of the printed Operators Rule compositions in the generated Go library,
// relative to the root of the module.
const goOperatorsFile = "pkg/runtime/libgo/operators_exec.go"

// goSpec is a printed function of the Go library: a specialization of a composition for the node
// Parser or the typed TParser.
type goSpec struct {
	fn    string
	typed bool
	on    []string
	name  string // the Go name
	plain bool   // printed as a function of its package, without the parser as receiver
	// doc, if not empty, replaces the Doc of the function; the Go library names the form of an
	// ordinary parse, which no Exec IR function describes.
	doc string
	// cold, if not empty, is the Go name of the function that the cold path of fn is split into, which
	// the request asks for (library.Request.Cold) and the pipeline of the library makes
	// (gopass.ColdPath).
	cold string
}

// goSpecs are the functions of the Go library that the compositions replace.
var goSpecs = []goSpec{
	// Parser.Call hands observed and Document parses to the tracked path, so it neither observes nor
	// reads a Document itself beyond that test.
	{fn: "call", on: []string{exec.Document, exec.Observe, exec.Missing, exec.Units}, name: "Call"},
	// The deferral of memoization: the node library's is the one that generated parsers call, also for
	// the typed library's rules; the typed library's compositions call their own, of a typed rule.
	{fn: "firstCall", name: "FirstCall"},
	{fn: "firstCall", typed: true, name: "firstCall"},
	{fn: "grow", on: []string{exec.Missing, exec.Units}, name: "grow"},
	// The expectation record, whose guard inlines into the generated code: no form of a parse counts a
	// unit for it.
	{fn: "expect", on: []string{exec.Units}, name: "Expect", cold: "expectSlow"},
	{fn: "mergeExpected", on: []string{exec.Units}, name: "MergeExpected"},
	{fn: "keep", on: []string{exec.Units}, name: "Keep"},
	{fn: "call", typed: true, on: []string{exec.Document, exec.Observe, exec.Missing, exec.Typed, exec.Units}, name: "callBody"},
	{fn: "callMemo", typed: true, on: []string{exec.Document, exec.Observe, exec.Missing, exec.Typed, exec.Units}, name: "CallMemo"},
	{fn: "grow", typed: true, on: []string{exec.Missing, exec.Typed, exec.Units}, name: "grow"},
	// The observed calls and plain invocations of both libraries, which their callers take in an
	// observed parse.
	{fn: "observed", on: []string{exec.Document, exec.Observe}, name: "callObserved"},
	{fn: "observedPlain", on: []string{exec.Document, exec.Observe}, name: "invokePlainObserved"},
	{fn: "observed", typed: true, on: []string{exec.Document, exec.Observe, exec.Typed}, name: "callObserved"},
	{fn: "observedPlain", typed: true, on: []string{exec.Document, exec.Observe}, name: "invokePlainObserved"},
	// The tracked path of the node library: the rule call of a Document parse, the unmemoized and
	// memoized calls, the call without observation, and the entry of the path.
	{fn: "document", on: []string{exec.Document, exec.Units}, name: "callDocument"},
	{fn: "unmemoized", on: []string{exec.Missing}, name: "callUnmemoized"},
	{fn: "memoized", on: []string{exec.Document, exec.Observe, exec.Missing, exec.Units}, name: "callMemo"},
	{fn: "body", on: []string{exec.Document, exec.Missing, exec.Units}, name: "callBody"},
	{fn: "tracked", on: []string{exec.Document, exec.Observe}, name: "callTracked"},
	// The rule call of a Document parse in the typed library.
	{fn: "typedDocument", typed: true, on: []string{exec.Document, exec.Observe, exec.Missing, exec.Typed, exec.Units}, name: "DocumentCall"},
	// The edits of a Document that a lookup applies to the memo entry it finds, in both libraries: the
	// form a parse runs and the form an edit runs, which has no parse. Applying an edit is no event, so
	// the two do the same work.
	{fn: "memoAdvance", on: []string{exec.Document, exec.Units}, name: "advanceEntry"},
	{fn: "memoAdvance", on: []string{exec.Document}, name: "advanceEntry", plain: true},
	{fn: "memoAdvance", typed: true, on: []string{exec.Document, exec.Units}, name: "advanceEntry"},
	{fn: "memoAdvance", typed: true, on: []string{exec.Document}, name: "advanceTEntry", plain: true},
	// The repetition runs of a Document parse in the node library, which generated rule bodies run
	// through ResumeStart, ResumeMore, ResumeNext, ResumeBegin, ResumeEnd and ResumeFinish, the forms of
	// the Flow operations of the same names (flow.resumeStart and the others).
	{fn: "runStart", on: goRunOn, name: "runStart"},
	{fn: "runMore", on: goRunOn, name: "ResumeMore"},
	{fn: "runReuse", on: goRunOn, name: "runReuse"},
	{fn: "runPrefix", on: goRunOn, name: "runPrefix"},
	{fn: "runResync", on: goRunOn, name: "runResync"},
	{fn: "runTail", on: goRunOn, name: "runTail"},
	{fn: "runNext", on: goRunOn, name: "ResumeNext"},
	{fn: "runBegin", on: goRunOn, name: "ResumeBegin"},
	{fn: "runEnd", on: goRunOn, name: "ResumeEnd"},
	{fn: "runFinish", on: goRunOn, name: "runFinish"},
	// The predicates and bindings of both libraries.
	{fn: "predicate", name: "Predicate"},
	{fn: "bind", name: "Bind"},
	{fn: "predicate", typed: true, name: "Predicate"},
	{fn: "bind", typed: true, name: "Bind"},
}

// goRunOn are the features of the node library's forms of the repetition runs of a Document: a
// parse with limit controls counts logical units, and polls them on work that counts none.
var goRunOn = []string{exec.Document, exec.Units}

// goStreamSpecs are the functions of the stream mechanism that the compositions replace, in the
// node library (the typed library has no streams).
var goStreamSpecs = []goSpec{
	{fn: "streamEmit", on: []string{exec.Stream, exec.Units}, name: "EmitStream"},
	{fn: "streamCommit", on: []string{exec.Stream}, name: "streamCommit"},
	{fn: "streamsIn", on: []string{exec.Stream}, name: "StreamsIn"},
}

// goOperatorSpecs are the functions of the Operators Rule loop that the compositions replace, in the
// node and the typed library.
var goOperatorSpecs = []goSpec{
	{fn: "operatorsParse", on: []string{exec.Missing, exec.Units}, name: "OperatorsRuleParse"},
	{fn: "operatorsExpr", on: []string{exec.Missing, exec.Units}, name: "operatorsRuleExpr"},
	{fn: "operatorsNud", on: []string{exec.Missing, exec.Units}, name: "operatorsRuleNud"},
	{fn: "longest", on: []string{exec.Missing, exec.Units}, name: "longest"},
	{fn: "skipTrivia", on: []string{exec.Missing}, name: "skipTrivia"},
	{fn: "reset", on: []string{exec.Missing}, name: "Reset"},
	{fn: "operatorsParseTyped", typed: true, on: []string{exec.Missing, exec.Typed, exec.Units}, name: "OperatorsRuleParse"},
	{fn: "operatorsExprTyped", typed: true, on: []string{exec.Missing, exec.Typed, exec.Units}, name: "operatorsRuleExpr"},
	{fn: "operatorsNudTyped", typed: true, on: []string{exec.Missing, exec.Typed, exec.Units}, name: "operatorsRuleNud"},
	{fn: "longest", typed: true, on: []string{exec.Missing, exec.Units}, name: "longest"},
	{fn: "skipTrivia", typed: true, on: []string{exec.Missing}, name: "skipTrivia"},
	{fn: "reset", typed: true, on: []string{exec.Missing}, name: "Reset"},
}

// printGoOperators returns the Go file of the printed Operators Rule compositions for package libgo.
func printGoOperators(u file) (string, error) { return goFile(u, goOperatorSpecs) }

// printGoStream returns the Go file of the printed stream compositions for package libgo.
func printGoStream(u file) (string, error) {
	m := u.module()
	var b strings.Builder
	for _, s := range goStreamSpecs {
		f, err := u.fn(s)
		if err != nil {
			return "", err
		}
		src, err := print.Func(m, f, goTarget(s))
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + src)
	}
	return libFileHeader(b.String(), nil) + b.String(), nil
}

// libFileHeader returns the header of a printed file of package libgo with the body: the
// generated-code comment, the package clause and the imports of the standard packages std and of
// the packages of the library that body names.
func libFileHeader(body string, std []string) string {
	return "// Code generated by internal/backend/codegen/golang/libprint; DO NOT EDIT.\n\npackage libgo\n" + libImports(body, std)
}

// printLibGo returns the Go file of the printed compositions for package libgo.
func printLibGo(u file) (string, error) {
	body, err := goBody(u, goSpecs)
	if err != nil {
		return "", err
	}
	// The limit on pending edits is defined once, in the operations, and printed for the library.
	limit := fmt.Sprintf(`
// pendingEditLimit is the number of edits that a memo entry of a Document may lag behind
// (exec.PendingEditLimit): a lookup treats an entry that lags behind by more edits as absent, as the
// engine does, and the typed memo drops an entry once more edits have passed since a parse stored it or
// found it.
const pendingEditLimit = %d

// minRecordedRun is the number of elements below which a Document records no run of a repetition
// (exec.MinRecordedRun), as in the engine.
const minRecordedRun = %d
`, exec.PendingEditLimit, exec.MinRecordedRun)
	return libFileHeader(body, nil) + limit + body, nil
}

// goFile returns the Go file of the printed functions specs for package libgo.
func goFile(u file, specs []goSpec) (string, error) {
	body, err := goBody(u, specs)
	if err != nil {
		return "", err
	}
	return libFileHeader(body, nil) + body, nil
}

// goBody returns the printed functions specs for package libgo.
func goBody(u file, specs []goSpec) (string, error) {
	m := u.module()
	var b strings.Builder
	for _, s := range specs {
		fs, err := goFuncs(u, s)
		if err != nil {
			return "", err
		}
		t := goTarget(s)
		if s.plain {
			t.Recv = ""
		}
		if made := fs[1:]; len(made) > 0 {
			// The function that the cold path is split into, which the guard calls, has the Go name
			// that the request asked for (library.Request.Cold), s.cold, and is printed under it.
			if len(made) > 1 || made[0].Name != s.cold {
				return "", fmt.Errorf("no Go name for the functions %d made of %s", len(made), s.name)
			}
			name := t.FuncName
			t.FuncName = func(n string) string {
				if n == s.cold {
					return n
				}
				return name(n)
			}
		}
		for _, f := range fs {
			src, err := print.Func(m, f, t)
			if err != nil {
				return "", err
			}
			b.WriteString("\n" + src)
		}
	}
	return b.String(), nil
}

// goFuncs returns the functions that the Go library prints for s: the function of the composition
// specialized for its features, and the functions that the Go passes of the library's pipeline made
// of it, such as the cold path that go-cold-path splits from its guard (gopass.ColdPath).
func goFuncs(u file, s goSpec) ([]*code.Func, error) {
	f, err := u.fn(s)
	if err != nil {
		return nil, err
	}
	if s.doc != "" {
		f.Doc = s.doc
	}
	made, err := u.made(s)
	if err != nil {
		return nil, err
	}
	return append([]*code.Func{f}, made...), nil
}

// goTarget returns the Go implementation of the operations in the node or the typed library, for
// the printed function s.
func goTarget(s goSpec) *print.Target {
	typed := s.typed
	t := &print.Target{Recv: "p *Parser", Source: "internal/ir/code/exec/compose"}
	handles := map[string]string{
		exec.Rule: "*Rule", exec.Value: "*sxirvalue.Node", exec.Entry: "*memoEntry", exec.Values: "[]any", exec.Errs: "recEntry",
		exec.ExpID: "ExpID", exec.Mark: "ExpMark", exec.Set: "[]ExpID", exec.Kept: "[]ExpID", exec.EditLog: "[]documentEdit",
		exec.Ctx: "ctxCallState", exec.Res: "*ctxResult", exec.Grow: "growState", exec.Run: "*RunState",
		exec.Frame: "*CaptureFrame", exec.Backtrack: "Mark", exec.Scope: "*Env", exec.Operators: "[]*OperatorsRuleOp",
		exec.Operator: "*OperatorsRuleOp", exec.Line: "*OperatorsRuleLine", exec.Attempt: "operatorsRuleAttempt",
		action.Action: "func(*ActCtx) (any, any)", exec.Datum: "any", action.Seq: "[]*sxirvalue.Node",
		action.Obj: "*sxirvalue.Node", action.Name: "string", action.Str: "string", action.Act: "*ActCtx", action.Fail: "any", action.Elem: "*sxirvalue.Node",
		action.NodeType: "*nodeTypeKinds", action.FType: "*FieldType",
	}
	names := map[string]string{"streamEmit": "EmitStream", "streamCommit": "streamCommit", "streamsIn": "StreamsIn", "call": "Call", "firstCall": "FirstCall", "grow": "grow", "unmemoized": "callUnmemoized", "memoized": "callMemo",
		"document": "callDocument", "body": "callBody", "observed": "callObserved", "tracked": "callTracked", "expect": "Expect", "mergeExpected": "MergeExpected", "keep": "Keep",
		// A kept set is a slice like the set of an isolation, so MergeExpected merges both.
		"mergeKept": "MergeExpected"}
	for _, s := range goOperatorSpecs {
		names[s.fn] = s.name
	}
	libGoActionNames(names)
	entryType, valueField, alloc := "memoEntry", "node", "p.memo.alloc()"
	attemptType := "operatorsRuleAttempt"
	// created is the field of the nodes that action expressions and actions create.
	created := "created"
	rule := func(r string) string { return r }
	// step passes the value of a growth step as the node or the typed value of growAdopt and growKeepStep.
	step := func(v string) string { return v + ", nil" }
	if typed {
		t.Recv = "p *TParser"
		handles[exec.Rule], handles[exec.Value], handles[exec.Entry] = "*TRule", "any", "*tmemoEntry"
		handles[exec.Body] = "func(*TParser) (any, bool)"
		handles[exec.Frame], handles[exec.Operators], handles[exec.Operator] = "*TCaptureFrame", "[]*TOperatorsRuleOp", "*TOperatorsRuleOp"
		handles[exec.Line], handles[exec.Attempt] = "*TOperatorsRuleLine", "toperatorsRuleAttempt"
		handles[action.Action] = "func(*TActCtx) (any, any)"
		handles[action.Seq], handles[action.Obj], handles[action.Act], handles[action.Elem] = "[]any", "TVal", "*TActCtx", "any"
		handles[action.NodeType] = "uint16"
		names = map[string]string{"call": "callBody", "firstCall": "firstCall", "typedDocument": "DocumentCall", "observed": "callObserved", "callMemo": "CallMemo", "grow": "grow", "keep": "Keep",
			"mergeExpected": "MergeExpected", "mergeKept": "MergeExpected"}
		for _, s := range goOperatorSpecs {
			names[s.fn] = s.name
		}
		libGoActionNames(names)
		entryType, valueField, alloc = "tmemoEntry", "val", "p.memoSlab.one()"
		attemptType, created = "toperatorsRuleAttempt", "Created"
		rule = func(r string) string { return r + ".Rule" }
		step = func(v string) string { return "nil, " + v }
	}
	if !typed {
		for _, r := range goSpecs {
			if slices.Equal(r.on, goRunOn) && strings.HasPrefix(r.fn, "run") {
				names[r.fn] = r.name
			}
		}
	}
	names[s.fn] = s.name
	t.Handle = func(name string) string { return handles[name] }
	t.FuncName = func(name string) string { return names[name] }
	t.Reg = func(name string) string {
		return map[string]string{exec.Pos: "p.Pos", exec.Silent: "p.Silent", exec.Cut: "p.Cut"}[name]
	}
	t.Op = func(c *code.Call, a []string) string {
		if s, ok := goValueOp(c.Callee, a, typed); ok {
			return s
		}
		if s, ok := goRunOp(c.Callee, a); ok && !typed {
			return s
		}
		switch c.Callee {
		case "depth.enter":
			return "p.Depth++\nif p.Depth > p.MaxDepth {\np.TooDeep()\n}"
		case "depth.exit":
			return "p.Depth--"
		case "frame.mark":
			return "p.Mark()"
		case "frame.markPos":
			return a[0] + ".Pos"
		case "frame.markRec":
			return a[0] + ".recovered"
		case "frame.markEnv":
			return a[0] + ".env"
		case "frame.markTrail":
			return a[0] + ".Trail"
		case "frame.undo":
			if typed {
				return fmt.Sprintf("p.undoCaptures(%s)\np.DropTrail(%[1]s)", a[0])
			}
			return fmt.Sprintf("p.undoCaptures(%s)\np.Trail = p.Trail[:%[1]s]", a[0])
		case "frame.current":
			return "p.Frame"
		case "frame.use":
			return "p.Frame = " + a[0]
		case "frame.open":
			return "p.NewFrame(" + a[0] + ")"
		case "frame.slots":
			return "len(" + a[0] + ".Vals)"
		case "frame.set":
			return fmt.Sprintf("p.SetCapture(%s, %s)", a[0], a[1])
		case "frame.close":
			if typed {
				return "p.FreeFrame(" + a[0] + ")"
			}
			return ""
		case "frame.save":
			return fmt.Sprintf("p.saved = append(p.saved[:0], %s.Vals...)", a[0])
		case "frame.restore":
			return fmt.Sprintf("copy(%s.Vals, p.saved)", a[0])
		case "env.restore":
			return "p.Env = " + a[0]
		case "env.bind":
			return fmt.Sprintf("p.bind(%s, %s)", a[0], a[1])
		case "action.ctx":
			if typed {
				return "p.UseCtx(TActCtx{P: p, F: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})"
			}
			return "p.useCtx(ActCtx{P: p, f: p.Frame, start: p.Pos, end: p.Pos, cbase: len(p.created)})"
		case "action.created":
			return "len(p." + created + ")"
		case "action.dropCreated":
			return fmt.Sprintf("clear(p.%s[%s:])\np.%[1]s = p.%[1]s[:%[2]s]", created, a[0])
		case "action.kids":
			return "len(p.KidStack)"
		case "action.dropKids":
			return fmt.Sprintf("clear(p.KidStack[%s:])\np.KidStack = p.KidStack[:%[1]s]", a[0])
		case "action.elemsAbove":
			return "p.Kids(" + a[0] + ")"
		case "action.keep":
			return fmt.Sprintf("p.KidStack = append(p.KidStack, %s[%s])", a[0], a[1])
		case "opr.led":
			return a[0] + ".OperatorsRule.Led"
		case "opr.prefix":
			return a[0] + ".OperatorsRule.Prefix"
		case "opr.count":
			return "len(" + a[0] + ")"
		case "opr.at":
			return a[0] + "[" + a[1] + "]"
		case "opr.operands":
			return "len(" + a[0] + ".OperatorsRule.Operands)"
		case "opr.operand":
			return a[0] + ".OperatorsRule.Operands[" + a[1] + "]"
		case "opr.line":
			return a[0] + ".Line"
		case "opr.level":
			return a[0] + ".Level"
		case "opr.infix":
			return a[0] + `.Kind == "infix"`
		case "opr.postfix":
			return a[0] + `.Kind == "postfix"`
		case "opr.right":
			return a[0] + `.Assoc == "right"`
		case "opr.nonAssoc":
			return a[0] + `.Assoc == "none"`
		case "opr.scope":
			return "len(" + a[0] + ".Scope)"
		case "opr.hasSkip":
			return a[0] + ".OperatorsRule.Skip != nil"
		case "opr.match":
			return a[0] + ".M(p)"
		case "opr.lineValue":
			return fmt.Sprintf("p.lineResult(%s, %s, %s, %s, %s)", a[0], a[1], a[2], a[3], a[4])
		case "opr.build":
			return fmt.Sprintf("p.operatorsRuleBuild(%s, &%s, %s, %s)", a[0], a[1], a[2], a[3])
		case "opr.buildTyped":
			return fmt.Sprintf("p.operatorsRuleBuild(%s, &%s, %s, %s, %s)", a[0], a[1], a[2], a[3], a[4])
		case "opr.attempt":
			return fmt.Sprintf("%s{op: %s, frame: %s, v: %s, start: %s, end: p.Pos, env: p.Env, cut: %s, errs: %s}",
				attemptType, a[0], a[1], a[2], a[3], a[4], a[5])
		case "opr.found":
			return a[0] + ".op != nil"
		case "opr.op", "opr.end", "opr.cut", "opr.env", "opr.errs":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "opr.")
		case "opr.setCutFailed":
			return "p.cutFailed = " + a[0]
		case "opr.cutFailed":
			return "p.cutFailed"
		case "opr.setNudBound":
			return "p.nudBound = " + a[0]
		case "opr.nudBound":
			return "p.nudBound"
		case "opr.setAfterEmpty":
			return "p.afterEmpty = " + a[0]
		case "opr.afterEmpty":
			return "p.afterEmpty"
		case "units.count":
			if k, ok := c.Args[0].(*code.Const); ok && k.Value == uint64(1) {
				return "p.Units++"
			}
			return "p.Units += " + a[0]
		case "units.check":
			return "if p.Units > p.UnitsCheck {\np.PollUnits()\n}"
		case "units.key":
			return "p.KeyUnits(" + rule(a[0]) + ")"
		case "rule.cyclic":
			return a[0] + ".Rec"
		case "rec.entries":
			return "len(p.Recovered) - " + a[0]
		case "seen.defers":
			// Set once at the start of a parse: a stream or a Document parse memoizes on the first call.
			return "p.deferMemo"
		case "seen.first":
			return "p.memo.first(" + a[1] + ", " + a[0] + ")"
		// A parse in the missing-token mode memoizes no rule that can insert a missing token (insertsRule),
		// a Document parse included, except that a leader still grows through its memo: the rule
		// operations report so, as the engine's do.
		case "rule.memo":
			return "(" + a[0] + ".Memo && !p.insertsRule(" + rule(a[0]) + "))"
		// The missing-token mode (missing.go). Only a seed of a growth, a context result, holds insertions:
		// a memo entry that a call in the mode reuses is one of a rule that cannot insert.
		case "missing.active":
			return "p.inserting()"
		case "missing.recompute":
			return "p.insertsRule(" + rule(a[0]) + ")"
		case "missing.mark":
			return "p.MissingLen()"
		case "missing.reset":
			return "p.ResetMissing(" + a[0] + ")"
		case "missing.replayResult":
			return fmt.Sprintf("p.replayMissing(%s.ins, %[1]s.errs, true)", a[0])
		case "frame.markMissing":
			return a[0] + ".missing"
		case "opr.attemptMissing":
			return fmt.Sprintf("%s{op: %s, frame: %s, v: %s, start: %s, end: p.Pos, env: p.Env, cut: %s, errs: %s, ins: p.missingSince(%s)}",
				attemptType, a[0], a[1], a[2], a[3], a[4], a[5], a[6])
		case "opr.replay":
			// Outside the missing-token mode a candidate holds no insertion: its errors are recorded as
			// rec.add records them.
			return fmt.Sprintf("if p.inserting() {\np.replayMissing(%s.ins, %[1]s.errs, false)\n} else if !%[1]s.errs.none() {\np.recAdd(%[1]s.errs)\n}", a[0])
		case "rule.leader":
			return a[0] + ".Leader"
		case "rule.plain":
			return "len(" + a[0] + ".Scope) == 0"
		case "rule.docMemo":
			return fmt.Sprintf("(%s.DocMemo && (%[1]s.Leader || !p.insertsRule(%s)))", a[0], rule(a[0]))
		case "rule.docOnlyMemo":
			return fmt.Sprintf("(%s.DocMemo && !%[1]s.Memo && !%[1]s.Leader && !p.insertsRule(%s))", a[0], rule(a[0]))
		case "rule.positional":
			return a[0] + ".Positional"
		case "rule.id":
			return a[0] + ".Id"
		case "rule.seen":
			return a[0] + ".Seen"
		case "rule.invoke":
			return fmt.Sprintf("p.invoke(%s, %s)", a[0], a[1])
		case "rule.invokePlain":
			return fmt.Sprintf("p.invokePlainBody(%s, %s)", a[0], a[1])
		case "rule.run":
			return a[0] + "(p)"
		case "env.key":
			return "p.memoEnv(" + rule(a[0]) + ")"
		case "rec.mark":
			return "len(p.Recovered)"
		case "rec.truncate":
			return "p.Recovered = p.Recovered[:" + a[0] + "]"
		case "rec.since":
			return "p.recSince(" + a[0] + ")"
		case "rec.add":
			return "p.recAdd(" + a[0] + ")"
		case "rec.none":
			return a[0] + ".none()"
		case "exp.farthest":
			return "p.farthest"
		case "exp.recording":
			return "p.Expectations"
		case "exp.reset":
			return fmt.Sprintf("p.farthest = %s\np.exp = p.exp[:p.expBase]\np.expBits = 0", a[0])
		case "exp.fresh":
			return "len(p.exp) == p.expBase"
		case "exp.placeholder":
			return "p.exp = append(p.exp, ExpFar)"
		case "exp.maybe":
			return fmt.Sprintf("p.expBits&(uint64(1)<<(uint32(%s)*0x9e3779b1>>26)) != 0", a[0])
		case "exp.count":
			return "len(p.exp) - p.expBase"
		case "exp.at":
			return "p.exp[p.expBase+" + a[0] + "]"
		case "exp.add":
			return fmt.Sprintf("p.expBits |= uint64(1) << (uint32(%s) * 0x9e3779b1 >> 26)\np.exp = append(p.exp, %[1]s)", a[0])
		case "exp.setCount":
			return "len(" + a[0] + ")"
		case "exp.setAt":
			return a[0] + "[" + a[1] + "]"
		case "exp.isolate":
			return "p.Isolate(" + a[0] + ")"
		case "exp.unisolate":
			return "p.Unisolate(" + a[0] + ")"
		case "exp.empty":
			return "nil"
		case "exp.last":
			return "p.lastKept"
		case "exp.keptCount":
			return "len(" + a[0] + ")"
		case "exp.keptAt":
			return a[0] + "[" + a[1] + "]"
		case "memo.ok", "memo.silent":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "memo.")
		case "memo.far", "memo.end":
			// An entry keeps its positions as int32: no input is longer than the largest int32.
			return "int(" + a[0] + "." + strings.TrimPrefix(c.Callee, "memo.") + ")"
		case "memo.expected":
			// An entry keeps its slices as pointers and lengths (memoSide).
			return a[0] + ".exps()"
		case "memo.errs":
			return a[0] + ".recs()"
		case "memo.value":
			return a[0] + "." + valueField
		case "memo.setExpected":
			return fmt.Sprintf("%s.far = int32(%s)\n%s.setExps(%s)", a[0], a[1], a[0], a[2])
		case "obs.memoHit":
			return "p.Observation.memoHit()"
		case "obs.enter":
			return fmt.Sprintf("p.Observation.Enter(%s.Name, %s, %s)", a[0], a[1], a[2])
		case "obs.exit":
			return fmt.Sprintf("p.Observation.Exit(%s, %s, %s)", a[0], a[1], a[2])
		case "doc.examinedBegin":
			return "p.ExaminedBegin()"
		case "doc.examinedEnd":
			return "p.ExaminedEnd(" + a[0] + ")"
		case "doc.leave":
			// The binding level of a call is named min: the low end is lowered without the builtin.
			return fmt.Sprintf("p.docExamined = max(%s, p.docExamined)\nif %s < p.docLow {\np.docLow = %s\n}", a[0], a[1], a[1])
		case "doc.touchFrom":
			return "p.documentTouchFrom(" + a[0] + ")"
		case "doc.low":
			return "p.docLow"
		case "doc.move":
			if typed {
				// The typed library moves no copy of a value (typedMove).
				return "typedMove(" + a[0] + ")"
			}
			return "p.documentMove(" + a[0] + ")"
		case "memo.before":
			// The entry's position is the call's (reuse): a lookup applied the edits to the entry.
			return "int(" + a[0] + ".from) < start"
		case "memo.pin":
			return a[0] + ".pinned = true"
		// The edits that a lookup applies to an entry (memoAdvance), the same in both libraries.
		case "memo.seed":
			// The library keeps no seed of a growth in the memo.
			return "false"
		case "memo.from":
			return "int(" + a[0] + ".from)"
		case "memo.pinned":
			return a[0] + ".pinned"
		case "memo.shifted":
			if typed {
				// The typed library moves only basic values (typedMove), so a value has to move exactly
				// when the shift is not zero.
				return "(" + a[0] + ".shift != 0)"
			}
			return a[0] + ".shifted"
		case "memo.applied":
			return "int(" + a[0] + ".vgen)"
		case "memo.advance":
			s := fmt.Sprintf("%[1]s.end += int32(%[4]s)\n%[1]s.from, %[1]s.examined = int32(%[2]s), int32(%[3]s)\n%[1]s.far += int32(%[4]s)\n%[1]s.shift += int32(%[4]s)\n%[1]s.vgen = uint32(%[5]s)",
				a[0], a[1], a[2], a[3], a[5])
			if typed {
				// The shifted flag of the typed library is the shift's (memo.shifted): it does not record it.
				return s
			}
			return s + fmt.Sprintf("\n%s.shifted = %s", a[0], a[4])
		case "edit.count":
			return "len(" + a[0] + ")"
		case "edit.start":
			return a[0] + "[" + a[1] + "].start"
		case "edit.end":
			return a[0] + "[" + a[1] + "].end"
		case "edit.delta":
			return a[0] + "[" + a[1] + "].delta"
		case "obs.active":
			return "p.Observation != nil"
		case "doc.active":
			return "p.Document != nil"
		case "doc.tracking":
			return "p.track"
		case "doc.touch":
			return "p.documentTouch(" + a[0] + ")"
		case "doc.examined":
			return "p.docExamined"
		case "memo.examined":
			return "int(" + a[0] + ".examined)"
		case "doc.evaluated":
			return "p.Document.stats.Evaluated++"
		case "doc.reused":
			return "p.Document.stats.Reused++"
		case "memo.examine":
			// The library records the start of the examined input, the low end that before compares with
			// the position: a call examines at most the unit before its position, so the start is the
			// position or the unit before it, as in the engine.
			return fmt.Sprintf("%s.examined, %[1]s.from, %[1]s.pinned = int32(%s), int32(p.docLow), false", a[0], a[2])
		case "ctx.found":
			return a[0] + " != nil"
		case "ctx.ok":
			return a[0] + ".ok"
		case "ctx.value":
			return a[0] + "." + valueField
		case "ctx.end", "ctx.errs", "ctx.far", "ctx.expected", "ctx.examined":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "ctx.")
		case "ctx.before":
			return a[0] + ".from < " + a[1]
		case "ctx.allows":
			return fmt.Sprintf("%s.memoAllows(%s.present)", a[0], a[1])
		case "ctx.active":
			return a[0] + ".ctx"
		case "ctx.lookup":
			return fmt.Sprintf("p.ctxLookup(&%s, %s.Id, %s, %s)", a[0], a[1], a[2], a[3])
		case "ctx.push":
			return "p.ctxPush(&" + a[0] + ")"
		case "ctx.pop":
			return "p.ctxPop(&" + a[0] + ")"
		case "ctx.storeMemo":
			return "storeMemo"
		case "ctx.storeCtx":
			return "storeCtx"
		case "memo.setPresent":
			return fmt.Sprintf("%s.present |= %s.present", a[0], a[1])
		case "ctx.keep":
			return fmt.Sprintf("p.ctxKeep(&%s, %s.Id, %s, %s, %s, &ctxResult{%s: %s, ok: %s, end: %s, examined: p.examinedNow(), from: p.fromNow(), silent: %s, errs: %s, far: %s, expected: %s})",
				a[0], a[1], a[2], a[3], a[4], valueField, a[8], a[5], a[6], a[7], a[9], a[10], a[11])
		case "ctx.done":
			return "p.ctxDone(&" + a[0] + ")"
		case "depth.current":
			return "p.Depth"
		case "stream.emits":
			return "p.Stream != nil && p.Stream.Emit != nil"
		case "stream.start":
			return "p.Stream.start"
		case "stream.check":
			return "p.streamCheck()"
		case "stream.report":
			return "p.streamReport()"
		case "stream.deliver":
			return "p.streamDeliver(" + a[0] + ")"
		case "stream.discard":
			return "p.streamDiscard(" + a[0] + ")"
		case "stream.cutText":
			return "p.Stream.textFloor = " + a[0]
		case "stream.release":
			return "p.streamRelease()"
		case "memo.floor":
			return "p.Stream.memoBase"
		case "memo.prune":
			return "p.pruneStreamMemo(" + a[0] + ")"
		case "stream.dropRecords":
			return "p.streamDropRecords(" + a[0] + ")"
		case "stream.tail":
			return "p.Stream.tail.limit > 0"
		case "stream.settleErrors":
			return "p.streamSettleErrors()"
		case "grow.begin":
			return fmt.Sprintf("p.growBegin(%s, %s)", rule(a[0]), a[1])
		case "grow.ctx":
			return a[0] + ".ctx"
		case "grow.longer":
			return fmt.Sprintf("p.growLonger(&%s, %s)", a[0], a[1])
		case "grow.adopt":
			return fmt.Sprintf("p.growAdopt(&%s, %s, %s)", a[0], step(a[1]), a[2])
		case "grow.memoStep":
			return "p.growMemoStep(&" + a[0] + ")"
		case "grow.isolate":
			return "p.growIsolate(&" + a[0] + ")"
		case "grow.keepStep":
			return fmt.Sprintf("p.growKeepStep(&%s, %s, %s, %s)", a[0], step(a[1]), a[2], a[3])
		case "grow.grew":
			return fmt.Sprintf("p.growGrew(&%s, %s)", a[0], a[1])
		case "grow.adoptStep":
			return fmt.Sprintf("p.growAdoptStep(&%s, %s)", a[0], a[1])
		case "grow.end":
			return "p.growEnd(&" + a[0] + ")"
		}
		panic("core: no Go implementation of " + c.Callee)
	}
	t.Negate = func(c *code.Call, a []string) (string, bool) {
		switch c.Callee {
		case "opr.found":
			return a[0] + ".op == nil", true
		case "opr.hasSkip":
			return a[0] + ".OperatorsRule.Skip == nil", true
		case "opr.infix":
			return a[0] + `.Kind != "infix"`, true
		case "rec.none":
			return "!" + a[0] + ".none()", true
		case "memo.pinned":
			return "!" + a[0] + ".pinned", true
		}
		return "", false
	}
	t.Stmt = func(c *code.Call, a []string, dst []string, define bool) (string, bool) {
		op := " = "
		if define {
			op = " := "
		}
		key := func() string { return strings.Join(a[:4], ", ") }
		switch c.Callee {
		case "opr.skip":
			return fmt.Sprintf("_, %s%s%s.OperatorsRule.Skip(p)", dst[0], op, a[0]), true
		case "memo.lookup":
			s := dst[0] + op + "p.memoGet(" + key() + ", " + a[4] + ")"
			if dst[1] != "_" {
				s += "\n" + dst[1] + op + dst[0] + " != nil"
				// A Document applies its edits to an entry when the entry is found (advanceEntry).
				s += fmt.Sprintf("\nif %[2]s && %[1]s.vgen != p.gen && !p.advanceEntry(%[1]s, p.edits) {\n%[2]s = false\n}", dst[0], dst[1])
			}
			return s, true
		case "ctx.enter":
			// The context of a call of a rule of no cycle, or outside every growth, is empty.
			c, seed := dst[0], dst[1]
			s := ""
			if define {
				s = fmt.Sprintf("var %s ctxCallState\nvar %s *ctxResult\n", c, seed)
			}
			return s + fmt.Sprintf("if %s.Cycle != 0 && len(p.gamma) != 0 {\n%s = p.ctxEnter(&%s, %s, %s)\n}", a[0], seed, c, rule(a[0]), a[1]), true
		case "exp.copy":
			// The kept sets fill append-only chunks of the arena.
			return fmt.Sprintf(`if cap(p.arena)-len(p.arena) < len(%[1]s) {
// A new chunk; the previous one lives while sets in it remain.
p.arena = make([]ExpID, 0, max(1024, len(%[1]s)))
p.memUsed += uint64(cap(p.arena)) * expIDBytes
}
off := len(p.arena)
p.arena = appendSpend(p, p.arena, %[1]s)
p.lastKept = p.arena[off:len(p.arena):len(p.arena)]
%[2]s%[3]sp.lastKept`, a[0], dst[0], op), true
		case "doc.enter":
			return fmt.Sprintf("%s, %s%sp.docExamined, p.docLow\np.docExamined, p.docLow = %s, %s", dst[0], dst[1], op, a[0], a[0]), true
		case "memo.store":
			e := dst[0]
			fields := fmt.Sprintf("%s: %s, ok: %s, end: int32(%s), silent: %s", valueField, a[7], a[4], a[5], a[6])
			fields += ", vgen: p.gen"
			if !typed {
				// The nodes of the value hold positions of the current text (documentMove).
				fields += ", ngen: p.gen"
			}
			return fmt.Sprintf("%s%s%s\n*%s = %s{%s}\n%[4]s.setRecs(%[9]s)\n%[4]s = p.memoPut(%[7]s, %[8]s, %[4]s)", e, op, alloc, e, entryType, fields, key(), a[9], a[8]), true
		}
		return "", false
	}
	return t
}

// goRunOp returns the node library's Go form of an operation of the repetition runs of a Document,
// of the edit log they read and of the poll of a reused element, given its printed arguments, or
// reports false for another operation. The library's RunState holds the fields of the Run record as
// the engine's does (engineRunOp); the recorded elements are its own.
func goRunOp(callee string, a []string) (string, bool) {
	switch callee {
	case "doc.log":
		return "p.edits", true
	case "budget.poll":
		return "p.poll()", true
	}
	name, ok := strings.CutPrefix(callee, "run.")
	if !ok {
		return "", false
	}
	for _, f := range exec.RunFields() {
		field := strings.ToUpper(f.Name[:1]) + f.Name[1:]
		if name == f.Name {
			return a[0] + "." + field, true
		}
		if name == "set"+field {
			return a[0] + "." + field + " = " + a[1], true
		}
	}
	elem := map[string]string{"oldStart": "start", "oldEnd": "end", "oldFrom": "from", "oldExamined": "examined", "oldFar": "far", "oldKept": "exp"}
	if f, ok := elem[name]; ok {
		return a[0] + ".old[" + a[1] + "]." + f, true
	}
	switch name {
	case "init":
		return "p.runInit(" + strings.Join(a, ", ") + ")", true
	case "takeHere":
		return "p.runTakeHere(" + a[0] + ")", true
	case "previous":
		return "p.lastRuns != nil && p.gen > 0", true
	case "takeLast":
		return "p.runTakeLast(" + strings.Join(a, ", ") + ")", true
	case "oldCount":
		return "len(" + a[0] + ".old)", true
	case "take":
		return "p.runTake(" + strings.Join(a, ", ") + ")", true
	case "resumed":
		return "p.Document.stats.Reused++\np.Document.stats.Resumed++", true
	case "add":
		return "p.runAdd(" + strings.Join(a, ", ") + ")", true
	case "clean":
		return "p.runClean(" + a[0] + ")", true
	case "minimum":
		return "minRecordedRun", true
	case "record":
		return "p.runRecord(" + a[0] + ")", true
	}
	panic("core: no Go implementation of " + callee)
}
