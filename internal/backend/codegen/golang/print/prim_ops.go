package print

import (
	"fmt"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The Go forms of the primitive operations of grammar code at the Exec stage. The name of a primitive
// (package prim) selects its form; the immediate holds its static data and args its printed arguments. A primitive without results prints as a statement,
// without its final newline.

// op returns the Go text of the call c of a primitive operation with the printed arguments args.
func (p *primPrinter) op(c *code.Call, args []string) string {
	g := p.g
	switch name := c.Callee; name {
	// Statements.
	case prim.OpStep:
		// Only the lowering of logical units steps (lowerdata.Options.Logical): the parser charges no
		// budget.
		return logicalPoll
	case prim.OpSilent:
		s := c.Imm.(prim.Silence)
		if s.Delta > 0 {
			if s.Site != 0 {
				return fmt.Sprintf("p.Silent++\np.LookBegin(%d)", s.Site)
			}
			return "p.Silent++"
		}
		if s.Site != 0 {
			return fmt.Sprintf("p.LookEnd(%t)\np.Silent--", s.Ok)
		}
		return "p.Silent--"
	case prim.OpAdvance:
		if n, ok := c.Args[0].(*code.Const); ok {
			if g.Logical {
				return advance(int(n.Value.(int64))) + "\np.Units += " + strconv.Itoa(int(n.Value.(int64)))
			}
			return advance(int(n.Value.(int64)))
		}
		if g.Logical {
			return "p.Pos += " + args[0] + "\np.Units += uint64(" + args[0] + ")"
		}
		return "p.Pos += " + args[0]
	case prim.OpExpect:
		x := c.Imm.(prim.Expectation)
		if x.Message {
			return fmt.Sprintf("p.%s(%s, MsgBit|%d)", p.expectHelper(), args[0], p.desc(x.Exp))
		}
		return fmt.Sprintf("p.%s(%s, %s)", p.expectHelper(), args[0], p.exp(x.Exp))
	case prim.OpRestore:
		return fmt.Sprintf("p.Restore(%s, %s)", args[0], args[1])
	case prim.OpMergeExpected:
		return fmt.Sprintf("p.MergeExpected(%s, %s)", args[0], args[1])
	case prim.OpMergeRenamed:
		return fmt.Sprintf("p.MergeExpected(%s, RenamedExpected(%s, %s, %d))", args[0], p.matcher(), args[1], p.desc(c.Imm.(prim.ExpID)))
	case prim.OpPushKid:
		if p.frame {
			return fmt.Sprintf("p.KidStack = append(p.KidStack, %s)", args[0])
		}
		return fmt.Sprintf("p.PushKid(%s)", args[0])
	case prim.OpStreamStart:
		return "p.StreamStart()"
	case prim.OpSkipBegin:
		return fmt.Sprintf("p.SkipBegin(%d)", c.Imm.(prim.Site))
	case prim.OpRunBegin:
		w := c.Imm.(prim.RunWalk)
		return fmt.Sprintf("p.RunBegin(%d, %t, %t)", w.Site, w.Build, w.KeyEnv)
	case prim.OpSkipVisit:
		return "p.SkipVisit()"
	case prim.OpSkipValue:
		return fmt.Sprintf("p.SkipValue(%s)", args[0])
	case prim.OpSkipEnd:
		return "p.SkipEnd()"
	case prim.OpRunWalked:
		// A walk shorter than RunHotSpan changes nothing, so the call is left out.
		return fmt.Sprintf("if p.Pos-%s >= RunHotSpan {\np.RunWalked(%d, %s)\n}", args[0], c.Imm.(prim.Site), args[0])
	case prim.OpScanEnd:
		return fmt.Sprintf("p.ScanEnd(%d, %s, %s)", c.Imm.(prim.Site), args[0], args[1])
	case prim.OpResumeBegin:
		return fmt.Sprintf("p.ResumeBegin(%s)", args[0])
	case prim.OpResumeEnd:
		return fmt.Sprintf("p.ResumeEnd(%s, %s, %s)", args[0], args[1], args[2])
	case prim.OpResumeFinish:
		return fmt.Sprintf("p.ResumeFinish(%s)", args[0])
	case prim.OpEmitStream:
		return fmt.Sprintf("p.EmitStream(%s, p.Pos)", args[0])
	case prim.OpDropKids:
		return fmt.Sprintf("p.DropKids(%s)", args[0])
	case prim.OpSetWhere:
		return fmt.Sprintf("p.Where = %q", string(c.Imm.(prim.Chars)))
	case prim.OpDropCreated:
		return fmt.Sprintf("p.DropCreated(%s.Cbase)", args[0])
	case prim.OpUnfresh:
		return fmt.Sprintf("if n := AsTval(%s); n != nil {\nn.TSetFresh(false)\n}", args[0])
	case prim.OpReset:
		return fmt.Sprintf("p.Reset(%s)", args[0])
	case prim.OpSetCapture:
		return fmt.Sprintf("p.%s(%d, %s)", g.helper("SetCapture"), c.Imm.(prim.Slot), args[0])
	case prim.OpClearVals:
		return fmt.Sprintf("clear(%s.Vals)", args[0])
	case prim.OpDropTrail:
		return fmt.Sprintf("p.DropTrail(%s.Trail)", args[0])
	case prim.OpFreeFrame:
		return fmt.Sprintf("p.FreeFrame(%s)", args[0])
	case prim.OpAppendError:
		return fmt.Sprintf("p.PushRecovered(%s)", args[0])
	case prim.OpDeclareNames:
		// Declared whatever the reachability.
		n := c.Imm.(*prim.Names)
		p.scopeNames(n)
		p.declare(n)
		return ""
	case prim.OpObserveSkip:
		return g.traceSkip(c.Imm.(prim.ObservedSkip))
	case prim.OpSkippedCalls:
		return p.skippedCalls(c.Imm.(prim.SkippedCalls))
	case prim.OpSetEnv:
		return "p.Env = " + args[0]
	case prim.OpSetFrame:
		return "p.Frame = " + args[0]
	case prim.OpAppend:
		return p.appendText(c, args)

	// Expressions.
	case prim.OpEnv:
		return "p.Env"
	case prim.OpFrame:
		return "p.Frame"
	case prim.OpRecLen:
		return "len(p.Recovered)"
	case prim.OpKidsLen:
		return "len(p.KidStack)"
	case prim.OpStreamOn:
		return fmt.Sprintf("p.StreamsIn(%d, %v)", g.Cur.ID, g.Cur.Rec)
	case prim.OpSkipHit:
		return "p.SkipHit()"
	case prim.OpRunHot:
		return fmt.Sprintf("p.RunHot(%d)", c.Imm.(prim.Site))
	case prim.OpSkipKids:
		return "p.SkipKids()"
	case prim.OpScanReach:
		return fmt.Sprintf("p.ScanReach(%d, %s, %s)", c.Imm.(prim.Site), args[0], args[1])
	case prim.OpScanKids:
		return fmt.Sprintf("p.ScanKids(%d, %s, %s, %s)", c.Imm.(prim.Site), args[0], args[1], args[2])
	case prim.OpResumeOn:
		return "p.ResumeOn()"
	case prim.OpInDocument:
		return "(p.Document != nil)" // parenthesized: a guard negates it
	case prim.OpResumeStart:
		x := c.Imm.(prim.Resume)
		return fmt.Sprintf("p.ResumeStart(%d, %d, %d, %t)", x.Site, x.Min, x.Max, x.Shiftable)
	case prim.OpResumeMore:
		return fmt.Sprintf("p.ResumeMore(%s)", args[0])
	case prim.OpResumeNext:
		return fmt.Sprintf("p.ResumeNext(%s)", args[0])
	case prim.OpHasLit:
		return p.hasLit(c.Imm.(prim.Lit))
	case prim.OpMatchLit:
		lit := c.Imm.(prim.Lit)
		if lit.Fold {
			return fmt.Sprintf("%s.%s(%s, %d, false)", p.matcher(), p.helper("MatchLiteralFold"), g.literal(string(common.FoldedRunes([]rune(lit.Text)))), p.desc(lit.Exp))
		}
		name := g.literal(lit.Text)
		var desc rprogram.ExpID
		if lit.Text == "" {
			desc = g.Desc(core.LiteralDesc(&ast.Literal{}))
		} else {
			desc = p.desc(lit.Exp)
		}
		return fmt.Sprintf("%s.%s(%s, %q, %d, false)", p.matcher(), p.helper("MatchLiteral"), name, lit.Text, desc)
	case prim.OpGuardFails:
		guard := c.Imm.(prim.Guard)
		cond := NegateCondition(classReject(grammarClass(p.class(guard.Class))))
		return fmt.Sprintf("!(%s && (%s)) && %s", args[1], chIdent.ReplaceAllString(cond, args[0]), depthLimit(guard.Depth))
	case prim.OpGuardLitFails:
		guard := c.Imm.(prim.Guard)
		return fmt.Sprintf("!p.%s(%d, %t) && %s", g.helper("HasChar"), guard.Rune, guard.Fold, depthLimit(guard.Depth))
	case prim.OpCallRule:
		x := c.Imm.(prim.RuleCall)
		return g.ruleCall(g.RuleOf(x.Rule), x.Level)
	case prim.OpIsolate:
		return "p.Isolate(p.Pos)"
	case prim.OpUnisolate:
		return fmt.Sprintf("p.Unisolate(%s)", args[0])
	case prim.OpText:
		return fmt.Sprintf("p.Text(%s, p.Pos)", args[0])
	case prim.OpLiteralText:
		return fmt.Sprintf("p.LiteralText(%s, %q)", args[0], string(c.Imm.(prim.Chars)))
	case prim.OpStr:
		return strconv.Quote(string(c.Imm.(prim.Chars)))
	case prim.OpNewMatch:
		return fmt.Sprintf("p.%s(%s, p.Pos, %s, true)", g.helper("NewMatch"), args[0], args[1])
	case prim.OpNewTerminalNode:
		return fmt.Sprintf("p.%s(%s, %s)", g.helper("NewTerminal"), args[0], args[1])
	case prim.OpNewBranchNode:
		kind := "SeqKind"
		if c.Imm.(prim.List) {
			kind = "ListKind"
		}
		return fmt.Sprintf("p.%s(%s, %s, %s)", g.helper("NewBranch"), kind, args[0], args[1])
	case prim.OpNewNode:
		typ := "tuple"
		if c.Imm.(prim.List) {
			typ = valueimpl.TypeList
		}
		return fmt.Sprintf("p.%s(%q, %s, p.Pos, %s)", g.helper("NewNode"), typ, args[0], args[1])
	case prim.OpNewVals:
		if p.untyped() {
			return fmt.Sprintf("p.Nodes(%d)[:0]", c.Imm.(prim.Room))
		}
		return fmt.Sprintf("p.NewVals(%d)[:0]", c.Imm.(prim.Room))
	case prim.OpKids:
		return fmt.Sprintf("p.Kids(%s)", args[0])
	case prim.OpShapeSingle:
		return fmt.Sprintf("p.ShapeSingle(%s, %s, %s)", p.shaper(c.Imm.(prim.Shape)), args[0], args[1])
	case prim.OpShapeCapture:
		return fmt.Sprintf("p.ShapeCapture(%s, %s, %s)", p.shaper(c.Imm.(prim.Shape)), args[0], args[1])
	case prim.OpScopeValue:
		return fmt.Sprintf("p.ScopeValue(%s, %s, %s)", p.shaper(c.Imm.(prim.Shape)), args[0], args[1])
	case prim.OpAttachFrame:
		names := p.scopeNames(c.Imm.(prim.Attach).Names)
		if p.untyped() {
			// The Node library's AttachCaptures takes the frame.
			return fmt.Sprintf("p.AttachCaptures(%s, %s, %s, %s, p.Pos)", args[0], names, args[2], args[1])
		}
		return fmt.Sprintf("p.AttachCaptures(%s, %s, %s.Vals, %s, p.Pos)", args[0], names, args[2], args[1])
	case prim.OpAttachCaptures:
		names := p.scopeNames(c.Imm.(prim.Attach).Names)
		return fmt.Sprintf("p.AttachCaptures(%s, %s, []any{%s}, %s, p.Pos)", args[0], names, strings.Join(args[2:], ", "), args[1])
	case prim.OpMarkState:
		return "p.Mark()"
	case prim.OpMarkPos:
		return args[0] + ".Pos"
	case prim.OpNewFrame:
		return fmt.Sprintf("p.NewFrame(len(%s))", p.scopeNames(c.Imm.(*prim.Names)))
	case prim.OpFrameVal:
		return fmt.Sprintf("%s.Vals[%d]", args[0], c.Imm.(prim.Slot))
	case prim.OpKeep:
		return fmt.Sprintf("p.Keep(%s)", args[0])
	case prim.OpMakeError:
		// Under logical units the syntax error that a recovery builds is an event of one compute unit,
		// which the operation that builds it counts (flow.makeError).
		return fmt.Sprintf("p.%s(%s, %s)", g.pickLogical("MakeError"), args[0], args[1])
	case prim.OpErrorValue:
		return fmt.Sprintf("NewTError(%s, p.Pos, %s.Error())", args[0], args[1])
	case prim.OpNewErrorNode:
		return fmt.Sprintf("p.NewErrorNode(%s, %s)", args[0], args[1])
	case prim.OpFreshNode:
		return fmt.Sprintf("TFreshNode(%s)", args[0])
	case prim.OpConvertResult:
		x := c.Imm.(prim.Conversion)
		if x.Owned {
			return fmt.Sprintf("p.TConvertResultOwned(trules[%d], %s, %s)", x.Rule, args[0], args[1])
		}
		return fmt.Sprintf("p.TConvertResult(trules[%d], %s)", x.Rule, args[0])
	case prim.OpUseCtx:
		return fmt.Sprintf("p.UseCtx(TActCtx{P: p, Start: %s, End: p.Pos, Cbase: len(p.Created)})", args[0])
	case prim.OpEval:
		x := c.Imm.(prim.Action)
		defer p.caps(x.Names, args[1:])()
		stmts, res := g.actionStmts(x.Action, check.NewScope(), nil)
		p.pre += stmts
		return res
	case prim.OpCtxFinish:
		return fmt.Sprintf("%s.Finish(%s)", args[0], args[1])
	default:
		panic(fmt.Sprintf("primitive printer: operation %s", c.Callee))
	}
}

// appendText returns the statement of flow.append, which assigns the list it appends to.
func (p *primPrinter) appendText(c *code.Call, args []string) string {
	vals, x := args[0], args[1]
	spread := bool(c.Imm.(prim.Spread))
	if spread && p.untyped() {
		x = "D(" + x + ").Children..."
	} else if spread {
		x = fmt.Sprintf("AsTval(%s).TKids()...", x)
	}
	return fmt.Sprintf("%s = append(%s, %s)", vals, vals, x)
}

// hasLit returns the condition that the input at the position begins with the literal, read in place.
func (p *primPrinter) hasLit(lit prim.Lit) string {
	g := p.g
	if g.Traced {
		panic("internal error: an in-place read of a literal in a traced body")
	}
	rs := []rune(lit.Text)
	cond := fmt.Sprintf("p.Pos+%d <= len(p.In)", len(rs))
	if len(rs) == 1 {
		cond = "p.Pos < len(p.In)"
	}
	for i, r := range rs {
		if i == 0 {
			cond += fmt.Sprintf(" && p.In[p.Pos] == %d", r)
		} else {
			cond += fmt.Sprintf(" && p.In[p.Pos+%d] == %d", i, r)
		}
	}
	if p.document {
		return "p.Document == nil && (" + cond + ")"
	}
	return cond
}

// negate returns the negation of the call c of a primitive operation with the printed arguments args.
func (p *primPrinter) negate(c *code.Call, args []string) string {
	g := p.g
	switch c.Callee {
	case prim.OpAtAnchor:
		switch c.Imm.(prim.Anchor) {
		case prim.BeginInput:
			return "p.Pos != 0"
		case prim.EndInput:
			return "!p." + g.atEnd() + "()"
		case prim.BeginLine:
			return "!p." + g.atLineStart() + "()"
		default:
			return "!p." + g.helper("AtLineEnd") + "()"
		}
	case prim.OpMatchClass:
		class := p.class(c.Imm.(prim.ClassID))
		table := g.classTable(grammarClass(class))
		if g.Logical {
			return fmt.Sprintf("!p.ClassFastLogical(&%s) && !p.ClassSkipLogical(&%s, %d)", table, table, p.desc(class.Exp))
		}
		return fmt.Sprintf("!p.ClassFast(&%s) && !p.ClassSkip(&%s, %d)", table, table, p.desc(class.Exp))
	case prim.OpInClass:
		return p.reject(p.class(c.Imm.(prim.ClassID)), args[0])
	case prim.OpSkipTerminal:
		table, ok := g.terminalTable(c.Imm.(prim.Terminal))
		if !ok {
			panic("primitive printer: terminal region without a terminal program")
		}
		return fmt.Sprintf("!p.%s(%s)", g.pickLogical("SkipTerminal"), table)
	case prim.OpSkipClasses:
		run := c.Imm.(prim.ClassRun)
		items := make([]ast.Expr, len(run))
		for i, cl := range run {
			items[i] = grammarClass(p.class(cl))
		}
		return fmt.Sprintf("!p.%s(%s)", g.pickLogical("ClassesSkip"), g.classSteps(items))
	case prim.OpPred:
		x := c.Imm.(prim.Predicate)
		if x.Frame {
			// The action expression reads the current frame.
			return fmt.Sprintf("!p.Predicate(func(c *%s) (any, any) { %s })", g.ctxType(), g.actionBody(x.Action, common.FrameScope(x.Names), nil))
		}
		defer p.caps(x.Names, args)()
		return fmt.Sprintf("!p.Predicate(func(c *TActCtx) (any, any) { %s })", g.actionBody(x.Action, check.NewScope(), nil))
	case prim.OpBind:
		x := c.Imm.(prim.Binding)
		if x.Frame {
			return fmt.Sprintf("!p.Bind(%d, func(c *%s) (any, any) { %s })", g.Prog.Vars.Index(x.Var), g.ctxType(), g.actionBody(x.Action, common.FrameScope(x.Names), nil))
		}
		defer p.caps(x.Names, args)()
		return fmt.Sprintf("!p.Bind(%d, func(c *TActCtx) (any, any) { %s })", g.Prog.Vars.Index(x.Var), g.actionBody(x.Action, check.NewScope(), nil))
	}
	return "!" + p.op(c, args)
}

// caps sets the capture variables of the generator's action expressions to the captures of the slots names, whose
// printed arguments are args (nil for a slot the action expression does not read), and returns the function that
// restores them.
func (p *primPrinter) caps(names, args []string) func() {
	caps := map[string]string{}
	for i, a := range args {
		if a != "nil" {
			caps[names[i]] = a
		}
	}
	prev := p.g.caps
	p.g.caps = caps
	return func() { p.g.caps = prev }
}
