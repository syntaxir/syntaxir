package vmlower

import (
	"cmp"
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	flowpkg "syntaxir.dev/sxir/internal/ir/code/flow"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// selectFlows sets in the module the Flow bodies of the functions of the table (Input.Table): the
// body of a rule, its stream body, and the skip and the lines of an Operators Rule. A rule body or
// stream body runs in the rule frame of the VM, whose slots must be the capture layout that the
// plan gave the rule (plandata.Plan.CheckLayout), a line in the frame of the line's scope, whose
// slots must be those of the planned function, and a skip in the frame it is run in, without slots
// of its own. A function that uses an operation that the bytecode Flow bodies do not have is an
// error. The action expression of a predicate of a body is added to the module's action table,
// after the action expressions of the tables. The functions are selected in the order of their
// keys, so the module depends only on the table.
func (c *compiler) selectFlows() error {
	bc, t := c.m, c.in.Table
	plan, fns := t.Plan, t.Module.Module().Funcs
	keys := make([]plandata.Key, len(plan.Funcs))
	for i, s := range plan.Funcs {
		keys[i] = s.Key
	}
	slices.SortFunc(keys, func(a, b plandata.Key) int {
		return cmp.Or(cmp.Compare(a.Use, b.Use), cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Line, b.Line))
	})
	for _, k := range keys {
		i, _ := t.Index(k)
		fn := fns[i]
		if k.Rule < 0 || k.Rule >= len(bc.Rules) {
			return fmt.Errorf("internal error: %s: no rule %d in the module", fn.Name, k.Rule)
		}
		// set stores the body, once it is selected, where the module holds the function's code.
		var set func(f *bytecode.Flow)
		switch k.Use {
		case plandata.RuleBody:
			if err := plan.CheckLayout(k.Rule, fn.Name, c.strNames(bc.Rules[k.Rule].Scope)); err != nil {
				return err
			}
			if spec, ok := plan.Func(k); ok && spec.Form == lowerdata.Flat && bc.Rules[k.Rule].Value != nil {
				// A flat function makes the rule's value itself; the call would shape it again.
				return fmt.Errorf("internal error: %s: the flat function of a rule whose call shapes its value", fn.Name)
			}
			set = func(f *bytecode.Flow) {
				if bc.Flows == nil {
					bc.Flows = map[int]*bytecode.Flow{}
				}
				bc.Flows[k.Rule] = f
			}
		case plandata.StreamBody:
			// The stream body runs in place of the rule body, in its frame.
			if err := plan.CheckLayout(k.Rule, fn.Name, c.strNames(bc.Rules[k.Rule].Scope)); err != nil {
				return err
			}
			set = func(f *bytecode.Flow) {
				if bc.Streams == nil {
					bc.Streams = map[int]*bytecode.Flow{}
				}
				bc.Streams[k.Rule] = f
			}
		case plandata.OperatorsSkip, plandata.OperatorsLine:
			var err error
			if set, err = c.operatorsLine(k, fn.Name, plan); err != nil {
				return err
			}
		default:
			return fmt.Errorf("internal error: %s: a function of use %d has no bytecode Flow body", fn.Name, k.Use)
		}
		f, err := c.flow(fn, k.Rule)
		if err != nil {
			return fmt.Errorf("internal error: %s: %w", fn.Name, err)
		}
		set(f)
	}
	return nil
}

// operatorsLine returns the function that sets the Flow body of the skip or line k of the Operators
// Rule of its rule, named name, in the module, with the shape of the line's value from the Core IR.
// A line's frame has the slots of the line's scope, which must be the slots of the planned function;
// a skip has none.
func (c *compiler) operatorsLine(k plandata.Key, name string, plan *plandata.Plan) (func(*bytecode.Flow), error) {
	o := c.m.Rules[k.Rule].OperatorsRule
	co, ok := c.core.Rules[k.Rule].Body.(*ir.OperatorsRule)
	if o < 0 || !ok {
		return nil, fmt.Errorf("%s: rule %d has no Operators Rule", name, k.Rule)
	}
	spec, ok := plan.Func(k)
	if !ok {
		return nil, fmt.Errorf("%s: the plan has no function of the key %+v", name, k)
	}
	slots := make([]string, len(spec.Slots))
	for i, sl := range spec.Slots {
		slots[i] = sl.Name
	}
	pr := &c.m.OperatorsRules[o]
	if k.Use == plandata.OperatorsSkip {
		if len(slots) != 0 {
			return nil, fmt.Errorf("%s: the skip has slots %v", name, slots)
		}
		return func(f *bytecode.Flow) { pr.SkipFlow = f }, nil
	}
	var line *bytecode.LineInfo
	var shape ir.Shape
	if k.Line < len(pr.Operands) {
		if k.Line >= len(co.Operands) {
			return nil, fmt.Errorf("%s: no operand line %d in the Core IR", name, k.Line)
		}
		line, shape = &pr.Operands[k.Line], co.Operands[k.Line].Shape
	} else {
		id := k.Line - len(pr.Operands)
		for _, ops := range [][]bytecode.OperatorInfo{pr.Prefix, pr.InfixPostfix} {
			for i := range ops {
				if ops[i].ID == id {
					line = &ops[i].Line
				}
			}
		}
		if line == nil || id >= len(co.Ops) {
			return nil, fmt.Errorf("%s: no operator %d", name, id)
		}
		shape = co.Ops[id].Line.Shape
	}
	if got := c.strNames(line.Scope); !slices.Equal(got, slots) && (len(got) != 0 || len(slots) != 0) {
		return nil, fmt.Errorf("internal error: the frame of %s has slots %v, not the planned %v", name, got, slots)
	}
	return func(f *bytecode.Flow) { line.Flow, line.Shape = f, coreShapeOf(shape) }, nil
}

// flow returns the Flow body of the function fn, the flow form of rule index rule.
func (c *compiler) flow(fn *code.Func, rule int) (*bytecode.Flow, error) {
	a := flowpkg.AttrsOf(fn)
	if a.Rule != rule {
		return nil, fmt.Errorf("the function is not the body of rule %d", rule)
	}
	s := &flowSel{c: c, rule: rule, f: &bytecode.Flow{}, regs: map[string]int{}, labels: map[string][]int{}, conts: map[string][]int{}, scopes: map[*prim.Names]int{}}
	// The locals have registers of their own, below the temporaries of the statements, which each
	// statement reuses (stmt).
	// A loop that steps has a register of its own for the position of its last step (loop).
	s.progress = map[string]int{}
	code.Walk(fn.Body, func(st code.Stmt) {
		switch x := st.(type) {
		case *code.Let:
			for _, n := range x.Names {
				s.local(n)
			}
		case *code.Loop:
			if hasStep(x) {
				s.progress[x.Label] = s.local("\x00progress " + x.Label)
			}
		}
	}, nil)
	zeros := map[int]any{} // the zero value of each local, by register
	for _, p := range fn.Locals {
		zero, err := zeroOf(p.Type)
		if err != nil {
			return nil, fmt.Errorf("local %s: %w", p.Name, err)
		}
		zeros[s.local(p.Name)] = zero
	}
	if a.Start != "" {
		r, ok := s.regs[a.Start]
		if !ok {
			return nil, fmt.Errorf("the entry position %s is not a local", a.Start)
		}
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowPos, Dst: []int{r}})
	}
	if err := s.stmts(fn.Body.Stmts); err != nil {
		return nil, err
	}
	if a.Frame {
		// A framed function returns its value and true, or the null value and false, by its own
		// returns; it runs in the rule frame that the VM's rule sets up, which owns its finish.
		if n := len(s.f.Code); n == 0 || !s.f.Code[n-1].Op.Ends() || s.f.Code[n-1].Op == bytecode.FlowJump {
			return nil, fmt.Errorf("the framed function does not end with a return")
		}
		if len(s.labels) > 0 || len(s.conts) > 0 {
			return nil, fmt.Errorf("an exit to a block that does not enclose it")
		}
		coalesceCopies(s.f)
		threadJumps(s.f)
		return s.f, s.zeroLocals(zeros)
	}
	// The body of a local function is the block of the fail label: running to its end succeeds with
	// the result, and an exit to the label fails. Both returns are emitted after the body.
	if _, ok := s.regs[a.Result]; !ok {
		return nil, fmt.Errorf("the result %q is not a local", a.Result)
	}
	if err := s.stmts([]code.Stmt{&code.Return{Values: []code.Expr{&code.Local{Name: a.Result}, code.BoolC(true)}}}); err != nil {
		return nil, err
	}
	failAt := len(s.f.Code)
	if err := s.stmts([]code.Stmt{&code.Return{Values: []code.Expr{&code.Const{}, code.BoolC(false)}}}); err != nil {
		return nil, err
	}
	for _, j := range s.labels[a.Fail] {
		s.f.Code[j].Target = failAt
	}
	delete(s.labels, a.Fail)
	if len(s.labels) > 0 || len(s.conts) > 0 {
		return nil, fmt.Errorf("an exit to a block that does not enclose it")
	}
	coalesceCopies(s.f)
	threadJumps(s.f)
	return s.f, s.zeroLocals(zeros)
}

// zeroLocals writes, at the start of the body, the zero value of each local that some path reads
// before the body writes it (bytecode.Flow.Unassigned), and of no other: a function declares its
// locals with their zero values, and the VM does not clear a frame of registers.
func (s *flowSel) zeroLocals(zeros map[int]any) error {
	regs := s.f.Unassigned()
	init := make([]bytecode.FlowInstr, len(regs))
	for i, r := range regs {
		zero, ok := zeros[r]
		if !ok {
			return fmt.Errorf("register %d is read before it is written", r)
		}
		init[i] = bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{r}, Imm: zero}
	}
	for i := range s.f.Code {
		if sh, _ := s.f.Code[i].Op.Shape(); sh.Target {
			s.f.Code[i].Target += len(init)
		}
	}
	s.f.Code = append(init, s.f.Code...)
	return nil
}

// zeroOf returns the zero value of a local of type t in the Flow body's registers.
func zeroOf(t code.Type) (any, error) {
	switch t.Kind {
	case code.KBool:
		return false, nil
	case code.KInt:
		return 0, nil
	case code.KHandle:
		return nil, nil
	}
	return nil, fmt.Errorf("a local of type %s has no bytecode Flow register", t)
}

// flowSel selects the instructions of one Flow function: regs maps its locals to registers, and
// labels holds the jumps that wait for the end of each block or loop they exit, and conts those that
// wait for the next iteration of each loop they continue.
type flowSel struct {
	c      *compiler
	rule   int // the rule the function belongs to
	f      *bytecode.Flow
	regs   map[string]int
	labels map[string][]int
	conts  map[string][]int
	// scopes holds the entry of the module's scope table of each scope of the function's frames.
	scopes map[*prim.Names]int
	// next is the first register that no local and no live temporary holds.
	next int
	// progress holds the register of the step of each loop whose body steps (hasStep), by label, and
	// loops the labels of the loops that enclose the current statement, innermost last.
	progress map[string]int
	loops    []string
}

// hasStep reports whether the loop x steps: whether a statement of its body is a call of flow.step.
// The lowering puts the step of a loop in its body, not in a block within it.
func hasStep(x *code.Loop) bool {
	return slices.ContainsFunc(x.Body.Stmts, func(st code.Stmt) bool {
		d, ok := st.(*code.Do)
		return ok && d.Call.Callee == flowpkg.OpStep
	})
}

// scope returns the entry of the module's scope table that holds the names n, adding it on first use.
func (s *flowSel) scope(n *prim.Names) int {
	if sc, ok := s.scopes[n]; ok {
		return sc
	}
	s.c.m.Scopes = append(s.c.m.Scopes, s.c.strList(n.List))
	sc := len(s.c.m.Scopes) - 1
	s.scopes[n] = sc
	return sc
}

// reg returns a free register for a temporary: one above the locals and the temporaries of the
// statements that enclose the current one.
func (s *flowSel) reg() int {
	r := s.next
	s.next++
	s.f.Regs = max(s.f.Regs, s.next)
	return r
}

// emit appends in. An operation whose results are all unused (a call of a statement) gets
// destinations that discard them, so that every instruction names the registers of its shape.
func (s *flowSel) emit(in bytecode.FlowInstr) int {
	if sh, _ := in.Op.Shape(); len(in.Dst) == 0 && sh.Dst > 0 {
		in.Dst = slices.Repeat([]int{-1}, sh.Dst)
	}
	s.f.Code = append(s.f.Code, in)
	return len(s.f.Code) - 1
}

// local returns the register of the local name, which keeps it for the whole function.
func (s *flowSel) local(name string) int {
	if r, ok := s.regs[name]; ok {
		return r
	}
	if s.next != len(s.regs) {
		panic("internal error: a local declared after a temporary")
	}
	r := s.reg()
	s.regs[name] = r
	return r
}

func (s *flowSel) block(b *code.Block) error {
	if b == nil {
		return nil
	}
	if err := s.stmts(b.Stmts); err != nil {
		return err
	}
	if b.Label != "" {
		end := len(s.f.Code)
		for _, j := range s.labels[b.Label] {
			s.f.Code[j].Target = end
		}
		delete(s.labels, b.Label)
	}
	return nil
}

// stmt emits the statement st. The temporaries it takes are free again after it: a temporary holds
// a value from its definition to its use within one statement.
func (s *flowSel) stmt(st code.Stmt) error {
	defer func(next int) { s.next = next }(s.next)
	switch x := st.(type) {
	case *code.Block:
		return s.block(x)
	case *code.Comment:
		return nil
	case *code.Exit:
		j := s.emit(bytecode.FlowInstr{Op: bytecode.FlowJump})
		s.labels[x.Label] = append(s.labels[x.Label], j)
		return nil
	case *code.Break:
		j := s.emit(bytecode.FlowInstr{Op: bytecode.FlowJump})
		s.labels[x.Label] = append(s.labels[x.Label], j)
		return nil
	case *code.Continue:
		j := s.emit(bytecode.FlowInstr{Op: bytecode.FlowJump})
		s.conts[x.Label] = append(s.conts[x.Label], j)
		return nil
	case *code.Loop:
		return s.loop(x)
	case *code.Return:
		if len(x.Values) != 2 {
			return fmt.Errorf("a return of %d values has no bytecode Flow form", len(x.Values))
		}
		v, err := s.expr(x.Values[0])
		if err != nil {
			return err
		}
		ok, err := s.expr(x.Values[1])
		if err != nil {
			return err
		}
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowReturn, Args: []int{v, ok}})
		return nil
	case *code.Do:
		return s.call(x.Call, nil)
	case *code.If:
		if x.Init != nil {
			if err := s.set(x.Init); err != nil {
				return err
			}
		}
		// The temporaries of the condition are dead once it has jumped.
		next := s.next
		jf, err := s.branch(x.Cond, false)
		if err != nil {
			return err
		}
		s.next = next
		if err := s.block(x.Then); err != nil {
			return err
		}
		if x.Else == nil {
			s.patch(jf, len(s.f.Code))
			return nil
		}
		jend := s.emit(bytecode.FlowInstr{Op: bytecode.FlowJump})
		s.patch(jf, len(s.f.Code))
		if err := s.block(x.Else); err != nil {
			return err
		}
		s.f.Code[jend].Target = len(s.f.Code)
		return nil
	case *code.Let:
		dst := make([]int, len(x.Names))
		for i, n := range x.Names {
			dst[i] = s.local(n)
		}
		return s.assign(dst, x.Value)
	case *code.Set:
		return s.set(x)
	}
	return fmt.Errorf("statement %T has no bytecode Flow form", st)
}

// loop emits the loop x: Init, then at the top the check of Cond, the body, and Post, which a
// Continue reaches, before the jump back to the top. A Break or an Exit to its label leaves it. A
// loop that steps first sets the register of its step to -1, below every position, so that its first
// step passes and each later one requires the position to have advanced (bytecode.FlowStep).
func (s *flowSel) loop(x *code.Loop) error {
	if x.Init != nil {
		if err := s.set(x.Init); err != nil {
			return err
		}
	}
	if r, ok := s.progress[x.Label]; ok {
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{r}, Imm: -1})
	}
	s.loops = append(s.loops, x.Label)
	defer func() { s.loops = s.loops[:len(s.loops)-1] }()
	top := len(s.f.Code)
	var jf []int
	if x.Cond != nil {
		next := s.next
		var err error
		if jf, err = s.branch(x.Cond, false); err != nil {
			return err
		}
		s.next = next
	}
	if err := s.block(x.Body); err != nil {
		return err
	}
	post := len(s.f.Code)
	if x.Post != nil {
		if err := s.set(x.Post); err != nil {
			return err
		}
	}
	s.emit(bytecode.FlowInstr{Op: bytecode.FlowJump, Target: top})
	end := len(s.f.Code)
	s.patch(jf, end)
	for _, j := range s.conts[x.Label] {
		s.f.Code[j].Target = post
	}
	for _, j := range s.labels[x.Label] {
		s.f.Code[j].Target = end
	}
	delete(s.conts, x.Label)
	delete(s.labels, x.Label)
	return nil
}

// set assigns the places of x: with a call of as many results as places, its results, otherwise
// one value per place, all evaluated before the first assignment.
func (s *flowSel) set(x *code.Set) error {
	dst := make([]int, len(x.Dst))
	for i, p := range x.Dst {
		switch {
		case p.Reg && p.Name == prim.Pos:
			dst[i] = posDst
		case p.Reg && p.Name == prim.Cut:
			dst[i] = cutDst
		case p.Reg:
			return fmt.Errorf("an assignment to register %s has no bytecode Flow form", p.Name)
		case p.Name == "":
			dst[i] = -1
		default:
			r, ok := s.regs[p.Name]
			if !ok {
				return fmt.Errorf("assignment to undeclared local %s", p.Name)
			}
			dst[i] = r
		}
	}
	if len(x.Values) == 1 {
		return s.assign(dst, x.Values[0])
	}
	if len(x.Values) != len(dst) {
		return fmt.Errorf("assignment of %d values to %d places", len(x.Values), len(dst))
	}
	// A value goes straight into the register of its local unless a later value reads the local,
	// which must see the value before the assignment. The position and the cut flag are set after
	// every value, since a value may read them.
	tmp := make([]int, len(x.Values))
	for i, v := range x.Values {
		if d := dst[i]; d >= 0 && !slices.ContainsFunc(x.Values[i+1:], func(e code.Expr) bool { return reads(e, x.Dst[i].Name) }) {
			if err := s.exprTo(v, d); err != nil {
				return err
			}
			tmp[i] = d
			continue
		}
		r, err := s.expr(v)
		if err != nil {
			return err
		}
		if l, ok := v.(*code.Local); ok && slices.ContainsFunc(x.Dst, func(p code.Place) bool { return !p.Reg && p.Name == l.Name }) {
			// The local is assigned by this statement too, which must not change the value read.
			t := s.reg()
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{t}, Args: []int{r}})
			r = t
		}
		tmp[i] = r
	}
	for i, d := range dst {
		if d != tmp[i] {
			s.store(d, tmp[i])
		}
	}
	return nil
}

// reads reports whether the expression e reads the local name.
func reads(e code.Expr, name string) bool {
	switch x := e.(type) {
	case *code.Local:
		return x.Name == name
	case *code.Unary:
		return reads(x.X, name)
	case *code.Binary:
		return reads(x.X, name) || reads(x.Y, name)
	case *code.Convert:
		return reads(x.X, name)
	case *code.Call:
		return x.Fn != nil && reads(x.Fn, name) || slices.ContainsFunc(x.Args, func(a code.Expr) bool { return reads(a, name) })
	}
	return false
}

// readsReg reports whether the expression e reads a local whose register is r.
func (s *flowSel) readsReg(e code.Expr, r int) bool {
	for name, reg := range s.regs {
		if reg == r && reads(e, name) {
			return true
		}
	}
	return false
}

// posDst and cutDst are the destinations of an assignment to the position register and to the cut
// flag register.
const (
	posDst = -2
	cutDst = -3
)

// store assigns the register r to the destination d (-1 discards it, posDst sets the position and
// cutDst the cut flag).
func (s *flowSel) store(d, r int) {
	switch {
	case d == posDst:
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowSetPos, Args: []int{r}})
	case d == cutDst:
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowSetCut, Args: []int{r}})
	case d >= 0:
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{d}, Args: []int{r}})
	}
}

// assign stores the value of e in the registers dst (-1 discards one): a call with as many
// results as registers stores its results, and any other expression one value.
func (s *flowSel) assign(dst []int, e code.Expr) error {
	if c, ok := e.(*code.Call); ok {
		return s.call(c, dst)
	}
	if len(dst) != 1 {
		return fmt.Errorf("an expression of one value assigned to %d places", len(dst))
	}
	if dst[0] >= 0 {
		return s.exprTo(e, dst[0])
	}
	r, err := s.expr(e)
	if err != nil {
		return err
	}
	s.store(dst[0], r)
	return nil
}

// exprTo evaluates e into the register d of a local. The instruction that makes the value writes d
// itself, after it has read its arguments, so no copy follows it; only the operators || and &&,
// whose value is written before the right operand is read, go through a temporary.
func (s *flowSel) exprTo(e code.Expr, d int) error {
	switch x := e.(type) {
	case *code.Local:
		r, ok := s.regs[x.Name]
		if !ok {
			return fmt.Errorf("read of undeclared local %s", x.Name)
		}
		if r != d {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{d}, Args: []int{r}})
		}
		return nil
	case *code.Const, *code.Reg:
		_, err := s.value(e, d)
		return err
	case *code.Unary:
		_, err := s.value(e, d)
		return err
	case *code.Call:
		// The selection of a call may write its results before its last instruction reads the
		// arguments, so a call that reads d goes through a temporary.
		if !s.readsReg(x, d) {
			return s.call(x, []int{d})
		}
	case *code.Binary:
		if x.Op != "||" && x.Op != "&&" {
			_, err := s.binaryTo(x, d)
			return err
		}
	}
	r, err := s.expr(e)
	if err != nil {
		return err
	}
	s.store(d, r)
	return nil
}

// expr evaluates e and returns the register that holds its value: the register of a local it reads,
// or a temporary.
func (s *flowSel) expr(e code.Expr) (int, error) {
	switch x := e.(type) {
	case *code.Local:
		r, ok := s.regs[x.Name]
		if !ok {
			return 0, fmt.Errorf("read of undeclared local %s", x.Name)
		}
		return r, nil
	case *code.Call:
		r := s.reg()
		if err := s.call(x, []int{r}); err != nil {
			return 0, err
		}
		return r, nil
	case *code.Binary:
		return s.binaryTo(x, -1)
	}
	return s.value(e, -1)
}

// value evaluates the constant, register or negation e into the register d, or into a temporary if d
// is -1, and returns that register.
func (s *flowSel) value(e code.Expr, d int) (int, error) {
	switch x := e.(type) {
	case *code.Const:
		var v any
		switch val := x.Value.(type) {
		case nil:
		case bool:
			v = val
		case int64:
			v = int(val)
		default:
			return 0, fmt.Errorf("constant %v of type %s has no bytecode Flow form", val, x.Type)
		}
		d = s.dst(d)
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowConst, Dst: []int{d}, Imm: v})
		return d, nil
	case *code.Reg:
		op := bytecode.FlowPos
		switch x.Name {
		case prim.Pos:
		case prim.Cut:
			op = bytecode.FlowCut
		default:
			return 0, fmt.Errorf("register %s has no bytecode Flow form", x.Name)
		}
		d = s.dst(d)
		s.emit(bytecode.FlowInstr{Op: op, Dst: []int{d}})
		return d, nil
	case *code.Unary:
		if x.Op != "!" {
			return 0, fmt.Errorf("unary %s has no bytecode Flow form", x.Op)
		}
		a, err := s.expr(x.X)
		if err != nil {
			return 0, err
		}
		d = s.dst(d)
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowNot, Dst: []int{d}, Args: []int{a}})
		return d, nil
	}
	return 0, fmt.Errorf("expression %T has no bytecode Flow form", e)
}

// dst returns d, or a temporary if d is -1.
func (s *flowSel) dst(d int) int {
	if d < 0 {
		return s.reg()
	}
	return d
}

// binaryOps holds the instruction of each binary operator, and whether it takes its operands swapped.
var binaryOps = map[string]struct {
	op   bytecode.FlowOp
	swap bool
}{
	"+": {bytecode.FlowAdd, false}, "-": {bytecode.FlowSub, false},
	"==": {bytecode.FlowEq, false}, "!=": {bytecode.FlowNe, false},
	"<": {bytecode.FlowLt, false}, "<=": {bytecode.FlowLe, false},
	">": {bytecode.FlowLt, true}, ">=": {bytecode.FlowLe, true},
}

// binaryTo evaluates x into the register d, or into a temporary if d is -1, and returns that
// register. || and && evaluate their right operand only when the left one does not decide the value;
// their value is always a temporary, which they write before the right operand is read.
func (s *flowSel) binaryTo(x *code.Binary, d int) (int, error) {
	if x.Op == "||" || x.Op == "&&" {
		l, err := s.expr(x.X)
		if err != nil {
			return 0, err
		}
		r := s.reg()
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{r}, Args: []int{l}})
		test := r
		if x.Op == "||" {
			test = s.reg()
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowNot, Dst: []int{test}, Args: []int{r}})
		}
		jf := s.emit(bytecode.FlowInstr{Op: bytecode.FlowJumpFalse, Args: []int{test}})
		y, err := s.expr(x.Y)
		if err != nil {
			return 0, err
		}
		s.emit(bytecode.FlowInstr{Op: bytecode.FlowMove, Dst: []int{r}, Args: []int{y}})
		s.f.Code[jf].Target = len(s.f.Code)
		return r, nil
	}
	b, ok := binaryOps[x.Op]
	if !ok {
		return 0, fmt.Errorf("binary %s has no bytecode Flow form", x.Op)
	}
	l, err := s.expr(x.X)
	if err != nil {
		return 0, err
	}
	y, err := s.expr(x.Y)
	if err != nil {
		return 0, err
	}
	if b.swap {
		l, y = y, l
	}
	d = s.dst(d)
	s.emit(bytecode.FlowInstr{Op: b.op, Dst: []int{d}, Args: []int{l, y}})
	return d, nil
}

// call emits the operation c with its results in dst (nil for none). Each operation selected
// here is one Flow operation whose charges and effects its VM body reproduces.
func (s *flowSel) call(c *code.Call, dst []int) error {
	for _, d := range dst {
		if d == posDst || d == cutDst {
			return fmt.Errorf("an operation that assigns a register has no bytecode Flow form")
		}
	}
	i, ok := opIndex[c.Callee]
	if !ok {
		return fmt.Errorf("operation %s has no bytecode Flow form", c.Callee)
	}
	return opPatterns[i].Sel(s, c, dst)
}

// args evaluates the expressions es and returns the registers of their values.
func (s *flowSel) args(es []code.Expr) ([]int, error) {
	out := make([]int, len(es))
	for i, e := range es {
		r, err := s.expr(e)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// op emits the instruction of operation op for the call c, whose arguments are its registers, with
// the immediate imm and the results in dst.
func (s *flowSel) op(op bytecode.FlowOp, c *code.Call, dst []int, imm any) error {
	sh, _ := op.Shape()
	if len(c.Args) != sh.Args {
		return fmt.Errorf("%s with %d arguments", c.Callee, len(c.Args))
	}
	if len(dst) != 0 && len(dst) != sh.Dst {
		return fmt.Errorf("%s with %d results", c.Callee, len(dst))
	}
	var a []int // nil for none, as a module file reads it back
	for _, e := range c.Args {
		r, err := s.expr(e)
		if err != nil {
			return err
		}
		a = append(a, r)
	}
	s.emit(bytecode.FlowInstr{Op: op, Dst: dst, Args: a, Imm: imm})
	return nil
}

// actionSite adds the action code of the action site i to the module's action table, translated for
// the frame whose slots are names, the scope the predicate or binding runs in, and returns its index.
func (s *flowSel) actionSite(i int, names []string) (int, error) {
	slots := make([]ir.Slot, len(names))
	for j, n := range names {
		slots[j] = ir.Slot{Name: n}
	}
	code, err := s.c.actionCode(actionSite{site: i, scope: slots})
	if err != nil {
		return 0, err
	}
	s.c.m.Actions = append(s.c.m.Actions, code)
	return len(s.c.m.Actions) - 1, nil
}

// shapeOf returns the immediate of the shape sh.
func shapeOf(sh prim.Shape) bytecode.FlowShapeOf {
	return bytecode.FlowShapeOf{Elems: sh.Elems, Shared: sh.Shared, Tuple: sh.Tuple, K: sh.K, Mode: int(sh.Mode)}
}

// coreShapeOf returns the immediate of the shape sh of Core IR, that of a rule or a line of an
// Operators Rule, which the tables of the module take from the Core module.
func coreShapeOf(sh ir.Shape) bytecode.FlowShapeOf {
	return shapeOf(prim.Shape{Elems: sh.Elems, K: sh.K, Mode: prim.SingleMode(sh.Mode), Tuple: sh.Tuple, Shared: sh.Shared})
}

// expID returns the parser's ID of the expectation id of the Flow module: the fixed expectations
// keep their IDs, and the others are the strings of the bytecode module after the fixed table.
func (s *flowSel) expID(id prim.ExpID) int {
	if id <= prim.ExpID(program.IDEndLine) {
		return int(id)
	}
	return int(program.NumFixedDescs) + int(s.c.exp(id))
}

// strNames returns the strings of the module at the indexes ids.
func (c *compiler) strNames(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = c.m.Strings[id]
	}
	return out
}

// ruleName returns the name of the rule of index i in the module.
func (c *compiler) ruleName(i int) string {
	if i < 0 || i >= len(c.m.Rules) {
		return ""
	}
	return c.m.Strings[c.m.Rules[i].Name]
}
