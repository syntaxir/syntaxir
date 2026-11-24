package print

import (
	"fmt"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/tsprim"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The TypeScript forms of the primitive operations of grammar code at the Exec stage. The name of a
// primitive (package prim) selects its form; the immediate holds its static data and the arguments
// its operands.

// tsPrimPrinter prints the primitives of a function of grammar code at the Exec stage.
type tsPrimPrinter struct {
	g *Generator
	b strings.Builder
	// gen reports that the function being printed is a resumable function of the stream, a generator.
	gen bool
	// dead reports that the current point is unreachable.
	dead bool
	// reads holds the locals and the parts of marks read at reachable points, by the pass before (nil
	// before the first pass: all are read).
	reads map[string]bool
	// next collects the locals and the parts of marks that the pass being printed reads.
	next map[string]bool
	// dry reports a pass that only collects reads: it writes no declarations.
	dry    bool
	labels map[string]string
	// decls holds the constants the printed text names, which the caller declares if it keeps the text.
	decls []tsDecl
	names map[*prim.Names]string
	// marks holds the marks of the whole parser state by local, with the parts they save.
	marks map[string]prim.StateMark
	// outlined holds the calls of the functions printed for the outlined nodes.
	outlined map[*code.Call]string
	// parts holds, for each mark local, the names that stand for its position, environment, trail
	// length and number of recovered errors, so that a mark is declared as far as it is read.
	parts map[string]*[tsMarkLen]string
}

// part returns the name that stands for the component i of the mark m.
func (p *tsPrimPrinter) part(m string, i int) string {
	ps := p.parts[m]
	if ps == nil {
		ps = &[tsMarkLen]string{}
		for j := range ps {
			ps[j] = m + tsMarkParts[j]
		}
		p.parts[m] = ps
	}
	return ps[i]
}

// label returns the TypeScript label of the label name: L_ and its name, or for a label that the
// lowering left unnamed, L and its number.
func (p *tsPrimPrinter) label(name string) string {
	if n, ok := p.labels[name]; ok {
		return n
	}
	n := "L" + strconv.Itoa(len(p.labels)+1)
	if !prim.Unnamed(name) {
		n = "L_" + name
	}
	for _, other := range p.labels {
		if other == n {
			n += "_" + strconv.Itoa(len(p.labels)+1)
		}
	}
	p.labels[name] = n
	return n
}

// read records the locals that the expressions read at a reachable point.
func (p *tsPrimPrinter) read(xs ...code.Expr) {
	if p.dead {
		return
	}
	for _, x := range xs {
		code.Walk(&code.Block{Stmts: []code.Stmt{&code.Assert{Cond: x}}}, nil, func(e code.Expr) {
			if l, ok := e.(*code.Local); ok {
				p.next[l.Name] = true
			}
		})
	}
}

// readName records that the local or the part of a mark name is read at a reachable point.
func (p *tsPrimPrinter) readName(name string) {
	if !p.dead {
		p.next[name] = true
	}
}

// isRead reports whether the local name is read, by the previous pass.
func (p *tsPrimPrinter) isRead(name string) bool {
	return name != "" && (p.reads == nil || p.reads[name])
}

// tsPrimPure reports whether evaluating the call c of a primitive has no effect, so that an
// assignment of it to nothing read is left out.
func tsPrimPure(c *code.Call) bool {
	switch c.Callee {
	case prim.OpEnv, prim.OpFrame, prim.OpRecLen, prim.OpKidsLen, prim.OpNewVals, prim.OpStr, prim.OpMarkPos,
		prim.OpFrameVal, prim.OpInClass, prim.OpGuardFails, prim.OpResumeOn, prim.OpResumeMore, prim.OpInDocument,
		prim.OpMissingLen:
		return true
	}
	return false
}

// tsPure reports whether evaluating x has no effect.
func tsPure(x code.Expr) bool { return Pure(x, tsPrimPure) }

// stmtOp writes the call c of a primitive without results, whose printed arguments args returns.
func (p *tsPrimPrinter) stmtOp(c *code.Call, args func() []string) {
	switch c.Callee {
	case prim.OpInputDemand:
		p.demand(c.Imm.(prim.Demand))
	case prim.OpStreamStart:
		p.write(p.streamParser() + ".streamStart();\n")
	case prim.OpEmitStream:
		p.read(c.Args...)
		p.write(fmt.Sprintf("%s.streamEmit(%s, p.pos);\n", p.streamParser(), args()[0]))
	case prim.OpStep:
		// The check of the counter at a loop's iteration (logical units only lower steps).
		p.write(tsUnitsCheck)
	case prim.OpSilent:
		s := c.Imm.(prim.Silence)
		if s.Delta > 0 {
			p.write("p.silent++;\n")
			if s.Site != 0 {
				p.write(fmt.Sprintf("p.lookBegin(%d);\n", s.Site))
			}
		} else {
			if s.Site != 0 {
				p.write(fmt.Sprintf("p.lookEnd(%t);\n", s.Ok))
			}
			p.write("p.silent--;\n")
		}
	case prim.OpAdvance:
		p.read(c.Args...)
		n := args()[0]
		p.write("p.pos += " + n + ";\n")
		if p.g.Logical {
			p.write("p.units += " + n + ";\n")
		}
	case prim.OpExpect:
		p.read(c.Args...)
		x := c.Imm.(prim.Expectation)
		if x.Message {
			p.write(fmt.Sprintf("p.expect(%s, msgBit | %d);\n", args()[0], p.desc(x.Exp)))
		} else {
			p.write(fmt.Sprintf("p.expect(%s, %s);\n", args()[0], p.exp(x.Exp)))
		}
	case prim.OpRestore:
		p.read(c.Args...)
		a := args()
		p.write(fmt.Sprintf("p.pos = %s;\np.recovered.length = %s;\n", a[0], a[1]))
	case prim.OpMergeExpected:
		p.read(c.Args...)
		a := args()
		p.write(fmt.Sprintf("p.mergeKept(%s, %s);\n", a[0], a[1]))
	case prim.OpMergeRenamed:
		p.read(c.Args...)
		a := args()
		p.write(fmt.Sprintf("p.mergeKept(%s, p.renameExpected(%s, %d));\n", a[0], a[1], p.desc(c.Imm.(prim.ExpID))))
	case prim.OpAppendError:
		p.read(c.Args...)
		if p.g.Logical {
			// The syntax error that a recovery built is an event of one compute unit.
			p.write("p.units++;\n")
		}
		p.write(fmt.Sprintf("p.recovered.push(%s!);\n", args()[0]))
	case prim.OpSkippedCalls:
		p.write(p.skippedCalls(c.Imm.(prim.SkippedCalls)))
	case prim.OpMissingScopeEnd:
		p.read(c.Args...)
		p.write(fmt.Sprintf("p.missingScopeEnd(%s);\n", args()[0]))
	case prim.OpResetMissing:
		p.read(c.Args...)
		p.write(fmt.Sprintf("p.resetMissing(%s);\n", args()[0]))
	case prim.OpPushKid:
		p.read(c.Args...)
		p.write(fmt.Sprintf("p.kidStack.push(%s);\n", args()[0]))
	case prim.OpDropKids:
		p.read(c.Args...)
		p.write(fmt.Sprintf("p.dropKids(%s);\n", args()[0]))
	case prim.OpAppend:
		p.read(c.Args...)
		a := args()
		if c.Imm.(prim.Spread) {
			p.write(fmt.Sprintf("for (const k of (%s as Node).children) {\n  %s.push(k);\n}\n", a[1], a[0]))
		} else {
			p.write(fmt.Sprintf("%s.push(%s);\n", a[0], a[1]))
		}
	case prim.OpSetCapture:
		p.read(c.Args...)
		p.write(fmt.Sprintf("p.setCapture(%d, %s);\n", c.Imm.(prim.Slot), args()[0]))
	case prim.OpClearVals:
		p.read(c.Args...)
		p.write(args()[0] + ".fill(null);\n")
	case prim.OpFreeFrame:
		// Frames are not pooled.
	case prim.OpDeclareNames:
		// The names are declared where they are first used.
	case prim.OpReset:
		p.write(p.reset(p.markLocal(c)))
	case prim.OpDropTrail:
		m := p.markLocal(c)
		if !p.marks[m].Trail {
			panic("tsgen: the verifier admitted a mark without a trail dropped")
		}
		p.readName(p.part(m, 2))
		p.write(fmt.Sprintf("p.trail.length = %sTrail;\n", m))
	case prim.OpSkipBegin:
		p.write(fmt.Sprintf("p.skipBegin(%d);\n", c.Imm.(prim.Site)))
	case prim.OpRunBegin:
		w := c.Imm.(prim.RunWalk)
		key := "null"
		if w.KeyEnv {
			key = p.g.runKeyName(w.Site)
		}
		p.write(fmt.Sprintf("p.runBegin(%d, %t, %s);\n", w.Site, w.Build, key))
	case prim.OpSkipVisit:
		p.write("p.skipVisit();\n")
	case prim.OpSkipValue:
		p.write(fmt.Sprintf("p.skipValue(%s);\n", args()[0]))
	case prim.OpSkipEnd:
		p.write("p.skipEnd();\n")
	case prim.OpRunWalked:
		p.write(fmt.Sprintf("p.runWalked(%d, %s);\n", c.Imm.(prim.Site), args()[0]))
	case prim.OpScanEnd:
		a := args()
		p.write(fmt.Sprintf("p.scanEnd(%d, %s, %s);\n", c.Imm.(prim.Site), a[0], a[1]))
	case prim.OpResumeBegin:
		p.write(fmt.Sprintf("p.resumeBegin(%s);\n", args()[0]))
	case prim.OpResumeEnd:
		a := args()
		p.write(fmt.Sprintf("p.resumeEnd(%s, %s, %s);\n", a[0], a[1], a[2]))
	case prim.OpResumeFinish:
		p.write(fmt.Sprintf("p.resumeFinish(%s);\n", args()[0]))
	case prim.OpObserveSkip:
		if p.dead || p.dry {
			return
		}
		trace := p.g.traceSkipped(c.Imm.(prim.ObservedSkip))
		p.b.WriteString(strings.TrimLeft(trace, " "))
	default:
		admitted("the operation " + c.Callee)
	}
}

// expr returns the TypeScript expression of the call c of a primitive with results, whose printed
// arguments args returns.
func (p *tsPrimPrinter) expr(c *code.Call, args func() []string) string {
	g := p.g
	switch c.Callee {
	case prim.OpEnv:
		return "p.env"
	case prim.OpFrame:
		return "p.frame"
	case prim.OpRecLen:
		return "p.recovered.length"
	case prim.OpInDocument:
		return "p.inDocument"
	case prim.OpKidsLen:
		return "p.kidStack.length"
	case prim.OpStreamOn:
		return fmt.Sprintf("%s.streamsIn(%d, %t)", p.streamParser(), g.Cur.ID, g.Cur.Rec)
	case prim.OpSkipHit:
		return "p.skipHit()"
	case prim.OpRunHot:
		return fmt.Sprintf("p.runHot(%d)", c.Imm.(prim.Site))
	case prim.OpSkipKids:
		return "p.skipKids()"
	case prim.OpScanReach:
		a := args()
		return fmt.Sprintf("p.scanReach(%d, %s, %s)", c.Imm.(prim.Site), a[0], a[1])
	case prim.OpScanKids:
		a := args()
		return fmt.Sprintf("p.scanKids(%d, %s, %s, %s)", c.Imm.(prim.Site), a[0], a[1], a[2])
	case prim.OpResumeOn:
		return "p.resumeOn()"
	case prim.OpResumeStart:
		x := c.Imm.(prim.Resume)
		return fmt.Sprintf("p.resumeStart(%d, %d, %d, %t)", x.Site, x.Min, x.Max, x.Shiftable)
	case prim.OpResumeMore:
		return fmt.Sprintf("p.resumeMore(%s)", args()[0])
	case prim.OpResumeNext:
		return fmt.Sprintf("p.resumeNext(%s)", args()[0])
	case prim.OpMatchLit:
		lit := c.Imm.(prim.Lit)
		if p.dry {
			return ""
		}
		v := p.litConst(lit)
		if lit.Fold {
			return fmt.Sprintf("p.matchLiteralFold(%s, %d, false)", v, p.desc(lit.Exp))
		}
		var desc rprogram.ExpID
		if lit.Text == "" {
			desc = g.Desc(core.LiteralDesc(&ast.Literal{}))
		} else {
			desc = p.desc(lit.Exp)
		}
		return fmt.Sprintf("p.matchLiteral(%s, %d, false)", v, desc)
	case prim.OpGuardFails:
		guard := c.Imm.(prim.Guard)
		a := args()
		ch, more := a[0], a[1]
		return fmt.Sprintf("!(%s && (%s)) && %s", more, tsClassIn(p.class(guard.Class), ch), tsDepthLimit(guard.Depth))
	case prim.OpGuardLitFails:
		guard := c.Imm.(prim.Guard)
		return fmt.Sprintf("p.matchChar(%d, %t) < 0 && %s", guard.Rune, guard.Fold, tsDepthLimit(guard.Depth))
	case prim.OpInClass:
		return tsClassIn(p.class(c.Imm.(prim.ClassID)), args()[0])
	case prim.OpAtAnchor, prim.OpAtEnd:
		switch c.Imm.(prim.Anchor) {
		case prim.BeginInput:
			return "p.pos === 0"
		case prim.EndInput:
			return "p.atEnd()"
		case prim.BeginLine:
			return "p.atLineStart()"
		default:
			return "p.atLineEnd()"
		}
	case prim.OpCallRule:
		x := c.Imm.(prim.RuleCall)
		return p.call(g.RuleOf(x.Rule), x.Level)
	case prim.OpIsolate:
		return "p.isolate()"
	case prim.OpKeep:
		// The expectations are a copy of the record already.
		return args()[0]
	case prim.OpMakeError:
		a := args()
		return fmt.Sprintf("p.recoveredError(%s, %s)", a[0], a[1])
	case prim.OpNewMissingNode:
		a := args()
		return fmt.Sprintf("p.missingNode(%s, %s, %s!)", a[0], TsString(c.Imm.(prim.Insertion).Lit.Text), a[1])
	case prim.OpMissingScope:
		return fmt.Sprintf("p.missingScope(msgBit | %d)", p.desc(c.Imm.(prim.ExpID)))
	case prim.OpMissingLen:
		return "p.missingLen()"
	case prim.OpNewErrorNode:
		a := args()
		return p.counted(fmt.Sprintf("new Node(\"error\", \"\", %s, p.pos, \"\", noNodes, [{ name: \"message\", value: %s!.message }], true, true)", a[0], a[1]), "nodeBytes + arrayBytes(1) + objectBytes(2)")
	case prim.OpText, prim.OpLiteralText:
		// A literal's text is the input it matched: where the literal holds the replacement character,
		// that stands for invalid input.
		p.read(c.Args...)
		return fmt.Sprintf("p.text(%s, p.pos)", args()[0])
	case prim.OpStr:
		return TsString(string(c.Imm.(prim.Chars)))
	case prim.OpNewTerminalNode:
		a := args()
		return p.counted(fmt.Sprintf("new Node(\"string\", \"\", %s, p.pos, %s, noNodes, noFields, true, true)", a[0], a[1]), "nodeBytes")
	case prim.OpNewBranchNode:
		kind := "tuple"
		if c.Imm.(prim.List) {
			kind = "list"
		}
		a := args()
		return p.counted(fmt.Sprintf("new Node(\"%s\", \"\", %s, p.pos, \"\", %s, noFields, true, false)", kind, a[0], a[1]), "nodeBytes")
	case prim.OpNewVals:
		return "[]"
	case prim.OpKids:
		return fmt.Sprintf("p.kids(%s)", args()[0])
	case prim.OpShapeSingle:
		a := args()
		return fmt.Sprintf("p.shapeSingle(%d, %s, %s)", tsShape(c.Imm.(prim.Shape)), a[0], a[1])
	case prim.OpShapeCapture:
		a := args()
		return fmt.Sprintf("p.shapeCapture(%d, %s, %s)", tsShape(c.Imm.(prim.Shape)), a[0], a[1])
	case prim.OpScopeValue:
		a := args()
		return fmt.Sprintf("p.scopeValue(%d, %s, %s)", tsShape(c.Imm.(prim.Shape)), a[0], a[1])
	case prim.OpAttachFrame:
		a := args()
		return fmt.Sprintf("p.attachCaptures(%s, %s, %s, %s, p.pos)", a[0], p.scopeNames(c.Imm.(prim.Attach).Names), a[2], a[1])
	case prim.OpPred:
		x := c.Imm.(prim.Predicate)
		if !x.Frame {
			admitted("a predicate that reads variables")
		}
		// The action expression reads the current frame.
		if p.dry {
			return "p.predicate(a)"
		}
		return fmt.Sprintf("p.predicate(%s)", p.g.predicateFunc(x.Action, common.FrameScope(x.Names)))
	case prim.OpBind:
		x := c.Imm.(prim.Binding)
		if !x.Frame {
			admitted("a bind that reads variables")
		}
		if p.dry {
			return "p.bind(a)"
		}
		// The function is declared only if the rule keeps the printed text.
		name := p.g.Name("a")
		p.decl(name, p.g.actionFunc(name, x.Action, common.FrameScope(x.Names), nil))
		return fmt.Sprintf("p.bind(%d, %s)", p.g.Prog.Vars.Index(x.Var), name)
	case prim.OpCallOutline:
		return p.outline(c)
	case prim.OpMarkPos:
		m := p.markLocal(c)
		p.readName(p.part(m, 0))
		return m + "Pos"
	case prim.OpNewFrame:
		return fmt.Sprintf("p.newFrame(%d)", len(c.Imm.(*prim.Names).List))
	case prim.OpFrameVal:
		return fmt.Sprintf("%s[%d]!", args()[0], c.Imm.(prim.Slot))
	}
	admitted("the operation " + c.Callee)
	return ""
}

// markLocal returns the local of the mark that the call c takes as its argument.
func (p *tsPrimPrinter) markLocal(c *code.Call) string {
	l, ok := c.Args[0].(*code.Local)
	if !ok {
		panic("tsgen: the verifier admitted a mark that is not a variable")
	}
	return l.Name
}

// set writes the assignment s of one call of a primitive.
func (p *tsPrimPrinter) set(s *code.Set, expr func(code.Expr) string) {
	if p.dead {
		return
	}
	if len(s.Dst) == 1 {
		// The expectations are a copy already, so keeping them leaves the variable as it is.
		c := s.Values[0].(*code.Call)
		if c.Callee == prim.OpKeep && !s.Dst[0].Reg {
			if l, ok := c.Args[0].(*code.Local); ok && l.Name == s.Dst[0].Name {
				return
			}
		}
	}
	if text := p.setText(s, expr, true); text != "" {
		p.write(text)
	}
}

// setText returns the assignment s of one call of a primitive as TypeScript, or "" if it is left out.
// As a statement it ends with a semicolon and a newline.
func (p *tsPrimPrinter) setText(s *code.Set, expr func(code.Expr) string, stmt bool) string {
	end := ""
	if stmt {
		end = ";\n"
	}
	c := s.Values[0].(*code.Call)
	name := c.Callee
	used := false
	for _, d := range s.Dst {
		if d.Reg || d.Name == "" {
			used = true
		} else {
			used = used || p.isRead(d.Name)
		}
	}
	local := func(d code.Place) string {
		if d.Reg {
			return ""
		}
		return d.Name
	}
	if len(s.Dst) == 1 && name == prim.OpMarkState {
		m := local(s.Dst[0])
		if p.marks == nil {
			p.marks = map[string]prim.StateMark{}
		}
		p.marks[m] = c.Imm.(prim.StateMark)
		srcs := [tsMarkLen]string{"p.pos", "p.env", "p.trail.length", "p.recovered.length", "p.missingLen()"}
		var parts []string
		for i, src := range srcs {
			if p.isRead(p.part(m, i)) {
				parts = append(parts, m+tsMarkParts[i]+" = "+src)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, stmtSep(stmt)) + end
	}
	if len(s.Dst) == 2 && name == prim.OpShapeSingle {
		// The second result, whether the value is a single element, is not used.
		return p.setText(&code.Set{Dst: s.Dst[:1], Values: s.Values}, expr, stmt)
	}
	if len(s.Dst) == 2 && name == prim.OpUnisolate {
		// The farthest failure of the record, and a copy of its expectations in p.isoIds.
		p.read(c.Args...)
		far, ids := local(s.Dst[0]), local(s.Dst[1])
		farRead, idsRead := p.isRead(far), p.isRead(ids)
		call := fmt.Sprintf("p.unisolate(%s)", expr(c.Args[0]))
		switch {
		case farRead && idsRead:
			return fmt.Sprintf("%s = %s%s%s = p.isoIds%s", far, call, stmtSep(stmt), ids, end)
		case farRead:
			return fmt.Sprintf("%s = %s%s", far, call, end)
		}
		// Only the expectations are read, or none: the record still ends.
		if idsRead {
			return fmt.Sprintf("%s%s%s = p.isoIds%s", call, stmtSep(stmt), ids, end)
		}
		return call + end
	}
	if len(s.Dst) == 2 && name == prim.OpInsertMissing {
		// The syntax error of the insertion, null where the site does not insert, and whether it
		// inserted. Under logical units the error is an event of one compute unit, which the
		// operation that builds it counts (flow.insertMissing).
		p.read(c.Args...)
		x := fmt.Sprintf("p.insertMissing(%d, %s, %t)", c.Imm.(prim.Insertion).Site, expr(c.Args[0]), p.g.Logical)
		ev, ov := local(s.Dst[0]), local(s.Dst[1])
		errRead, okRead := p.isRead(ev), p.isRead(ov)
		switch {
		case errRead && okRead:
			return fmt.Sprintf("%s = %s%s%s = %s !== null%s", ev, x, stmtSep(stmt), ov, ev, end)
		case errRead:
			return fmt.Sprintf("%s = %s%s", ev, x, end)
		case okRead:
			return fmt.Sprintf("%s = %s !== null%s", ov, x, end)
		}
		return x + end
	}
	if len(s.Dst) == 2 && tsprim.TwoResults(name) {
		// A call with two results: the value and whether it matched.
		p.read(c)
		x := expr(c)
		vv, ov := local(s.Dst[0]), local(s.Dst[1])
		valRead, okRead := p.isRead(vv), p.isRead(ov)
		switch {
		case valRead && okRead:
			return fmt.Sprintf("%s = %s as Node | null%s%s = %s !== undefined%s", vv, x, stmtSep(stmt), ov, vv, end)
		case valRead:
			return fmt.Sprintf("%s = %s as Node | null%s", vv, x, end)
		case okRead:
			return fmt.Sprintf("%s = %s !== undefined%s", ov, x, end)
		}
		return x + end
	}
	if !used && tsPure(c) {
		return ""
	}
	if len(s.Dst) != 1 {
		admitted("an assignment of the several results of " + name)
	}
	var parts []string
	switch d := s.Dst[0]; {
	case d.Reg:
		p.read(c)
		parts = append(parts, p.reg(d.Name)+" = "+expr(c))
	case d.Name == "":
		if !tsPure(c) {
			p.read(c)
			parts = append(parts, expr(c))
		}
	default:
		if !p.isRead(d.Name) && tsPure(c) {
			break
		}
		p.read(c)
		x := expr(c)
		if p.isRead(d.Name) {
			parts = append(parts, d.Name+" = "+x)
		} else {
			parts = append(parts, x)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, stmtSep(stmt)) + end
}

// reg returns the TypeScript expression of the register name.
func (p *tsPrimPrinter) reg(name string) string {
	if name == prim.Cut {
		return "p.cut"
	}
	return "p.pos"
}

// testedCond returns the condition that the call, which has two results, fails: the call and its
// test in one condition, since nothing else reads the flag it sets. The condition assigns the value
// of the call to the local v if something reads v.
func (p *tsPrimPrinter) testedCond(call *code.Call, v string, expr func(code.Expr) string) string {
	p.read(call)
	cond := expr(call) + " === undefined"
	if p.isRead(v) {
		cond = fmt.Sprintf("(%s = %s as Node | null) === undefined", v, expr(call))
	}
	return cond
}

// peek writes the read of the next character into the places of s.
func (p *tsPrimPrinter) peek(s *code.Set) {
	if p.dead {
		return
	}
	name := func(i int) string {
		if s.Dst[i].Reg {
			return ""
		}
		return s.Dst[i].Name
	}
	ch, size, ok := name(0), name(1), name(2)
	wantCh, wantSize, wantOK := p.isRead(ch), p.isRead(size), p.isRead(ok)
	var b strings.Builder
	switch {
	case wantCh:
		fmt.Fprintf(&b, "%s = p.peek();\n", ch)
		if wantOK {
			fmt.Fprintf(&b, "%s = %s >= 0;\n", ok, ch)
		}
	case wantOK:
		fmt.Fprintf(&b, "%s = p.peek() >= 0;\n", ok)
	default:
		b.WriteString("p.peek();\n")
	}
	if wantSize {
		fmt.Fprintf(&b, "%s = p.psize;\n", size)
	}
	p.write(b.String())
}

// reset returns the statement that goes back to the mark m: the position, and the parts of the parser
// state that the code since the mark may change.
func (p *tsPrimPrinter) reset(m string) string {
	st, ok := p.marks[m]
	if !ok {
		panic("tsgen: the verifier admitted a reset of a mark that is not set")
	}
	p.readName(p.part(m, 0))
	if !st.Env && !st.Trail && !st.Rec && !st.Missing {
		return "p.pos = " + m + "Pos;\n"
	}
	// A mark that does not keep the insertions on the path goes back to as many as there are: the code
	// since the mark inserts none. A table outside the insertion program runs only outside the
	// missing-token mode, where there are none.
	missing := "0"
	if p.g.insertionTable() {
		missing = "p.missingLen()"
	}
	args := []string{m + "Pos", "p.env", "p.trail.length", "p.recovered.length", missing}
	for i, needed := range [tsMarkLen]bool{true, st.Env, st.Trail, st.Rec, st.Missing} {
		if needed {
			p.readName(p.part(m, i))
			args[i] = m + tsMarkParts[i]
		}
	}
	return "p.reset(" + strings.Join(args, ", ") + ");\n"
}

// outline prints the function of the outlined node of the call c and returns the call of it. The
// passes that only collect reads print no function.
func (p *tsPrimPrinter) outline(c *code.Call) string {
	if p.dry {
		return "e(p)"
	}
	if call, ok := p.outlined[c]; ok {
		return call
	}
	x := c.Imm.(prim.Outline)
	g := p.g
	k, ok := g.table.Plan.Outlined(x.Call)
	var l *execdata.Lowered
	if ok {
		l, ok = execdata.Func(g.table, k)
	}
	if !ok {
		panic(fmt.Errorf("internal error: the table %s has no function of the outlined node of rule %s", g.Table, g.Cur.Name))
	}
	call := g.funcCall(l, x.Slots, check.NewScope(), g.Cur.Name+" (outlined)")
	if p.outlined == nil {
		p.outlined = map[*code.Call]string{}
	}
	p.outlined[c] = call
	return call
}

// tsDepthLimit returns the condition that the nesting limit allows depth more calls.
func tsDepthLimit(depth int) string {
	if depth == 0 {
		return "p.depth <= p.maxDepth"
	}
	return fmt.Sprintf("p.depth + %d <= p.maxDepth", depth)
}

// litConst returns the constant of the literal lit that the library matches (a Lit), declaring it on
// first use: its text, folded if it is case-insensitive, with its code points and bytes.
func (p *tsPrimPrinter) litConst(lit prim.Lit) string {
	if p.dry {
		return "lit"
	}
	val := litText(lit)
	v, shared := p.g.LiteralName(val)
	if !shared {
		p.decl(v, fmt.Sprintf("const %s: Lit = { text: %s, cps: %s, bytes: %s };\n", v, TsString(val), TsInts([]rune(val)), TsInts([]byte(val))))
	}
	return v
}

// litText returns the text of the literal lit that the library matches: its value, folded if it is
// case-insensitive.
func litText(lit prim.Lit) string {
	if lit.Fold {
		return string(common.FoldedRunes([]rune(lit.Text)))
	}
	return nodes.LiteralValue(lit.Text)
}
