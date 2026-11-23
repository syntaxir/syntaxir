package vmlower

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	flowpkg "syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// The selection of the instructions of a Flow function is a table of local patterns over its
// Flow-stage operations (patterns). A pattern of one operation selects the instruction of that
// operation wherever the operation is called. A pattern of a sequence of statements, a
// superinstruction, selects one instruction for operations that test a match and leave a block when
// it fails: a Flow body that spells them out runs one dispatch for each.
//
// The table is applied in a fixed order: at each statement of a block, in order, the patterns of
// statements that match it are compared, and the one that covers the most operations wins, then the
// one that saves the most instructions, then the one declared first. A statement that no such pattern
// matches is selected by its kind, and the operations it calls by their patterns of one operation.
// The selection counts the matches of each pattern (Counts).

// pattern is an entry of the selection table: a pattern of one operation (Op), or of a sequence of
// statements (Stmts).
type pattern struct {
	Name string
	// Op is the operation of a pattern of one operation, and Sel selects its instruction for the call
	// c, with the results in dst (nil for none).
	Op  string
	Sel func(s *flowSel, c *code.Call, dst []int) error
	// Stmts matches the statements at the start of list: it returns the number of statements it
	// covers, 0 if it does not match, and the function that emits their instruction.
	Stmts func(s *flowSel, list []code.Stmt) (n int, emit func() error)
	// Ops is the number of Flow operations a pattern of statements covers, and Saved the number of
	// instructions it saves over selecting them one by one.
	Ops, Saved int
}

// statementPatterns are the superinstructions, in the order that breaks ties.
var statementPatterns = []pattern{
	{Name: "matchClass", Ops: 3, Saved: 11, Stmts: matchClass},
	{Name: "guardJump", Ops: 2, Saved: 3, Stmts: guardJump},
	{Name: "guardLitJump", Ops: 2, Saved: 3, Stmts: guardLitJump},
	{Name: "assertJump", Ops: 2, Saved: 3, Stmts: assertJump},
	{Name: "matchLitJump", Ops: 1, Saved: 1, Stmts: func(s *flowSel, list []code.Stmt) (int, func() error) {
		return testedJump(s, list, flowpkg.OpMatchLit, bytecode.FlowMatchLitJump, func(c *code.Call) (any, error) { return s.litOf(c.Imm.(prim.Lit)), nil })
	}},
	{Name: "callJump", Ops: 1, Saved: 1, Stmts: func(s *flowSel, list []code.Stmt) (int, func() error) {
		return testedJump(s, list, flowpkg.OpCallRule, bytecode.FlowCallJump, func(c *code.Call) (any, error) { return s.ruleCallOf(c) })
	}},
	{Name: "scanReachAt", Ops: 1, Saved: 3, Stmts: scanReachAt},
	{Name: "scanEndAt", Ops: 1, Saved: 3, Stmts: scanEndAt},
	{Name: "scanClass", Ops: 5, Saved: 5, Stmts: scanClass},
	{Name: "matchClassNode", Ops: 6, Saved: 15, Stmts: matchClassNode},
	{Name: "matchLitNode", Ops: 3, Saved: 4, Stmts: matchLitNode},
	{Name: "returnOk", Ops: 0, Saved: 1, Stmts: returnOk},
	{Name: "returnFail", Ops: 0, Saved: 2, Stmts: returnFail},
}

// returnOk matches the return of a value with success, `return v, true`: RETURNOK of v.
func returnOk(s *flowSel, list []code.Stmt) (int, func() error) {
	r, ok := list[0].(*code.Return)
	if !ok || len(r.Values) != 2 || !isConst(r.Values[1], true) {
		return 0, nil
	}
	return 1, func() error {
		v, err := s.expr(r.Values[0])
		if err != nil {
			return err
		}
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowReturnOk, Args: []int{v}})
		return nil
	}
}

// returnFail matches the return of failure, `return nil, false`: RETURNFAIL.
func returnFail(s *flowSel, list []code.Stmt) (int, func() error) {
	r, ok := list[0].(*code.Return)
	if !ok || len(r.Values) != 2 || !isConst(r.Values[0], nil) || !isConst(r.Values[1], false) {
		return 0, nil
	}
	return 1, func() error {
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowReturnFail})
		return nil
	}
}

// isConst reports whether e is the constant v.
func isConst(e code.Expr, v any) bool {
	c, ok := e.(*code.Const)
	return ok && c.Value == v
}

// jumpOf returns the label that the jump statement st leaves to, and whether it continues the loop
// of the label rather than leaving it; ok is false if st is no jump.
func jumpOf(st code.Stmt) (label string, cont, ok bool) {
	switch x := st.(type) {
	case *code.Exit:
		return x.Label, false, true
	case *code.Break:
		return x.Label, false, true
	case *code.Continue:
		return x.Label, true, true
	}
	return "", false, false
}

// onlyJump returns the jump that the block b consists of: one jump statement, and no label.
func onlyJump(b *code.Block) (label string, cont, ok bool) {
	if b == nil || b.Label != "" || len(b.Stmts) != 1 {
		return "", false, false
	}
	return jumpOf(b.Stmts[0])
}

// expectJump returns the expectation of the block b that records an expectation at the position
// and then jumps: `flow.expect(E, $pos); exit L`.
func expectJump(b *code.Block) (exp *code.Call, label string, cont, ok bool) {
	if b == nil || b.Label != "" || len(b.Stmts) != 2 {
		return nil, "", false, false
	}
	do, isDo := b.Stmts[0].(*code.Do)
	if !isDo || do.Call.Callee != flowpkg.OpExpect || len(do.Call.Args) != 1 || !isReg(do.Call.Args[0], prim.Pos) {
		return nil, "", false, false
	}
	label, cont, ok = jumpOf(b.Stmts[1])
	return do.Call, label, cont, ok
}

func isReg(e code.Expr, name string) bool {
	r, ok := e.(*code.Reg)
	return ok && r.Name == name
}

// isLocal reports whether e reads the local name.
func isLocal(e code.Expr, name string) bool {
	l, ok := e.(*code.Local)
	return ok && l.Name == name
}

// not returns the operand of the negation e, or nil if e is none.
func not(e code.Expr) code.Expr {
	if u, ok := e.(*code.Unary); ok && u.Op == "!" {
		return u.X
	}
	return nil
}

// callOf returns the call of the operation op that e is, or nil.
func callOf(e code.Expr, op string) *code.Call {
	if c, ok := e.(*code.Call); ok && c.Callee == op {
		return c
	}
	return nil
}

// place returns the register of the place p of a local or of a discarded value (-1), and false for a
// register place.
func (s *flowSel) place(p code.Place) (int, bool) {
	switch {
	case p.Reg:
		return 0, false
	case p.Name == "":
		return -1, true
	}
	r, ok := s.regs[p.Name]
	return r, ok
}

// classTest is the test of one character of a class at the start of a list of statements:
//
//	ch, size, ok = flow.peek()
//	if !ok || !flow.inClass(C, ch) { flow.expect(E, $pos); exit L }
//
// with the registers of ch, size and ok, the test's immediate, the local of size, and the jump that
// a character not in the class takes.
type classTest struct {
	dst   []int
	size  string
	in    *code.Call
	exp   *code.Call
	label string
	cont  bool
}

// classTestOf returns the test of one character of a class that list starts with, which covers two
// statements.
func classTestOf(s *flowSel, list []code.Stmt) (classTest, bool) {
	if len(list) < 2 {
		return classTest{}, false
	}
	peek, ok := list[0].(*code.Set)
	if !ok || len(peek.Dst) != 3 || len(peek.Values) != 1 || callOf(peek.Values[0], flowpkg.OpPeek) == nil {
		return classTest{}, false
	}
	test, ok := list[1].(*code.If)
	if !ok || test.Init != nil || test.Else != nil {
		return classTest{}, false
	}
	or, ok := test.Cond.(*code.Binary)
	if !ok || or.Op != "||" || !isLocal(not(or.X), peek.Dst[2].Name) {
		return classTest{}, false
	}
	in := callOf(not(or.Y), flowpkg.OpInClass)
	if in == nil || len(in.Args) != 1 || !isLocal(in.Args[0], peek.Dst[0].Name) || peek.Dst[0].Name == "" || peek.Dst[2].Name == "" {
		return classTest{}, false
	}
	exp, label, cont, ok := expectJump(test.Then)
	if !ok {
		return classTest{}, false
	}
	dst := make([]int, 3)
	for i, p := range peek.Dst {
		if dst[i], ok = s.place(p); !ok {
			return classTest{}, false
		}
	}
	return classTest{dst: dst, size: peek.Dst[1].Name, in: in, exp: exp, label: label, cont: cont}, true
}

// imm returns the immediate of the test t, as FlowMatchClass takes it.
func (t classTest) imm(s *flowSel) bytecode.FlowClassTestOf {
	return bytecode.FlowClassTestOf{Class: int(s.c.class(t.in.Imm.(prim.ClassID))), Expect: s.expectOf(t.exp)}
}

// matchClass matches the test of one character of a class (classTestOf).
func matchClass(s *flowSel, list []code.Stmt) (int, func() error) {
	t, ok := classTestOf(s, list)
	if !ok {
		return 0, nil
	}
	return 2, func() error {
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: bytecode.FlowMatchClass, Dst: t.dst, Imm: t.imm(s)}), t.label, t.cont)
		return nil
	}
}

// scanClass matches a repetition of one character of a class that keeps no value, with or without
// a count of the characters, and with or without the test of a walk of a scan site at the scan hot
// span:
//
//	loop L {
//		flow.step()
//		ch, size, ok = flow.peek()
//		if !ok || !flow.inClass(C, ch) { flow.expect(E, $pos); break L }
//		flow.advance(size)
//		[count = count + 1
//		[if count == ScanHotSpan { count = flow.scanReach(S, start, count) }]]
//	}
//
// and selects SCANCLASS, SCANCLASSCOUNT or SCANCLASSAT, which runs the whole loop.
func scanClass(s *flowSel, list []code.Stmt) (int, func() error) {
	loop, ok := list[0].(*code.Loop)
	if !ok || loop.Init != nil || loop.Cond != nil || loop.Post != nil || loop.Body == nil || loop.Body.Label != "" {
		return 0, nil
	}
	body := loop.Body.Stmts
	if len(body) < 4 || len(body) > 6 {
		return 0, nil
	}
	if step, isDo := body[0].(*code.Do); !isDo || step.Call.Callee != flowpkg.OpStep || len(step.Call.Args) != 0 {
		return 0, nil
	}
	t, ok := classTestOf(s, body[1:])
	if !ok || t.label != loop.Label || t.cont || t.size == "" {
		return 0, nil
	}
	if adv, isDo := body[3].(*code.Do); !isDo || adv.Call.Callee != flowpkg.OpAdvance || len(adv.Call.Args) != 1 || !isLocal(adv.Call.Args[0], t.size) {
		return 0, nil
	}
	if len(body) == 4 {
		return 1, func() error {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowScanClass, Dst: t.dst, Imm: t.imm(s)})
			return nil
		}
	}
	inc, isSet := body[4].(*code.Set)
	if !isSet || len(inc.Dst) != 1 || len(inc.Values) != 1 || inc.Dst[0].Name == "" {
		return 0, nil
	}
	count := inc.Dst[0].Name
	add, isBin := inc.Values[0].(*code.Binary)
	if !isBin || add.Op != "+" || !isLocal(add.X, count) || !isInt(add.Y, 1) {
		return 0, nil
	}
	n, ok := s.place(inc.Dst[0])
	if !ok || n < 0 || slices.Contains(t.dst, n) {
		return 0, nil
	}
	dst := append(slices.Clone(t.dst), n)
	if len(body) == 5 {
		return 1, func() error {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowScanClassCount, Dst: dst, Args: []int{n}, Imm: t.imm(s)})
			return nil
		}
	}
	cond, reach, ok := onlyStmt(body[5])
	if !ok {
		return 0, nil
	}
	c, counted, ok := scanCall(reach, flowpkg.OpScanReach)
	eq, isEq := cond.(*code.Binary)
	set, isSet := reach.(*code.Set)
	if !ok || counted != count || !isEq || eq.Op != "==" || !isLocal(eq.X, count) || !isInt(eq.Y, flowpkg.ScanHotSpan) ||
		!isSet || set.Dst[0].Name != count || !isLocal(c.Args[1], count) {
		return 0, nil
	}
	start, isStart := c.Args[0].(*code.Local)
	if !isStart {
		return 0, nil
	}
	from, ok := s.regs[start.Name]
	if !ok || from == n || slices.Contains(t.dst, from) {
		return 0, nil
	}
	return 1, func() error {
		imm := bytecode.FlowScanClassOf{Test: t.imm(s), Site: int(c.Imm.(prim.Site))}
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowScanClassAt, Dst: dst, Args: []int{from, n}, Imm: imm})
		return nil
	}
}

// guardJump matches a failed guard that leaves:
//
//	if flow.guardFails(G, ch, ok) { flow.expect(E, $pos); exit L }
func guardJump(s *flowSel, list []code.Stmt) (int, func() error) {
	test, ok := list[0].(*code.If)
	if !ok || test.Init != nil || test.Else != nil {
		return 0, nil
	}
	g := callOf(test.Cond, flowpkg.OpGuardFails)
	if g == nil || len(g.Args) != 2 {
		return 0, nil
	}
	exp, label, cont, ok := expectJump(test.Then)
	if !ok {
		return 0, nil
	}
	return 1, func() error {
		args, err := s.args(g.Args)
		if err != nil {
			return err
		}
		guard, err := s.guardOf(g.Imm.(prim.Guard))
		if err != nil {
			return err
		}
		imm := bytecode.FlowGuardTestOf{Guard: guard, Expect: s.expectOf(exp)}
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: bytecode.FlowGuardJump, Args: args, Imm: imm}), label, cont)
		return nil
	}
}

// guardLitJump matches a failed guard of a literal that leaves:
//
//	if flow.guardLitFails(G) { flow.expect(E, $pos); exit L }
func guardLitJump(s *flowSel, list []code.Stmt) (int, func() error) {
	test, ok := list[0].(*code.If)
	if !ok || test.Init != nil || test.Else != nil {
		return 0, nil
	}
	g := callOf(test.Cond, flowpkg.OpGuardLitFails)
	if g == nil || len(g.Args) != 0 {
		return 0, nil
	}
	exp, label, cont, ok := expectJump(test.Then)
	if !ok {
		return 0, nil
	}
	return 1, func() error {
		guard, err := litGuardOf(g.Imm.(prim.Guard))
		if err != nil {
			return err
		}
		imm := bytecode.FlowLitGuardTestOf{Guard: guard, Expect: s.expectOf(exp)}
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: bytecode.FlowGuardLitJump, Imm: imm}), label, cont)
		return nil
	}
}

// assertJump matches an anchor the position is not at, which leaves:
//
//	if !flow.atAnchor(K) { flow.expect(E, $pos); exit L }
func assertJump(s *flowSel, list []code.Stmt) (int, func() error) {
	test, ok := list[0].(*code.If)
	if !ok || test.Init != nil || test.Else != nil {
		return 0, nil
	}
	a := callOf(not(test.Cond), flowpkg.OpAtAnchor)
	if a == nil {
		a = callOf(not(test.Cond), flowpkg.OpAtEnd)
	}
	if a == nil {
		return 0, nil
	}
	exp, label, cont, ok := expectJump(test.Then)
	if !ok {
		return 0, nil
	}
	return 1, func() error {
		imm := bytecode.FlowAnchorTestOf{Anchor: int(a.Imm.(prim.Anchor)), Expect: s.expectOf(exp)}
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: bytecode.FlowAssertJump, Imm: imm}), label, cont)
		return nil
	}
}

// tested is a call of an operation of two results, the second of which is whether it matched, that
// leaves when it does not match, at the start of a list of statements:
//
//	if v, ok = op(); !ok { exit L }
//
// with the registers of v and ok, and the jump that a failed match takes.
type tested struct {
	call  *code.Call
	dst   []int
	label string
	cont  bool
}

// testedOf returns the call of op that list starts with, which covers one statement.
func testedOf(s *flowSel, list []code.Stmt, op string) (tested, bool) {
	test, ok := list[0].(*code.If)
	if !ok || test.Init == nil || test.Else != nil || len(test.Init.Dst) != 2 || len(test.Init.Values) != 1 {
		return tested{}, false
	}
	c := callOf(test.Init.Values[0], op)
	if c == nil || len(c.Args) != 0 || test.Init.Dst[1].Name == "" || !isLocal(not(test.Cond), test.Init.Dst[1].Name) {
		return tested{}, false
	}
	label, cont, ok := onlyJump(test.Then)
	if !ok {
		return tested{}, false
	}
	dst := make([]int, 2)
	for i, p := range test.Init.Dst {
		if dst[i], ok = s.place(p); !ok {
			return tested{}, false
		}
	}
	return tested{call: c, dst: dst, label: label, cont: cont}, true
}

// testedJump matches a call of op that leaves when it does not match (testedOf), and selects the
// instruction jump with the immediate imm of the call.
func testedJump(s *flowSel, list []code.Stmt, op string, jump bytecode.FlowOp, imm func(*code.Call) (any, error)) (int, func() error) {
	t, ok := testedOf(s, list, op)
	if !ok {
		return 0, nil
	}
	return 1, func() error {
		x, err := imm(t.call)
		if err != nil {
			return err
		}
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: jump, Dst: t.dst, Imm: x}), t.label, t.cont)
		return nil
	}
}

// posOf returns the register of the local that st sets to the position, x = $pos.
func posOf(s *flowSel, st code.Stmt) (string, int, bool) {
	set, ok := st.(*code.Set)
	if !ok || len(set.Dst) != 1 || len(set.Values) != 1 || set.Dst[0].Name == "" {
		return "", 0, false
	}
	if r, isReg := set.Values[0].(*code.Reg); !isReg || r.Name != prim.Pos {
		return "", 0, false
	}
	x, ok := s.place(set.Dst[0])
	return set.Dst[0].Name, x, ok
}

// terminalOf returns the register of the place that st sets to a terminal node from the local start,
// v = flow.newTerminalNode(start, text), and the call text, or false if st is no such statement.
func terminalOf(s *flowSel, st code.Stmt, start string) (int, *code.Call, bool) {
	set, ok := st.(*code.Set)
	if !ok || len(set.Dst) != 1 || len(set.Values) != 1 {
		return 0, nil, false
	}
	c := callOf(set.Values[0], flowpkg.OpNewTerminalNode)
	if c == nil || len(c.Args) != 2 || !isLocal(c.Args[0], start) {
		return 0, nil, false
	}
	text, ok := c.Args[1].(*code.Call)
	if !ok {
		return 0, nil, false
	}
	v, ok := s.place(set.Dst[0])
	return v, text, ok
}

// matchClassNode matches one character of a class (classTestOf) that a terminal node takes:
//
//	ch, size, ok = flow.peek()
//	if !ok || !flow.inClass(C, ch) { flow.expect(E, $pos); exit L }
//	x = $pos
//	flow.advance(size)
//	v = flow.newTerminalNode(x, flow.text(x))
func matchClassNode(s *flowSel, list []code.Stmt) (int, func() error) {
	t, ok := classTestOf(s, list)
	if !ok || len(list) < 5 || t.size == "" {
		return 0, nil
	}
	name, x, ok := posOf(s, list[2])
	if !ok || x < 0 || slices.Contains(t.dst, x) {
		return 0, nil
	}
	if adv, isDo := list[3].(*code.Do); !isDo || adv.Call.Callee != flowpkg.OpAdvance || len(adv.Call.Args) != 1 || !isLocal(adv.Call.Args[0], t.size) {
		return 0, nil
	}
	v, text, ok := terminalOf(s, list[4], name)
	if !ok || text.Callee != flowpkg.OpText || len(text.Args) != 1 || !isLocal(text.Args[0], name) {
		return 0, nil
	}
	return 5, func() error {
		dst := append(slices.Clone(t.dst), x, v)
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: bytecode.FlowMatchClassNode, Dst: dst, Imm: t.imm(s)}), t.label, t.cont)
		return nil
	}
}

// matchLitNode matches a literal that a terminal node takes:
//
//	x = $pos
//	if v, ok = flow.matchLit(L); !ok { exit L }
//	n = flow.newTerminalNode(x, flow.str(S))
func matchLitNode(s *flowSel, list []code.Stmt) (int, func() error) {
	if len(list) < 3 {
		return 0, nil
	}
	name, x, ok := posOf(s, list[0])
	if !ok || x < 0 {
		return 0, nil
	}
	t, ok := testedOf(s, list[1:], flowpkg.OpMatchLit)
	if !ok || slices.Contains(t.dst, x) {
		return 0, nil
	}
	n, str, ok := terminalOf(s, list[2], name)
	if !ok || str.Callee != flowpkg.OpStr || len(str.Args) != 0 {
		return 0, nil
	}
	return 3, func() error {
		imm := bytecode.FlowLitNodeOf{Lit: s.litOf(t.call.Imm.(prim.Lit)), Text: int(s.c.str(string(str.Imm.(prim.Chars))))}
		dst := append([]int{x}, append(slices.Clone(t.dst), n)...)
		s.jumpTo(s.emit(bytecode.FlowInstr{Op: bytecode.FlowMatchLitNode, Dst: dst, Imm: imm}), t.label, t.cont)
		return nil
	}
}

// isInt reports whether e is the int constant n.
func isInt(e code.Expr, n int) bool {
	c, ok := e.(*code.Const)
	if !ok {
		return false
	}
	v, ok := c.Value.(int64)
	return ok && v == int64(n)
}

// onlyStmt returns the statement that the then block of the plain conditional st consists of, and
// the condition; ok is false if st is no such conditional.
func onlyStmt(st code.Stmt) (cond code.Expr, body code.Stmt, ok bool) {
	test, ok := st.(*code.If)
	if !ok || test.Init != nil || test.Else != nil || test.Then == nil || test.Then.Label != "" || len(test.Then.Stmts) != 1 {
		return nil, nil, false
	}
	return test.Cond, test.Then.Stmts[0], true
}

// scanCall returns the call of the scan-site operation op of the statement body, whose arguments are
// the start and the count, the local of the count, and whether body is one.
func scanCall(body code.Stmt, op string) (c *code.Call, count string, ok bool) {
	switch x := body.(type) {
	case *code.Set:
		if len(x.Dst) != 1 || len(x.Values) != 1 {
			return nil, "", false
		}
		c = callOf(x.Values[0], op)
	case *code.Do:
		c = callOf(x.Call, op)
	}
	if c == nil || len(c.Args) != 2 {
		return nil, "", false
	}
	l, ok := c.Args[1].(*code.Local)
	if !ok {
		return nil, "", false
	}
	return c, l.Name, true
}

// guarded splits the condition of a scan-site test into the guard that a walk which resumes its
// run adds (flowlower's resumeRun.off: the walk is no walk of the site then), and the test itself:
//
//	guard && test
//
// The guard is nil for a test without one.
func guarded(cond code.Expr) (guard, test code.Expr) {
	if b, ok := cond.(*code.Binary); ok && b.Op == "&&" {
		return b.X, b.Y
	}
	return nil, cond
}

// emitGuarded emits the instruction that emit emits, behind the guard if there is one: a jump past it
// when the guard is false.
func (s *flowSel) emitGuarded(guard code.Expr, emit func() error) error {
	if guard == nil {
		return emit()
	}
	past, err := s.branch(guard, false)
	if err != nil {
		return err
	}
	if err := emit(); err != nil {
		return err
	}
	s.patch(past, len(s.f.Code))
	return nil
}

// scanReachAt matches the test of a walk of a scan site at the scan hot span, with or without the
// guard of a walk that may resume its run (guarded):
//
//	if [guard &&] count == ScanHotSpan { count = flow.scanReach(S, start, count) }
//
// and selects SCANREACHAT, which compares the count with the span of the parse, behind the guard.
func scanReachAt(s *flowSel, list []code.Stmt) (int, func() error) {
	cond, body, ok := onlyStmt(list[0])
	if !ok {
		return 0, nil
	}
	guard, cond := guarded(cond)
	c, count, ok := scanCall(body, flowpkg.OpScanReach)
	eq, isBin := cond.(*code.Binary)
	if !ok || !isBin || eq.Op != "==" || !isLocal(eq.X, count) || !isInt(eq.Y, flowpkg.ScanHotSpan) {
		return 0, nil
	}
	set, isSet := body.(*code.Set)
	if !isSet || set.Dst[0].Name != count {
		return 0, nil
	}
	dst, ok := s.place(set.Dst[0])
	if !ok {
		return 0, nil
	}
	return 1, func() error {
		return s.emitGuarded(guard, func() error {
			args, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowScanReachAt, Dst: []int{dst}, Args: args, Imm: int(c.Imm.(prim.Site))})
			return nil
		})
	}
}

// scanEndAt matches the end of a value-free walk of a scan site that reached the scan hot span, with
// or without the guard of a walk that may resume its run (guarded):
//
//	if [guard &&] ScanHotSpan-1 < count { flow.scanEnd(S, start, count) }
//
// and selects SCANENDAT, which compares the count with the span of the parse, behind the guard.
func scanEndAt(s *flowSel, list []code.Stmt) (int, func() error) {
	cond, body, ok := onlyStmt(list[0])
	if !ok {
		return 0, nil
	}
	guard, cond := guarded(cond)
	if _, isDo := body.(*code.Do); !isDo {
		return 0, nil
	}
	c, count, ok := scanCall(body, flowpkg.OpScanEnd)
	lt, isBin := cond.(*code.Binary)
	if !ok || !isBin || lt.Op != "<" || !isInt(lt.X, flowpkg.ScanHotSpan-1) || !isLocal(lt.Y, count) {
		return 0, nil
	}
	return 1, func() error {
		return s.emitGuarded(guard, func() error {
			args, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowScanEndAt, Args: args, Imm: int(c.Imm.(prim.Site))})
			return nil
		})
	}
}

// jumpTo makes the instruction at j, which has a target, jump to the end of the block or loop label,
// or to the next iteration of the loop label if cont.
func (s *flowSel) jumpTo(j int, label string, cont bool) {
	if cont {
		s.conts[label] = append(s.conts[label], j)
	} else {
		s.labels[label] = append(s.labels[label], j)
	}
}

// stmts emits the statements of list, each by the pattern of statements that wins at it or by its
// kind. The temporaries of a statement are free again after it.
func (s *flowSel) stmts(list []code.Stmt) error {
	for i := 0; i < len(list); {
		next := s.next
		best, n, emit := -1, 0, func() error { return nil }
		for k := range statementPatterns {
			p := &statementPatterns[k]
			if m, e := p.Stmts(s, list[i:]); m > 0 && (best < 0 || beats(p, &statementPatterns[best])) {
				best, n, emit = k, m, e
			}
		}
		var err error
		if best >= 0 {
			s.c.counts[statementPatterns[best].Name]++
			err = emit()
			i += n
		} else {
			err = s.stmt(list[i])
			i++
		}
		s.next = next
		if err != nil {
			return err
		}
	}
	return nil
}

// beats reports whether the pattern p wins over q, which is declared before it: it covers more
// operations, or as many and saves more instructions.
func beats(p, q *pattern) bool {
	return p.Ops > q.Ops || p.Ops == q.Ops && p.Saved > q.Saved
}

// branch emits code that continues at the jumps it returns when the boolean e is when, and at the
// next instruction otherwise. A negation, || and && become jumps, not values.
func (s *flowSel) branch(e code.Expr, when bool) ([]int, error) {
	if x := not(e); x != nil {
		return s.branch(x, !when)
	}
	if b, ok := e.(*code.Binary); ok && (b.Op == "||" || b.Op == "&&") {
		// The value of the left operand that decides the value of the operator: true for ||.
		decides := b.Op == "||"
		if when == decides {
			left, err := s.branch(b.X, when)
			if err != nil {
				return nil, err
			}
			right, err := s.branch(b.Y, when)
			return append(left, right...), err
		}
		past, err := s.branch(b.X, decides) // the value is decided, and it is not when
		if err != nil {
			return nil, err
		}
		jumps, err := s.branch(b.Y, when)
		s.patch(past, len(s.f.Code))
		return jumps, err
	}
	r, err := s.expr(e)
	if err != nil {
		return nil, err
	}
	op := bytecode.FlowJumpFalse
	if when {
		op = bytecode.FlowJumpTrue
	}
	return []int{s.emit(bytecode.FlowInstr{Op: op, Args: []int{r}})}, nil
}

// patch sets the target of the instructions at jumps to t.
func (s *flowSel) patch(jumps []int, t int) {
	for _, j := range jumps {
		s.f.Code[j].Target = t
	}
}

// superinstructions returns the names of the patterns of statements, in the order of the table: the
// counters of the lowering, which count their matches in a module.
func superinstructions() []string {
	out := make([]string, len(statementPatterns))
	for i, p := range statementPatterns {
		out[i] = p.Name
	}
	return out
}
