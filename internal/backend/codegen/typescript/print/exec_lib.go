package print

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
)

// tsMethod is a printed method of the TypeScript library's Parser class.
type tsMethod struct {
	// Name is the method's name, Text its source, indented for the class body.
	Name, Text string
}

// tsSpecs are the methods of the TypeScript library that the compositions replace. The methods of the
// Parser class that run rule bodies have resumable forms too, which the StreamParser class, the parser
// of a stream, takes (tsGenMethods). A parser with limit controls counts logical compute units (exec.Units): the call
// protocol counts the calls, their keys, the steps of growths and the entries of recovered errors it
// takes. The batch Parser's callBody observes nothing: the parsers of observed and Document parses
// override call, and count the entry of the call there (Parser.enterCall). The methods of tsDocClass
// belong to the DocumentParser class, the parser of a Document parse: its call takes callDocument,
// which memoizes the rules that the Document memoizes and records the input that each call examined.
// The work of a Document is free, so its methods count only the entries of recovered errors that the
// memoized call takes. memoAdvance applies the pending edits of the Document to an entry that a lookup
// finds; advanceEntry, the same form, belongs to the DocumentMemo class, which applies the edits to
// the entries at an edit and to an entry that a store joins. callTracked, of tsObsClass, is the
// memoized call of the ObservedParser class, which records how far each call examined the input
// without a Document; callUnmemoized belongs to TrackedParser, the class of both.
var tsSpecs = []struct {
	fn     string
	on     []string
	name   string
	target tsClass
}{
	{fn: "call", on: []string{exec.Units}, name: "callBody"},
	{fn: "firstCall", name: "firstCall"},
	{fn: "document", on: []string{exec.Document}, name: "callDocument", target: tsDocClass},
	{fn: "unmemoized", name: "callUnmemoized", target: tsDocClass},
	{fn: "memoized", on: []string{exec.Document, exec.Observe, exec.Units}, name: "callMemo", target: tsDocClass},
	{fn: "memoized", on: []string{exec.Document, exec.Observe, exec.Units}, name: "callTracked", target: tsObsClass},
	{fn: "memoAdvance", on: []string{exec.Document}, name: "memoAdvance", target: tsDocClass},
	{fn: "memoAdvance", on: []string{exec.Document}, name: "advanceEntry", target: tsDocClass},
	// The repetition runs of a Document parse, which the rule bodies run through the resume methods of
	// DocumentParser (resumeStart and the others), the forms of the Flow operations of the same names.
	// runBegin and runEnd take other names, since runBegin is the walk of a run site.
	{fn: "runStart", on: tsRunOn, name: "runStart", target: tsDocClass},
	{fn: "runMore", on: tsRunOn, name: "runMore", target: tsDocClass},
	{fn: "runReuse", on: tsRunOn, name: "runReuse", target: tsDocClass},
	{fn: "runPrefix", on: tsRunOn, name: "runPrefix", target: tsDocClass},
	{fn: "runResync", on: tsRunOn, name: "runResync", target: tsDocClass},
	{fn: "runTail", on: tsRunOn, name: "runTail", target: tsDocClass},
	{fn: "runNext", on: tsRunOn, name: "runNext", target: tsDocClass},
	{fn: "runBegin", on: tsRunOn, name: "runElemBegin", target: tsDocClass},
	{fn: "runEnd", on: tsRunOn, name: "runElemEnd", target: tsDocClass},
	{fn: "runFinish", on: tsRunOn, name: "runFinish", target: tsDocClass},
	{fn: "grow", on: []string{exec.Units}, name: "grow"},
	// The invocation of a rule's body (rule.invoke and rule.invokePlain), whose units the caller counts.
	{fn: "invoke", name: "invoke"},
	{fn: "invokePlain", name: "invokePlainBody"},
	{fn: "expect", name: "expect"},
	{fn: "mergeKept", name: "mergeKept"},
	{fn: "mergeExpected", name: "mergeExpected"},
	{fn: "keep", name: "keep"},
	// The Operators Rule loop and backtracking.
	{fn: "operatorsParse", on: []string{exec.Units}, name: "operatorsRuleParse"},
	{fn: "operatorsExpr", on: []string{exec.Units}, name: "operatorsRuleExpr"},
	{fn: "operatorsNud", on: []string{exec.Units}, name: "operatorsRuleNud"},
	{fn: "longest", on: []string{exec.Units}, name: "longest"},
	{fn: "skipTrivia", name: "skipTrivia"},
	{fn: "reset", name: "reset"},
	// Predicates and bindings.
	{fn: "predicate", name: "predicate"},
	{fn: "bind", name: "bind"},
	// The operators of action expressions.
	{fn: "equal", name: "actionEqual"},
	{fn: "index", name: "actionIndex"},
	{fn: "binary", name: "actionBinary"},
	{fn: "unary", name: "actionUnary"},
	{fn: "logic", name: "actionLogic"},
	{fn: "member", name: "actionMember"},
	{fn: "push", name: "actionPush"},
	{fn: "pushItems", name: "actionPushItems"},
	{fn: "items", name: "actionItems"},
	{fn: "itemAt", name: "actionItemAt"},
	{fn: "filterKeep", name: "actionFilterKeep"},
	{fn: "length", name: "actionLength"},
	{fn: "text", name: "actionText"},
	{fn: "convert", name: "actionConvert"},
	{fn: "cover", name: "actionCover"},
	{fn: "made", name: "actionMade"},
	{fn: "hull", name: "actionHull"},
	{fn: "conforms", name: "fieldConforms"},
	{fn: "valueConforms", name: "valueConforms"},
	{fn: "elemsConform", name: "elemsConform"},
	{fn: "tupleConform", name: "tupleConform"},
}

// tsRunOn are the features of the methods of the repetition runs of a Document. Their work counts no
// units; under exec.Units a reused element is a step at which a cancellation is observed (budget.poll).
var tsRunOn = []string{exec.Document, exec.Units}

// tsClass selects the target that prints a method of the library (tsSpecs): tsTarget, tsDocTarget or
// tsObsTarget.
type tsClass int

const (
	tsBatchClass tsClass = iota
	tsDocClass
	tsObsClass
)

// specFeatures maps the features of the compositions that the library's methods are specialized
// for to the features of a parser: a parser with limit controls counts logical compute units.
var specFeatures = map[string]common.Feature{exec.Document: common.FeatureDocument, exec.Observe: common.FeatureObserve, exec.Units: common.FeatureBudget}

// LibraryRequests returns the specializations that the TypeScript library of a parser with the
// features fs requests of the compositions (library.Request), one per method of tsSpecs, in its
// order: each is specialized for the features of its specification that fs has, under the name of
// its method, which is unique in the library; the printer prints it under the name of its
// composition, which the calls of the other methods take.
func LibraryRequests(fs common.FeatureSet) ([]library.Request, error) {
	rs := make([]library.Request, len(tsSpecs))
	for i, s := range tsSpecs {
		var on []string
		for _, o := range s.on {
			f, ok := specFeatures[o]
			if !ok {
				return nil, fmt.Errorf("tslib: the method %s is specialized for %s, which no feature of a parser maps to", s.name, o)
			}
			if fs.Has(f) {
				on = append(on, o)
			}
		}
		rs[i] = library.Request{Fn: s.fn, On: on, Name: s.name}
	}
	return rs, nil
}

// tsMethodsFor returns the printed methods of the TypeScript library from lib, the specialized
// compositions of the requests of LibraryRequests.
func tsMethodsFor(lib *library.Library) ([]tsMethod, error) {
	methods, _, err := libraryFuncs(lib)
	if err != nil {
		return nil, err
	}
	if len(methods) != len(tsSpecs) {
		return nil, fmt.Errorf("tslib: the library holds no specialized method for each request of the TypeScript library")
	}
	m := lib.Module
	var out []tsMethod
	for i, s := range tsSpecs {
		if f := lib.Funcs[i]; lib.Requests[i].Fn != s.fn || f.Name != s.name {
			return nil, fmt.Errorf("tslib: the specialized function %d of the library is %s of %s, not the method %s", i, f.Name, lib.Requests[i].Fn, s.name)
		}
		// The method is printed from a copy named after its composition, whose name the printer maps
		// to the name that calls of the composition take (FuncName).
		f := *lib.Funcs[i]
		f.Name = s.fn
		t := tsTarget
		switch s.target {
		case tsDocClass:
			t = tsDocTarget
		case tsObsClass:
			t = tsObsTarget
		}
		src, err := Method(m, &f, t)
		if err != nil {
			return nil, err
		}
		if printed := t.FuncName(s.fn); printed != s.name {
			// The method is printed under the name that calls of fn take; this form has its own.
			head := "\n  " + printed + "("
			if strings.Count(src, head) != 1 {
				return nil, fmt.Errorf("tslib: the printed method %s has %d heads %q", s.name, strings.Count(src, head), head)
			}
			src = strings.Replace(src, head, "\n  "+s.name+"(", 1)
			// The comment of the method names it too.
			if doc := "  // " + printed + " "; strings.HasPrefix(src, doc) {
				src = "  // " + s.name + " " + src[len(doc):]
			}
		}
		out = append(out, tsMethod{Name: s.name, Text: src})
	}
	gens, err := tsGenMethods(lib)
	if err != nil {
		return nil, err
	}
	out = append(out, gens...)
	kind, name := tsMemberKind()
	out = append(out, tsMethod{Name: "actionError", Text: tsActionError()}, tsMethod{Name: "memberKind", Text: kind},
		tsMethod{Name: "memberName", Text: name})
	return out, nil
}

// tsResumeOps are the operations that run the body of a rule or the skip or a line of an Operators Rule,
// which suspend in the parse of a stream where they demand input that has not arrived: a method that
// reaches one of them has a resumable form.
var tsResumeOps = map[string]bool{"rule.invoke": true, "rule.invokePlain": true, "rule.body": true, "opr.match": true, "opr.skip": true}

// tsResuming returns the compositions of the methods of the library's Parser class (tsBatchClass) that
// reach the body of a rule (tsResumeOps), directly or through another such composition, specialized as
// the methods are: those with a resumable form. Only the parse of a stream, which is neither a Document
// nor an observed parse, takes the resumable forms, so the other classes have none.
func tsResuming(lib *library.Library) (map[string]bool, error) {
	calls := map[string][]string{}
	for i, s := range tsSpecs {
		if s.target != tsBatchClass {
			continue
		}
		if _, ok := calls[s.fn]; ok {
			return nil, fmt.Errorf("tslib: the composition %s has two methods in the Parser class", s.fn)
		}
		cs := []string{}
		code.Walk(lib.Funcs[i].Body, nil, func(e code.Expr) {
			if c, ok := e.(*code.Call); ok && c.Fn == nil {
				cs = append(cs, c.Callee)
			}
		})
		calls[s.fn] = cs
	}
	res := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for fn, cs := range calls {
			if res[fn] {
				continue
			}
			for _, c := range cs {
				if tsResumeOps[c] || res[c] {
					res[fn], changed = true, true
					break
				}
			}
		}
	}
	// A resumable method calls the resumable forms of the compositions that reach a body, which only
	// the methods of the Parser class have.
	for fn := range res {
		for _, c := range calls[fn] {
			if _, ok := calls[c]; !ok && lib.Module.Op(c) == nil {
				return nil, fmt.Errorf("tslib: the resumable method of %s calls %s, which is no method of the Parser class", fn, c)
			}
		}
	}
	return res, nil
}

// tsGenMethods returns the resumable forms of the methods of the library's Parser class that reach the
// body of a rule (tsResuming), printed from the same specialized compositions under the method's name
// with the suffix Gen as generator methods of the StreamParser class: each runs the resumable body of a
// rule (StreamParser.bodyGens) and the resumable forms of the methods it calls with yield*, and so
// yields the units of input that they lack; every other operation is the method's own.
func tsGenMethods(lib *library.Library) ([]tsMethod, error) {
	res, err := tsResuming(lib)
	if err != nil {
		return nil, err
	}
	t := *tsTarget
	t.Generator = true
	t.Yields = func(name string) bool { return res[name] }
	t.FuncName = func(name string) string {
		if res[name] {
			return tsTarget.FuncName(name) + "Gen"
		}
		return tsTarget.FuncName(name)
	}
	t.Op = func(c *code.Call, a []string) string {
		switch c.Callee {
		case "rule.invoke":
			return fmt.Sprintf("(yield* this.invokeGen(%s, %s))", a[0], a[1])
		case "rule.invokePlain":
			return fmt.Sprintf("(yield* this.invokePlainBodyGen(%s, %s))", a[0], a[1])
		case "rule.body":
			// The rules that a resumable form calls are those whose bodies suspend, which have one.
			return fmt.Sprintf("(yield* this.bodyGens[%s.id]!(this, %s))", a[0], a[1])
		case "opr.match":
			// A line that cannot suspend has no resumable form.
			l := "(" + a[0] + " as ResumableLine)"
			return fmt.Sprintf("(%s.mGen === undefined ? %s.m(this) : (yield* %s.mGen!(this)))", l, a[0], l)
		case "opr.skip":
			o := "(" + a[0] + ".operatorsRule! as ResumableOperatorsRule)"
			return fmt.Sprintf("(%s.skipGen === undefined ? %s.skip!(this) : (yield* %s.skipGen!(this))) !== undefined", o, o, o)
		}
		return tsTarget.Op(c, a)
	}
	var out []tsMethod
	for i, s := range tsSpecs {
		if s.target != tsBatchClass || !res[s.fn] {
			continue
		}
		f := *lib.Funcs[i]
		f.Name = s.fn
		f.Doc = strings.TrimSpace(f.Doc) + "\nThis is its resumable form, which yields the units of input that the bodies it runs lack."
		src, err := Method(lib.Module, &f, &t)
		if err != nil {
			return nil, err
		}
		out = append(out, tsMethod{Name: "*" + s.name + "Gen", Text: src})
	}
	return out, nil
}

// tsOpTable returns the TypeScript array of the names of the operators that the failure code indexes.
func tsOpTable(code int) string {
	list := action.BinaryOps
	switch code {
	case action.ErrUnary:
		list = action.UnaryOps
	case action.ErrLogical:
		list = action.LogicalOps
	}
	q := make([]string, len(list))
	for i, o := range list {
		q[i] = fmt.Sprintf("%q", o)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// tsMemberKind returns the methods that classify a member name by action.MemberKinds: memberKind returns
// its kind, and memberName the name of the field or capture that a member of that kind reads.
func tsMemberKind() (kind, name string) {
	var b strings.Builder
	b.WriteString("  // memberKind returns the kind of the member read name (action.MemberKinds).\n  memberKind(name: string): number {\n    switch (name) {\n")
	for code, k := range action.MemberKinds {
		if code != action.MemberCapture && k.Spelling != "" {
			fmt.Fprintf(&b, "      case %q:\n        return %d; // %s\n", k.Spelling, code, k.Name)
		}
	}
	b.WriteString("    }\n")
	k := action.MemberKinds[action.MemberCapture]
	fmt.Fprintf(&b, "    if (name.startsWith(%q)) {\n      return %d; // %s\n    }\n", k.Spelling, action.MemberCapture, k.Name)
	fmt.Fprintf(&b, "    return %d; // %s\n  }", action.MemberField, action.MemberKinds[action.MemberField].Name)
	return b.String(), fmt.Sprintf("  // memberName returns the name of the field or capture that the member read name of the kind reads.\n  memberName(kind: number, name: string): string {\n    return kind === %d ? name.slice(%d) : name;\n  }",
		action.MemberCapture, len(k.Spelling))
}

// tsActionError returns the method that makes the text of a failure of an action expression from action.ActionErrors.
func tsActionError() string {
	var b strings.Builder
	b.WriteString("  actionError(code: number, a: Value, b: Value, op: number, n: number): string {\n    switch (code) {\n")
	for code, e := range action.ActionErrors {
		var t strings.Builder
		for _, seg := range e.Segments() {
			switch seg.Hole {
			case "":
				t.WriteString(strings.NewReplacer("\\", "\\\\", "`", "\\`", "$", "\\$").Replace(seg.Text))
			case "a", "b":
				t.WriteString("${typeName(" + seg.Hole + ")}")
			case "op":
				t.WriteString("${" + tsOpTable(code) + "[op]}")
			case "name":
				t.WriteString("${b as string}")
			case "i":
				t.WriteString("${a}")
			case "n":
				t.WriteString("${n}")
			}
		}
		t.WriteString(e.Hint)
		fmt.Fprintf(&b, "      case %d: // %s\n        return `%s`;\n", code, e.Name, t.String())
	}
	b.WriteString("    }\n    throw new Error(\"no action expression failure\");\n  }")
	return b.String()
}

// tsIsolationParts are the parts of the mark that exp.isolate returns and exp.unisolate takes: the farthest
// position and the base of the record that the isolation set aside. The two operations print the mark as
// the locals of these parts, and tsTarget.Parts names them for the callers that hold such a mark.
var tsIsolationParts = []Part{{Suffix: "Far", Type: "number"}, {Suffix: "Base", Type: "number"}}

// tsTarget is the TypeScript implementation of the operations in the library's Parser class.
var tsTarget = &Target{
	Source: "internal/ir/code/exec/compose",
	Packed: exec.Value,
	Parts: map[string][]Part{
		// The set of an isolation is the range of the stack of the record that it ended.
		exec.Set: {{Suffix: "From", Type: "number"}, {Suffix: "To", Type: "number"}},
		// A backtracking point is the position, the environment, the length of the trail and the
		// number of recovered errors.
		exec.Backtrack: {{Suffix: "Pos", Type: "number"}, {Suffix: "Env", Type: "Env | null"}, {Suffix: "Trail", Type: "number"},
			{Suffix: "Rec", Type: "number"}},
		// The mark of an isolation is the farthest position and the base of the record that it set aside.
		exec.Mark: tsIsolationParts,
	},
	Handle: func(name string) string {
		return map[string]string{
			exec.Rule: "Rule", exec.Value: "R", exec.Entry: "MemoEntry | null", exec.Values: "Value[] | null",
			exec.Errs: "Errs", exec.Kept: "number[]", exec.ExpID: "number", exec.EditLog: "DocumentEdit[]",
			exec.Ctx: "CtxCall", exec.Res: "CtxResult | null", exec.Grow: "GrowState", exec.Run: "RunState",
			exec.Frame: "Frame", exec.Scope: "Env | null", exec.Operators: "OperatorsRuleOp[]", exec.Operator: "OperatorsRuleOp",
			exec.Line: "OperatorsRuleLine", exec.Attempt: "OperatorsRuleAttempt | null",
			action.Action: "Action", exec.Datum: "Value", action.Seq: "readonly (Node | null)[]",
			action.Obj: "Node", action.Name: "string", action.Str: "string", action.Act: "Actx", action.Fail: "Value", action.Elem: "Node | null",
			action.NodeType: "string", action.FType: "FieldType",
		}[name]
	},
	Zero: func(name string) string {
		// A set of expectations starts empty.
		return map[string]string{exec.Kept: "noExp"}[name]
	},
	FuncName: func(name string) string {
		for _, s := range tsSpecs {
			if s.fn == name {
				return s.name
			}
		}
		return ""
	},
	Reg: func(name string) string {
		return map[string]string{exec.Pos: "this.pos", exec.Silent: "this.silent", exec.Cut: "this.cut"}[name]
	},
	Negate: func(c *code.Call, a []string) (string, bool) {
		switch c.Callee {
		case "opr.found":
			return a[0] + " === null", true
		case "opr.hasSkip":
			return a[0] + ".operatorsRule!.skip === null", true
		case "opr.infix":
			return a[0] + `.kind !== "infix"`, true
		case "rec.none":
			return a[0] + " !== null", true
		}
		return "", false
	},
	Op: func(c *code.Call, a []string) string {
		switch c.Callee {
		case "val.kind":
			return "valKind(" + a[0] + ")"
		case "val.unbox":
			return "unboxValue(" + a[0] + ")"
		case "val.fromBool":
			return a[0]
		case "val.toBool":
			return "(" + a[0] + " as boolean)"
		case "val.fail":
			return fmt.Sprintf("this.actionError(%s, %s, %s, %s, %s)", a[0], a[1], a[2], a[3], a[4])
		case "int.add", "int.sub", "int.mul", "int.quo", "int.rem", "int.cmp", "int.index":
			return "int" + strings.ToUpper(c.Callee[4:5]) + c.Callee[5:] + "(" + strings.Join(a, ", ") + ")"
		case "int.neg":
			return "intNeg(" + a[0] + ")"
		case "int.zero":
			return "(" + a[0] + " === 0)"
		case "str.add", "str.cmp", "str.eq":
			return "this.str" + strings.ToUpper(c.Callee[4:5]) + c.Callee[5:] + "(" + strings.Join(a, ", ") + ")"
		case "seq.items":
			return "seqItems(" + a[0] + ")"
		case "seq.len":
			return a[0] + ".length"
		case "seq.at":
			return "unboxValue(" + a[0] + "[" + a[1] + "]!)"
		case "val.name", "val.fromInt":
			return a[0]
		case "node.elem":
			return a[0]
		case "val.box":
			return a[0] + ".box(" + a[1] + ")"
		case "node.hasText":
			return a[0] + ".terminal"
		case "node.width":
			return "(" + a[0] + ".end - " + a[0] + ".start)"
		case "node.count":
			return a[0] + ".children.length"
		case "node.rangeText":
			return "this.text(" + a[0] + ".start, " + a[0] + ".end)"
		case "str.len":
			return "this.unitLen(" + a[0] + " as string)"
		case "str.of":
			return "(" + a[0] + " as string)"
		case "str.empty":
			return `""`
		case "node.isNode":
			return "isValueNode(" + a[0] + ")"
		case "node.start", "node.end":
			return a[0] + "." + c.Callee[5:]
		case "node.value":
			return "this.nodeValue(" + a[0] + ")"
		case "node.isStruct":
			return "(this.g.structFields.get(" + a[0] + ".type) !== undefined)"
		case "node.setSpan":
			return fmt.Sprintf("setNodeSpan(%s, %s, %s)", a[0], a[1], a[2])
		case "act.made":
			return "this.created.push(" + a[1] + ")"
		case "act.push":
			return "this.kidStack.push(" + a[1] + ")"
		case "act.pushAll":
			return a[0] + ".pushAll(" + a[1] + ")"
		case "ftype.kind":
			return a[0] + ".kind.charCodeAt(0)"
		case "ftype.elem":
			return a[0] + ".elem!"
		case "ftype.count":
			return a[0] + ".alts.length"
		case "ftype.alt":
			return a[0] + ".alts[" + a[1] + "]!"
		case "node.shape":
			return "nodeShape(" + a[0] + ")"
		case "node.isType":
			return "(" + a[0] + ".type === " + a[1] + ".name)"
		case "node.isMissing":
			return "false"
		case "node.children":
			return a[0] + ".children"
		case "seq.step":
			// A step over the elements that a value of an action holds or that a cast checks is no
			// event: the action counts the elements it builds (units.items, units.since).
			return ""
		case "val.seq":
			return fmt.Sprintf("this.counted(new Node(%s ? typeTupleValue : typeListValue, \"\", 0, 0, \"\", %s, noFields, false, false), nodeBytes)", a[1], a[2])
		case "node.typeName":
			return a[0]
		case "node.hasBase":
			return "(nodeShape(" + a[0] + ") <= shapeList)"
		case "node.convert":
			return fmt.Sprintf("this.retyped(%s, %s)", a[0], a[1])
		case "depth.exit":
			return "this.depth--"
		case "frame.markPos", "frame.markEnv", "frame.markTrail", "frame.markRec":
			return a[0] + strings.TrimPrefix(c.Callee, "frame.mark")
		case "frame.current":
			return "this.frame"
		case "frame.use":
			return "this.frame = " + a[0]
		case "frame.set":
			return fmt.Sprintf("this.setCapture(%s, %s)", a[0], a[1])
		case "frame.close":
			return ""
		case "frame.open":
			if k, ok := c.Args[0].(*code.Const); ok && k.Value == int64(0) {
				// A frame of no slots is never written: every such frame is the library's one.
				return "emptyFrame"
			}
			return "this.newFrame(" + a[0] + ")"
		case "frame.trail":
			return "this.trail.length"
		case "frame.drop":
			return "this.trail.length = " + a[0]
		case "frame.slots":
			return a[0] + ".length"
		case "env.scope":
			return "this.env"
		case "env.restore":
			return "this.env = " + a[0]
		case "env.bind":
			return fmt.Sprintf("this.bindValue(%s, %s)", a[0], a[1])
		case "action.ctx":
			return "this.useCtx(this.frame, this.pos, this.pos, null, null, null)"
		case "action.created":
			return "this.created.length"
		case "action.dropCreated":
			return "this.dropAbove(this.created, " + a[0] + ")"
		case "action.kids":
			return "this.kidStack.length"
		case "action.dropKids":
			return "this.dropAbove(this.kidStack, " + a[0] + ")"
		case "opr.led", "opr.prefix":
			return a[0] + ".operatorsRule!." + strings.TrimPrefix(c.Callee, "opr.")
		case "opr.count":
			return a[0] + ".length"
		case "opr.at":
			return a[0] + "[" + a[1] + "]!"
		case "opr.operands":
			return a[0] + ".operatorsRule!.operands.length"
		case "opr.operand":
			return a[0] + ".operatorsRule!.operands[" + a[1] + "]!"
		case "opr.line", "opr.level":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "opr.")
		case "opr.infix", "opr.postfix":
			return fmt.Sprintf("%s.kind === %q", a[0], strings.TrimPrefix(c.Callee, "opr."))
		case "opr.right":
			return a[0] + `.assoc === "right"`
		case "opr.nonAssoc":
			return a[0] + `.assoc === "none"`
		case "opr.scope":
			return a[0] + ".scope.length"
		case "opr.hasSkip":
			return a[0] + ".operatorsRule!.skip !== null"
		case "opr.skip":
			return a[0] + ".operatorsRule!.skip!(this) !== undefined"
		case "opr.match":
			return a[0] + ".m(this)"
		case "opr.lineValue":
			return fmt.Sprintf("this.lineResult(%s, %s, %s, %s as Node | null, %s)", a[0], a[1], a[2], a[3], a[4])
		case "opr.build":
			return fmt.Sprintf("this.operatorsRuleBuild(%s, %s!, %s as Node | null, %s as Node | null)", a[0], a[1], a[2], a[3])
		case "opr.attempt":
			return fmt.Sprintf("{ op: %s, frame: %s, v: %s as Node | null, start: %s, end: this.pos, env: this.env, cut: %s, errs: %s }",
				a[0], a[1], a[2], a[3], a[4], a[5])
		case "opr.found":
			return a[0] + " !== null"
		case "opr.op", "opr.end", "opr.cut", "opr.env", "opr.errs":
			return a[0] + "!." + strings.TrimPrefix(c.Callee, "opr.")
		case "opr.setCutFailed":
			return "this.cutFailed = " + a[0]
		case "opr.cutFailed":
			return "this.cutFailed"
		case "opr.setNudBound":
			return "this.nudBound = " + a[0]
		case "opr.nudBound":
			return "this.nudBound"
		case "opr.setAfterEmpty":
			return "this.afterEmpty = " + a[0]
		case "opr.afterEmpty":
			return "this.afterEmpty"
		case "units.count":
			if k, ok := c.Args[0].(*code.Const); ok && k.Value == uint64(1) {
				return "this.units++"
			}
			// The counter is a number; a count of u64 prints as a bigint.
			return "this.units += Number(" + a[0] + ")"
		case "units.check":
			return "if (this.units > this.unitsCheck) {\n  this.pollUnits();\n}"
		case "units.key":
			return "this.keyUnits(" + a[0] + ")"
		case "rule.cyclic":
			return a[0] + ".rec"
		case "rec.entries":
			return "this.recovered.length - " + a[0]
		case "seen.defers":
			return "this.deferMemo"
		case "seen.first":
			return "this.seenFirst(" + a[0] + ", " + a[1] + ")"
		case "rule.slots":
			return a[0] + ".scope.length"
		case "rule.memo", "rule.leader", "rule.id", "rule.seen":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "rule.")
		case "rule.docOnlyMemo":
			return "false" // TypeScript memoizes the same rules in every parse: none is memoized by a Document alone
		case "rule.plain":
			return a[0] + ".scope.length === 0"
		case "rule.invoke":
			return fmt.Sprintf("this.invoke(%s, %s)", a[0], a[1])
		case "rule.invokePlain":
			return fmt.Sprintf("this.invokePlainBody(%s, %s)", a[0], a[1])
		case "rule.novalue":
			return a[0] + ".novalue"
		case "rule.body":
			return fmt.Sprintf("%s.body(this, %s)", a[0], a[1])
		case "rule.finish":
			return fmt.Sprintf("this.finish(%s, %s, %s, %s)", a[0], a[1], a[2], a[3])
		case "env.key":
			return "this.memoKey(" + a[0] + ")"
		case "rec.mark":
			return "this.recovered.length"
		case "rec.truncate":
			return "this.recovered.length = " + a[0]
		case "rec.since":
			return "this.recSince(" + a[0] + ")"
		case "rec.none":
			return a[0] + " === null"
		case "exp.farthest":
			return "this.farthest"
		case "exp.recording":
			return "this.expectations"
		case "exp.reset":
			return fmt.Sprintf("this.farthest = %s;\nthis.expTop = this.expBase", a[0])
		case "exp.fresh":
			return "this.expTop === this.expBase"
		case "exp.placeholder":
			return "this.exp[this.expTop++] = expFar"
		case "exp.maybe":
			return "true" // the record has no filter of its entries
		case "exp.count":
			return "this.expTop - this.expBase"
		case "exp.at":
			return "this.exp[this.expBase + " + a[0] + "]!"
		case "exp.add":
			return "this.exp[this.expTop++] = " + a[0]
		case "exp.keptCount":
			return a[0] + ".length"
		case "exp.keptAt":
			return a[0] + "[" + a[1] + "]!"
		case "exp.setCount":
			return fmt.Sprintf("%sTo - %sFrom", a[0], a[0])
		case "exp.setAt":
			return fmt.Sprintf("this.exp[%sFrom + %s]!", a[0], a[1])
		case "exp.empty":
			return "noExp"
		case "exp.last":
			return "this.lastKept"
		case "memo.far", "memo.expected", "memo.ok", "memo.end", "memo.silent", "memo.errs":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "memo.")
		case "memo.value":
			return a[0] + ".node"
		case "memo.setExpected":
			return fmt.Sprintf("%s.far = %s;\n%s.expected = %s", a[0], a[1], a[0], a[2])
		case "obs.memoHit":
			return "this.observation?.memoHit()"
		case "ctx.found":
			return a[0] + " !== null"
		case "ctx.ok":
			return a[0] + "!.ok"
		case "ctx.value":
			return a[0] + "!.node"
		case "ctx.end", "ctx.errs", "ctx.far", "ctx.expected":
			return a[0] + "!." + strings.TrimPrefix(c.Callee, "ctx.")
		case "ctx.examined":
			return a[0] + "!.examined"
		case "ctx.before":
			return a[0] + "!.from < " + a[1]
		case "ctx.allows":
			return fmt.Sprintf("memoAllows(%s, %s.present)", a[0], a[1])
		case "ctx.active":
			return a[0] + ".ctx"
		case "ctx.lookup":
			return fmt.Sprintf("this.ctxLookup(%s, %s.id, %s, %s)", a[0], a[1], a[2], a[3])
		case "ctx.push", "ctx.pop", "ctx.done":
			return fmt.Sprintf("this.%s(%s)", map[string]string{"ctx.push": "ctxPush", "ctx.pop": "ctxPop", "ctx.done": "ctxDone"}[c.Callee], a[0])
		case "ctx.storeMemo":
			return "storeMemo"
		case "ctx.storeCtx":
			return "storeCtx"
		case "memo.setPresent":
			return fmt.Sprintf("%s.present |= %s.present", a[0], a[1])
		case "ctx.keep":
			return fmt.Sprintf("this.ctxKeep(%s, %s.id, %s, %s, %s, newCtxResult(%s, %s, %s, %s, %s, %s, %s, this.examinedNow(), this.fromNow()))",
				a[0], a[1], a[2], a[3], a[4], a[8], a[5], a[6], a[7], a[9], a[10], a[11])
		case "grow.begin":
			return fmt.Sprintf("this.growBegin(%s, %s)", a[0], a[1])
		case "grow.ctx":
			return a[0] + ".ctx"
		case "grow.longer":
			return fmt.Sprintf("this.growLonger(%s, %s)", a[0], a[1])
		case "grow.adopt":
			return fmt.Sprintf("this.growAdopt(%s, %s, %s)", a[0], a[1], a[2])
		case "grow.memoStep":
			return "this.growMemoStep(" + a[0] + ")"
		case "grow.isolate":
			return "this.growIsolate(" + a[0] + ")"
		case "grow.keepStep":
			return fmt.Sprintf("this.growKeepStep(%s, %s, %s, %s)", a[0], a[1], a[2], a[3])
		case "grow.grew":
			return fmt.Sprintf("this.growGrew(%s, %s)", a[0], a[1])
		case "grow.adoptStep":
			return fmt.Sprintf("this.growAdoptStep(%s, %s)", a[0], a[1])
		case "grow.end":
			return "this.growEnd(" + a[0] + ")"
		}
		panic("core: no TypeScript implementation of " + c.Callee)
	},
	Stmt: func(c *code.Call, a []string, dst []string, decl string) (string, bool) {
		lhs := func(name string) string {
			if decl != "" {
				return decl + " " + name
			}
			return name
		}
		key := func() string { return strings.Join(a[:4], ", ") }
		entry := func(e, node, ok, end, silent, errs string) string {
			return fmt.Sprintf("%s = this.memoPut(%s, newEntry(%s, %s, %s, %s, %s), %s);", lhs(e), key(), node, ok, end, silent, errs, a[9])
		}
		// absent prints an operation whose result is undefined where it has none as its two results.
		absent := func(call string) string {
			if dst[1] == "_" {
				return fmt.Sprintf("%s = %s ?? null;", lhs(dst[0]), call)
			}
			h := dst[0] + "Held"
			return fmt.Sprintf("const %[1]s = %[2]s;\n%[3]s = %[1]s !== undefined;\n%[4]s = %[1]s ?? null;", h, call, lhs(dst[1]), lhs(dst[0]))
		}
		switch c.Callee {
		case "node.of":
			var lines []string
			if dst[0] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s as Node;", lhs(dst[0]), a[0]))
			}
			if dst[1] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s instanceof Node;", lhs(dst[1]), a[0]))
			}
			return strings.Join(lines, "\n"), true
		case "node.heldText":
			var lines []string
			if dst[0] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s.text;", lhs(dst[0]), a[0]))
			}
			if dst[1] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s.terminal;", lhs(dst[1]), a[0]))
			}
			return strings.Join(lines, "\n"), true
		case "seq.span":
			var lines []string
			if dst[0] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s[%s]?.start ?? 0;", lhs(dst[0]), a[1], a[2]))
			}
			if dst[1] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s[%s]?.end ?? 0;", lhs(dst[1]), a[1], a[2]))
			}
			if dst[2] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s[%s] !== null;", lhs(dst[2]), a[1], a[2]))
			}
			return strings.Join(lines, "\n"), true
		case "node.span", "act.span":
			var lines []string
			if dst[0] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s.start;", lhs(dst[0]), a[0]))
			}
			if dst[1] != "_" {
				lines = append(lines, fmt.Sprintf("%s = %s.end;", lhs(dst[1]), a[0]))
			}
			return strings.Join(lines, "\n"), true
		case "node.field":
			return absent(fmt.Sprintf("lookupField(%s, %s)", a[0], a[1])), true
		case "node.zero":
			return absent(fmt.Sprintf("%s.memberZero(%s, %s)", a[0], a[1], a[2])), true
		case "depth.enter":
			return "if (++this.depth > this.maxDepth) {\n  this.tooDeep();\n}", true
		case "frame.mark":
			m := dst[0]
			return fmt.Sprintf("%s %sPos = this.pos;\n%[1]s %[2]sEnv = this.env;\n%[1]s %[2]sTrail = this.trail.length;\n%[1]s %[2]sRec = this.recovered.length;", decl, m), true
		case "frame.undo":
			return fmt.Sprintf("for (let t = this.trail.length - 1; t >= %s; t--) {\n  const u = this.trail[t]!;\n  u.f[u.slot] = u.old;\n}\nthis.trail.length = %[1]s;", a[0]), true
		case "frame.save":
			return fmt.Sprintf("this.saved.length = 0;\nfor (const x of %s) {\n  this.saved.push(x);\n}", a[0]), true
		case "frame.restore":
			return fmt.Sprintf("for (let t = 0; t < %s.length; t++) {\n  %[1]s[t] = this.saved[t]!;\n}", a[0]), true
		case "memo.lookup":
			s := fmt.Sprintf("%s = this.memoGet(%s, %s);", lhs(dst[0]), key(), a[4])
			if dst[1] != "_" {
				s += fmt.Sprintf("\n%s = %s !== null;", lhs(dst[1]), dst[0])
			}
			return s, true
		case "memo.store":
			return entry(dst[0], a[7], a[4], a[5], a[6], a[8]), true
		case "ctx.enter":
			// The context of a call of a rule of no cycle, or outside every growth, is empty.
			c, seed := dst[0], dst[1]
			return fmt.Sprintf("let %s = noCtx;\nlet %s: CtxResult | null = null;\nif (%s.cycle !== 0 && this.gamma.length !== 0) {\n  %s = newCtxCall();\n  %s = this.ctxEnter(%s, %s, %s);\n}",
				c, seed, a[0], c, seed, c, a[0], a[1]), true
		case "rec.add":
			return "this.recAdd(" + a[0] + ");", true
		case "exp.copy":
			return fmt.Sprintf("this.lastKept = this.exp.slice(%[1]sFrom, %[1]sTo);\n%[2]s = this.lastKept;", a[0], lhs(dst[0])), true
		case "exp.isolate":
			m := dst[0]
			return fmt.Sprintf("const %s%s = this.farthest;\nconst %s%s = this.expBase;\nthis.expBase = this.expTop;\nthis.farthest = %s;",
				m, tsIsolationParts[0].Suffix, m, tsIsolationParts[1].Suffix, a[0]), true
		case "exp.unisolate":
			far, set, m := dst[0], dst[1], a[0]
			return fmt.Sprintf("const %s = this.farthest;\nconst %sFrom = this.expBase;\nconst %sTo = this.expTop;\n"+
				"this.expTop = this.expBase;\nthis.expBase = %s%s;\nthis.farthest = %s%s;",
				far, set, set, m, tsIsolationParts[1].Suffix, m, tsIsolationParts[0].Suffix), true
		}
		return "", false
	},
}

// tsDocTarget is tsTarget in the methods of the DocumentParser class, which read the Document
// bookkeeping of the parser and of the rules.
var tsDocTarget = func() *Target {
	t := *tsTarget
	t.Op = func(c *code.Call, a []string) string {
		switch c.Callee {
		case "doc.active":
			return "true"
		case "doc.tracking":
			// These functions serve Document parses, which track the examined input.
			return "true"
		case "rule.docMemo", "rule.positional":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "rule.")
		case "rule.docOnlyMemo":
			return "(" + a[0] + ".docMemo && !" + a[0] + ".memo && !" + a[0] + ".leader)"
		case "doc.touch":
			return "this.documentTouch(" + a[0] + ")"
		case "doc.touchFrom":
			return "this.documentTouchFrom(" + a[0] + ")"
		case "doc.examined":
			return "this.docExamined"
		case "doc.low":
			return "this.docLow"
		case "doc.reused":
			return "this.reused++"
		case "doc.evaluated":
			return "this.evaluated++"
		case "doc.examinedBegin":
			return "this.docExaminedBegin()"
		case "doc.examinedEnd":
			return "this.docExaminedEnd(" + a[0] + ")"
		case "doc.move":
			return "this.documentMove(" + a[0] + ")"
		case "memo.examined":
			return a[0] + "!.examined"
		case "memo.before":
			return a[0] + "!.from < " + a[0] + "!.pos"
		case "memo.pin":
			return a[0] + ".pinned = true"
		case "memo.examine":
			// The entry records the start of the examined input, the low end that memo.before compares
			// with its position: a call examines at most the unit before its position, so the start is
			// the position or the unit before it.
			return fmt.Sprintf("%[1]s.examined = %[2]s;\n%[1]s.from = this.docLow;\n%[1]s.pinned = false", a[0], a[2])
		case "memo.seed":
			// The memo of a Document holds no seed: a growth keeps its seed in its own state.
			return "false"
		case "memo.from", "memo.pinned", "memo.errs":
			// The functions that take an entry as a parameter (memoAdvance) read it without a test.
			return a[0] + "!." + strings.TrimPrefix(c.Callee, "memo.")
		case "memo.shifted":
			// The value of an entry moves by the sum of the changes of length that moved the entry
			// (MemoEntry.shift), so it has to move when that sum is not zero.
			return a[0] + "!.shift !== 0"
		case "memo.applied":
			return a[0] + "!.vgen"
		case "memo.advance":
			// The shifted argument is not stored: memo.shifted reads it from the shift of the value.
			return fmt.Sprintf("%[1]s!.pos += %[4]s;\n%[1]s!.end += %[4]s;\n%[1]s!.from = %[2]s;\n%[1]s!.examined = %[3]s;\n%[1]s!.far += %[4]s;\n%[1]s!.shift += %[4]s;\n%[1]s!.vgen = %[5]s",
				a[0], a[1], a[2], a[3], a[5])
		case "edit.count":
			return a[0] + ".length"
		case "edit.start", "edit.end", "edit.delta":
			return a[0] + "[" + a[1] + "]!." + strings.TrimPrefix(c.Callee, "edit.")
		}
		if s, ok := tsRunOp(c.Callee, a); ok {
			return s
		}
		return tsTarget.Op(c, a)
	}
	t.Stmt = func(c *code.Call, a []string, dst []string, decl string) (string, bool) {
		switch c.Callee {
		case "doc.enter":
			return fmt.Sprintf("%[1]s %[2]s = this.docExamined;\n%[1]s %[3]s = this.docLow;\nthis.docExamined = %[4]s;\nthis.docLow = %[4]s;", decl, dst[0], dst[1], a[0]), true
		case "doc.leave":
			return fmt.Sprintf("this.docExamined = Math.max(%s, this.docExamined);\nif (%s < this.docLow) {\n  this.docLow = %[2]s;\n}", a[0], a[1]), true
		case "memo.lookup":
			// A lookup applies the pending edits of the Document to the entry it finds (memoAdvance):
			// one that cannot take them is not found.
			e, found := dst[0], dst[1]
			if found == "_" {
				panic("tsgen: the Document's memo.lookup needs its found result")
			}
			s, _ := tsTarget.Stmt(c, a, []string{e, "_"}, decl)
			lhs := found
			if decl != "" {
				lhs = decl + " " + found
			}
			return s + fmt.Sprintf("\n%s = %s !== null && (%[2]s.vgen === this.memoState.edits.length || this.memoAdvance(%[2]s, this.memoState.edits));", lhs, e), true
		}
		return tsTarget.Stmt(c, a, dst, decl)
	}
	return &t
}()

// tsObsTarget is tsDocTarget in the methods of the ObservedParser class, the parser of an observed
// parse without a Document: it tracks the examined end of each call, but has no Document bookkeeping.
var tsObsTarget = func() *Target {
	t := *tsDocTarget
	t.Op = func(c *code.Call, a []string) string {
		switch c.Callee {
		case "doc.active":
			return "false"
		case "memo.examine":
			return fmt.Sprintf("%s.examined = %s", a[0], a[2])
		case "doc.touchFrom", "doc.low", "doc.reused", "doc.evaluated", "doc.move", "memo.before", "memo.pin",
			"memo.seed", "memo.from", "memo.pinned", "memo.shifted", "memo.applied", "memo.advance",
			"edit.count", "edit.start", "edit.end", "edit.delta":
			panic("tsgen: the Document operation " + c.Callee + " in a method of an observed parse")
		}
		return tsDocTarget.Op(c, a)
	}
	// An observed parse without a Document has no edits to apply to the entries it finds.
	t.Stmt = func(c *code.Call, a []string, dst []string, decl string) (string, bool) {
		if c.Callee == "memo.lookup" {
			return tsTarget.Stmt(c, a, dst, decl)
		}
		return tsDocTarget.Stmt(c, a, dst, decl)
	}
	return &t
}()

// TSActionMethod returns the name of the method of the TypeScript library's Parser class that the

// tsActionMethod returns the name of the method of the TypeScript library's Parser class that the
// composition fn is printed as, for printing calls of the compositions outside the library (lowered
// action expressions).
func tsActionMethod(fn string) string { return tsTarget.FuncName(fn) }

// tsHandle returns the TypeScript type of the handle type name in the TypeScript library.
func tsHandle(name string) string { return tsTarget.Handle(name) }

// tsRunOp returns the TypeScript form, in the methods of DocumentParser, of an operation of the
// repetition runs of a Document and of the edit log they read, given its printed arguments, or
// reports false for another operation. RunState holds the fields of the Run
// record under their names (exec.RunFields), its mark as the parts of an isolation (tsIsolationParts);
// the recorded elements are the library's own.
func tsRunOp(callee string, a []string) (string, bool) {
	switch callee {
	case "doc.log":
		return "this.memoState.edits", true
	case "budget.poll":
		// A reused element counts no unit, but it is a step at which a cancellation is observed.
		return "this.control?.step()", true
	}
	name, ok := strings.CutPrefix(callee, "run.")
	if !ok {
		return "", false
	}
	for _, f := range exec.RunFields() {
		field := strings.ToUpper(f.Name[:1]) + f.Name[1:]
		switch {
		case name == f.Name && f.Name == "mark":
			// The mark is read where exp.unisolate takes it, which reads its parts.
			return a[0] + ".mark", true
		case name == "set"+field && f.Name == "mark":
			return fmt.Sprintf("%[1]s.mark%[3]s = %[2]s%[3]s;\n%[1]s.mark%[4]s = %[2]s%[4]s", a[0], a[1], tsIsolationParts[0].Suffix, tsIsolationParts[1].Suffix), true
		case name == f.Name:
			return a[0] + "." + f.Name, true
		case name == "set"+field:
			return a[0] + "." + f.Name + " = " + a[1], true
		}
	}
	elem := map[string]string{"oldStart": "start", "oldEnd": "end", "oldFrom": "from", "oldExamined": "examined", "oldFar": "far", "oldKept": "exp"}
	if f, ok := elem[name]; ok {
		return a[0] + ".old[" + a[1] + "]!." + f, true
	}
	switch name {
	case "init":
		return "this.runInit(" + strings.Join(a, ", ") + ")", true
	case "takeHere":
		return "this.runTakeHere(" + a[0] + ")", true
	case "previous":
		return "(this.lastRuns !== null && this.gen > 0)", true
	case "takeLast":
		return "this.runTakeLast(" + strings.Join(a, ", ") + ")", true
	case "oldCount":
		return a[0] + ".old.length", true
	case "take":
		return "this.runTake(" + strings.Join(a, ", ") + ")", true
	case "resumed":
		return "this.reused++;\nthis.resumed++", true
	case "add":
		return "this.runAdd(" + strings.Join(a, ", ") + ")", true
	case "clean":
		return "this.runClean(" + a[0] + ")", true
	case "minimum":
		return "minRecordedRun", true
	case "record":
		return "this.runRecord(" + a[0] + ")", true
	}
	panic("tsgen: no TypeScript form of " + callee)
}
