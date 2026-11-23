// Package actionlower lowers the action expressions of a checked program, the predicates and the
// values of bindings to the action programs that the runtime evaluates (state.ActionProgram): it lowers
// a Core IR action expression to Exec IR (internal/middleend/actionlower) and translates the function into the instructions of the
// register machine of action expressions. The closure backend and the bytecode compiler (vmlower) call it when
// they build a program; the runtime only runs the result.
package actionlower

import (
	"fmt"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Env is what the leaves of an action expression resolve against when it is compiled.
type Env struct {
	// Checker resolves the types of the program: the kinds of the values that action expressions make and the
	// types that casts check. Vars holds the program's declared variables (check.Model.Vars).
	Checker *check.Checker
	Vars    *check.VarTable
	// Scope holds the capture slots of the frame the action expression reads.
	Scope *program.Scope
	// Proj are the projected captures of the rule whose action the action expression is (Options.Proj of internal/middleend/actionlower),
	// nil for every other action expression.
	Proj map[string]string
}

// The register files.
const (
	fileA = iota // values: Datum, Obj, Fail
	fileN        // int, bool
	fileS        // Seq
	fileT        // Str
)

type actionReg struct {
	file int
	r    int32
}

// Compile compiles the Core IR action expression t, which reads the frame of env.Scope: it lowers t to Exec IR
// and translates the function into the instructions of a state.ActionProgram. An error is an internal
// error: an action expression that the front end does not build, or a lowering this translation does not know.
//
// The action expression counts logical compute units (spec/runtime/parsing.md#compute-units): the elements that
// map, filter and the folds visit (seq.step), and the values that the plain evaluation builds and
// the lowering does not (units.add, units.items, units.since), but no step of its own evaluation.
func Compile(t ir.ActionExpr, env Env) (*state.ActionProgram, error) {
	l, err := actionlower.Lower(t, actionlower.Options{Visits: true, Proj: env.Proj})
	if err != nil {
		return nil, err
	}
	c := &actionCompiler{env: env, m: l.Module, locals: map[string]actionReg{}, prog: &state.ActionProgram{}}
	for _, p := range l.Func.Locals {
		if _, err := c.declare(p.Name, p.Type); err != nil {
			return nil, fmt.Errorf("core: action %s: %w", ir.ActionExprString(t), err)
		}
	}
	if err := c.block(l.Func.Body); err != nil {
		return nil, fmt.Errorf("core: action %s: %w", ir.ActionExprString(t), err)
	}
	return c.prog, nil
}

type actionCompiler struct {
	env    Env
	m      *code.Module
	prog   *state.ActionProgram
	locals map[string]actionReg
}

func fileOf(t code.Type) (int, bool) {
	switch t.Kind {
	case code.KBool, code.KInt:
		return fileN, true
	case code.KHandle:
		switch t.Handle {
		case exec.Datum, action.Obj, action.Fail:
			return fileA, true
		case action.Seq:
			return fileS, true
		case action.Str:
			return fileT, true
		}
	}
	return 0, false
}

func (c *actionCompiler) reg(file int) actionReg {
	p := c.prog
	var n *int32
	switch file {
	case fileA:
		n = &p.NA
	case fileN:
		n = &p.NN
	case fileS:
		n = &p.NS
	default:
		n = &p.NT
	}
	r := actionReg{file, *n}
	*n++
	return r
}

// declare returns the register of the local name of type t, taking one in the file of t on first
// use. A name declared again in another file is an error: an instruction would write one file and
// its reader read the other.
func (c *actionCompiler) declare(name string, t code.Type) (actionReg, error) {
	f, ok := fileOf(t)
	if !ok {
		return actionReg{}, fmt.Errorf("local %s of type %s", name, t)
	}
	if r, ok := c.locals[name]; ok {
		if r.file != f {
			return actionReg{}, fmt.Errorf("local %s of type %s declared in another register file", name, t)
		}
		return r, nil
	}
	r := c.reg(f)
	c.locals[name] = r
	return r, nil
}

func (c *actionCompiler) emit(in state.ActionInstr) int {
	code := c.prog.Code
	k := len(code)
	switch {
	case in.Op == state.TCover && in.Dst == in.C && k >= 1 && code[k-1].Op == state.TSetField &&
		code[k-1].A == in.A && code[k-1].B == in.B && !c.jumpsTo(int32(k)):
		// A field of a struct literal: the field and the cover of its value, in one instruction. No
		// jump goes to the cover, which follows the field at once.
		code[k-1] = state.ActionInstr{Op: state.TField, Dst: in.Dst, A: in.A, B: in.B, C: code[k-1].C}
		return k - 1
	case in.Op == state.TRet && k >= 1 && code[k-1].Op == state.TMoveA && code[k-1].Dst == in.A && !c.jumpsTo(int32(k)):
		// The return of a register moved into another just before: return the source.
		code[k-1] = state.ActionInstr{Op: state.TRet, A: code[k-1].A}
		return k - 1
	}
	c.prog.Code = append(code, in)
	return k
}

// counted returns the index and the list of the loop x, which must count over the elements of a list:
//
//	for i < seq.len(items); i = i + 1 { body }
//
// with neither i nor items assigned in the body. It is the one loop that the lowering of action
// expressions makes (an iteration of map, filter, foldl or foldr), and the only one that action code
// has: TLoop and TEndLoop run it over the elements counted when it starts, so that every evaluation
// of action code ends (state.checkActionLoops).
func (c *actionCompiler) counted(x *code.Loop) (index, items actionReg, err error) {
	bad := func() (actionReg, actionReg, error) {
		return actionReg{}, actionReg{}, fmt.Errorf("a loop that does not count over the elements of a list")
	}
	lt, ok := x.Cond.(*code.Binary)
	if x.Init != nil || !ok || lt.Op != "<" {
		return bad()
	}
	i, ok := lt.X.(*code.Local)
	n, isCall := lt.Y.(*code.Call)
	if !ok || !isCall || n.Callee != "seq.len" || len(n.Args) != 1 {
		return bad()
	}
	l, ok := n.Args[0].(*code.Local)
	if !ok || x.Post == nil || len(x.Post.Dst) != 1 || x.Post.Dst[0].Reg || x.Post.Dst[0].Name != i.Name || len(x.Post.Values) != 1 {
		return bad()
	}
	inc, ok := x.Post.Values[0].(*code.Binary)
	if !ok || inc.Op != "+" {
		return bad()
	}
	if v, isLocal := inc.X.(*code.Local); !isLocal || v.Name != i.Name {
		return bad()
	}
	if one, isConst := inc.Y.(*code.Const); !isConst || one.Value != int64(1) {
		return bad()
	}
	assigned := false
	code.Walk(x.Body, func(st code.Stmt) {
		switch st := st.(type) {
		case *code.Set:
			for _, p := range st.Dst {
				assigned = assigned || p.Name == i.Name || p.Name == l.Name
			}
		case *code.Let:
			for _, name := range st.Names {
				assigned = assigned || name == i.Name || name == l.Name
			}
		}
	}, nil)
	index, okI := c.locals[i.Name]
	items, okL := c.locals[l.Name]
	if assigned || !okI || !okL || index.file != fileN || items.file != fileS {
		return bad()
	}
	return index, items, nil
}

// jumpsTo reports whether a jump of the code emitted so far goes to the instruction at pc.
func (c *actionCompiler) jumpsTo(pc int32) bool {
	for _, in := range c.prog.Code {
		if (in.Op == state.TJump || in.Op == state.TJumpF || in.Op == state.TLoop || in.Op == state.TEndLoop) && in.Dst == pc {
			return true
		}
	}
	return false
}

// results returns the result types of the operation or function callee.
func (c *actionCompiler) results(callee string) []code.Type {
	if o := c.m.Op(callee); o != nil {
		return o.Results
	}
	if f := c.m.Func(callee); f != nil {
		return f.Results
	}
	return nil
}

func (c *actionCompiler) block(b *code.Block) error {
	for i := 0; i < len(b.Stmts); i++ {
		next := func() code.Stmt {
			if i+1 < len(b.Stmts) {
				return b.Stmts[i+1]
			}
			return nil
		}
		switch s := b.Stmts[i].(type) {
		case *code.Let:
			call, isCall := s.Value.(*code.Call)
			if isCall {
				res := c.results(call.Callee)
				if len(res) == len(s.Names) && len(res) > 0 && res[len(res)-1] == code.THandle(action.Fail) {
					// A composition that can fail: its failure must be raised right after it.
					fail := s.Names[len(s.Names)-1]
					if !raises(next(), fail) {
						return fmt.Errorf("the failure %s of %s is not returned right after it", fail, call.Callee)
					}
					i++
					var dst actionReg
					if len(s.Names) == 2 {
						var err error
						if dst, err = c.declare(s.Names[0], res[0]); err != nil {
							return err
						}
					}
					if err := c.fallible(call, dst); err != nil {
						return err
					}
					continue
				}
			}
			if len(s.Names) != 1 {
				return fmt.Errorf("let of %d names", len(s.Names))
			}
			t, err := c.typeOf(s.Value)
			if err != nil {
				return err
			}
			dst, err := c.declare(s.Names[0], t)
			if err != nil {
				return err
			}
			if err := c.exprInto(s.Value, dst); err != nil {
				return err
			}
		case *code.Set:
			if len(s.Dst) != 1 || len(s.Values) != 1 || s.Dst[0].Reg {
				return fmt.Errorf("set of %d places", len(s.Dst))
			}
			dst, ok := c.locals[s.Dst[0].Name]
			if !ok {
				return fmt.Errorf("set of undeclared %s", s.Dst[0].Name)
			}
			if err := c.exprInto(s.Values[0], dst); err != nil {
				return err
			}
		case *code.Do:
			res := c.results(s.Call.Callee)
			if len(res) == 1 && res[0] == code.THandle(action.Fail) {
				return fmt.Errorf("the failure of %s is discarded", s.Call.Callee)
			}
			if err := c.effect(s.Call); err != nil {
				return err
			}
		case *code.If:
			if s.Init != nil {
				return fmt.Errorf("if with an init")
			}
			cond, err := c.expr(s.Cond)
			if err != nil {
				return err
			}
			jf := c.emit(state.ActionInstr{Op: state.TJumpF, A: cond.r})
			if err := c.block(s.Then); err != nil {
				return err
			}
			if s.Else != nil {
				j := c.emit(state.ActionInstr{Op: state.TJump})
				c.prog.Code[jf].Dst = int32(len(c.prog.Code))
				if err := c.block(s.Else); err != nil {
					return err
				}
				c.prog.Code[j].Dst = int32(len(c.prog.Code))
			} else {
				c.prog.Code[jf].Dst = int32(len(c.prog.Code))
			}
		case *code.Loop:
			index, items, err := c.counted(s)
			if err != nil {
				return err
			}
			count := c.reg(fileN)
			loop := c.emit(state.ActionInstr{Op: state.TLoop, A: items.r, B: index.r, C: count.r})
			if err := c.block(s.Body); err != nil {
				return err
			}
			c.emit(state.ActionInstr{Op: state.TEndLoop, Dst: int32(loop + 1), B: index.r, C: count.r})
			c.prog.Code[loop].Dst = int32(len(c.prog.Code))
		case *code.Return:
			if len(s.Values) != 2 {
				return fmt.Errorf("return of %d values", len(s.Values))
			}
			if isNull(s.Values[1]) {
				v, err := c.expr(s.Values[0])
				if err != nil {
					return err
				}
				c.emit(state.ActionInstr{Op: state.TRet, A: v.r})
				continue
			}
			if !isNull(s.Values[0]) {
				return fmt.Errorf("return of a value and a failure")
			}
			f, err := c.expr(s.Values[1])
			if err != nil {
				return err
			}
			c.emit(state.ActionInstr{Op: state.TRetErr, A: f.r})
		case *code.Comment:
		default:
			return fmt.Errorf("statement %T", s)
		}
	}
	return nil
}

func isNull(e code.Expr) bool {
	k, ok := e.(*code.Const)
	return ok && k.Type.Kind == code.KHandle && k.Value == nil
}

// raises reports whether s is the return of the failure in the local fail when it is not null, as
// the lowering of action expressions follows a call that can fail.
func raises(s code.Stmt, fail string) bool {
	f, ok := s.(*code.If)
	if !ok || f.Init != nil || f.Else != nil || len(f.Then.Stmts) != 1 {
		return false
	}
	cond, ok := f.Cond.(*code.Binary)
	if !ok || cond.Op != "!=" || !isLocal(cond.X, fail) || !isNull(cond.Y) {
		return false
	}
	r, ok := f.Then.Stmts[0].(*code.Return)
	return ok && len(r.Values) == 2 && isNull(r.Values[0]) && isLocal(r.Values[1], fail)
}

func isLocal(e code.Expr, name string) bool {
	l, ok := e.(*code.Local)
	return ok && l.Name == name
}

// typeOf returns the type of the expression e of one value.
func (c *actionCompiler) typeOf(e code.Expr) (code.Type, error) {
	switch e := e.(type) {
	case *code.Const:
		return e.Type, nil
	case *code.Local:
		r, ok := c.locals[e.Name]
		if !ok {
			return code.Type{}, fmt.Errorf("undeclared local %s", e.Name)
		}
		switch r.file {
		case fileA:
			return code.THandle(exec.Datum), nil
		case fileN:
			return code.TInt, nil
		case fileS:
			return code.THandle(action.Seq), nil
		default:
			return code.THandle(action.Str), nil
		}
	case *code.Unary:
		return code.TBool, nil
	case *code.Binary:
		if e.Op == "+" {
			return code.TInt, nil
		}
		return code.TBool, nil
	case *code.Call:
		res := c.results(e.Callee)
		if len(res) != 1 {
			return code.Type{}, fmt.Errorf("%s has %d results", e.Callee, len(res))
		}
		return res[0], nil
	}
	return code.Type{}, fmt.Errorf("expression %T", e)
}

// expr translates e and returns the register that holds its value.
func (c *actionCompiler) expr(e code.Expr) (actionReg, error) {
	if l, ok := e.(*code.Local); ok {
		r, ok := c.locals[l.Name]
		if !ok {
			return actionReg{}, fmt.Errorf("undeclared local %s", l.Name)
		}
		return r, nil
	}
	t, err := c.typeOf(e)
	if err != nil {
		return actionReg{}, err
	}
	f, ok := fileOf(t)
	if !ok {
		return actionReg{}, fmt.Errorf("value of type %s", t)
	}
	dst := c.reg(f)
	return dst, c.exprInto(e, dst)
}

// exprInto translates e into the register dst.
func (c *actionCompiler) exprInto(e code.Expr, dst actionReg) error {
	switch e := e.(type) {
	case *code.Const:
		switch e.Type.Kind {
		case code.KBool:
			v := int32(0)
			if e.Value.(bool) {
				v = 1
			}
			c.emit(state.ActionInstr{Op: state.TIntK, Dst: dst.r, A: v})
		case code.KInt:
			c.emit(state.ActionInstr{Op: state.TIntK, Dst: dst.r, A: int32(e.Value.(int64))})
		case code.KHandle:
			if e.Value != nil || dst.file != fileA {
				return fmt.Errorf("constant of type %s", e.Type)
			}
			c.emit(state.ActionInstr{Op: state.TConst, Dst: dst.r, A: c.constant(nil)})
		default:
			return fmt.Errorf("constant of type %s", e.Type)
		}
		return nil
	case *code.Local:
		src, ok := c.locals[e.Name]
		if !ok {
			return fmt.Errorf("undeclared local %s", e.Name)
		}
		if src.file != dst.file {
			return fmt.Errorf("move between register files")
		}
		if src == dst {
			return nil
		}
		op := [...]state.ActionOp{fileA: state.TMoveA, fileN: state.TMoveN, fileS: state.TMoveS, fileT: state.TMoveT}[src.file]
		c.emit(state.ActionInstr{Op: op, Dst: dst.r, A: src.r})
		return nil
	case *code.Unary:
		if e.Op != "!" {
			return fmt.Errorf("unary %s", e.Op)
		}
		x, err := c.expr(e.X)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TNot, Dst: dst.r, A: x.r})
		return nil
	case *code.Binary:
		x, err := c.expr(e.X)
		if err != nil {
			return err
		}
		if x.file == fileA && isNull(e.Y) && (e.Op == "==" || e.Op == "!=") {
			c.emit(state.ActionInstr{Op: state.TIsNil, Dst: dst.r, A: x.r})
			if e.Op == "!=" {
				c.emit(state.ActionInstr{Op: state.TNot, Dst: dst.r, A: dst.r})
			}
			return nil
		}
		y, err := c.expr(e.Y)
		if err != nil {
			return err
		}
		if x.file != fileN || y.file != fileN {
			return fmt.Errorf("binary %s of handles", e.Op)
		}
		switch e.Op {
		case "<":
			c.emit(state.ActionInstr{Op: state.TLt, Dst: dst.r, A: x.r, B: y.r})
		case "+":
			c.emit(state.ActionInstr{Op: state.TAdd, Dst: dst.r, A: x.r, B: y.r})
		case "==", "!=":
			c.emit(state.ActionInstr{Op: state.TEqN, Dst: dst.r, A: x.r, B: y.r})
			if e.Op == "!=" {
				c.emit(state.ActionInstr{Op: state.TNot, Dst: dst.r, A: dst.r})
			}
		default:
			return fmt.Errorf("binary %s", e.Op)
		}
		return nil
	case *code.Call:
		return c.call(e, dst)
	}
	return fmt.Errorf("expression %T", e)
}

// constant returns the index of v in the constant pool, adding it once.
func (c *actionCompiler) constant(v any) int32 {
	switch v.(type) {
	case nil, int, string, bool:
		for i, x := range c.prog.Consts {
			if x == v {
				return int32(i)
			}
		}
	}
	c.prog.Consts = append(c.prog.Consts, v)
	return int32(len(c.prog.Consts) - 1)
}

func (c *actionCompiler) name(s string) int32 {
	for i, n := range c.prog.Names {
		if n == s {
			return int32(i)
		}
	}
	c.prog.Names = append(c.prog.Names, s)
	return int32(len(c.prog.Names) - 1)
}

// args translates the arguments of the call e at the positions idx and returns their registers.
func (c *actionCompiler) args(e *code.Call, idx ...int) ([]int32, error) {
	rs := make([]int32, len(idx))
	for i, j := range idx {
		if j >= len(e.Args) {
			return nil, fmt.Errorf("%s has %d arguments", e.Callee, len(e.Args))
		}
		r, err := c.expr(e.Args[j])
		if err != nil {
			return nil, err
		}
		rs[i] = r.r
	}
	return rs, nil
}

// intImm returns the integer constant that is argument i of the call e.
func intImm(e *code.Call, i int) (int32, error) {
	if i < len(e.Args) {
		if k, ok := e.Args[i].(*code.Const); ok {
			switch v := k.Value.(type) {
			case int64:
				return int32(v), nil
			case bool:
				if v {
					return 1, nil
				}
				return 0, nil
			}
		}
	}
	return 0, fmt.Errorf("argument %d of %s is not a constant", i, e.Callee)
}

// immArg returns the immediate operand of the call that is argument i of e, which must be of the
// operation op.
func immArg(e *code.Call, i int, op string) (any, error) {
	if i < len(e.Args) {
		if a, ok := e.Args[i].(*code.Call); ok && a.Callee == op {
			return a.Imm, nil
		}
	}
	return nil, fmt.Errorf("argument %d of %s is not %s", i, e.Callee, op)
}

// fallible translates the call e of a composition that returns its failure, with its value, if it
// has one, into dst.
func (c *actionCompiler) fallible(e *code.Call, dst actionReg) error {
	in := state.ActionInstr{Dst: dst.r}
	switch e.Callee {
	case "member":
		kind, err := intImm(e, 1)
		if err != nil {
			return err
		}
		name, err := immArg(e, 3, action.OpActionName)
		if err != nil {
			return err
		}
		field, err := immArg(e, 4, action.OpActionField)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 2)
		if err != nil {
			return err
		}
		c.prog.Members = append(c.prog.Members, state.ActionMember{Kind: int(kind), Name: name.(string), Field: field.(action.Field).Field})
		in.Op, in.A, in.B = state.TMember, rs[0], int32(len(c.prog.Members)-1)
	case "unary", "logic":
		op, err := intImm(e, 0)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 1)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B = state.TUnary, rs[0], op
		if e.Callee == "logic" {
			in.Op = state.TLogic
		}
	case "binary":
		op, err := intImm(e, 0)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 1, 2)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B, in.C = state.TBinary, rs[0], rs[1], op
	case "text", "length", "filterKeep":
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = map[string]state.ActionOp{"text": state.TText, "length": state.TLength, "filterKeep": state.TFilter}[e.Callee], rs[0]
	case "convert":
		nt, err := immArg(e, 0, action.OpActionNodeType)
		if err != nil {
			return err
		}
		kinds := c.env.Checker.Kinds(nil).Nodes[nt.(string)]
		if kinds == nil {
			return fmt.Errorf("no node type %s", nt)
		}
		rs, err := c.args(e, 1)
		if err != nil {
			return err
		}
		c.prog.NodeTypes = append(c.prog.NodeTypes, kinds)
		in.Op, in.A, in.B = state.TConvert, rs[0], int32(len(c.prog.NodeTypes)-1)
	case "push", "pushItems", "items":
		at := 2
		nameAt := 1
		if e.Callee == "items" {
			at, nameAt = 1, 0
		}
		name, err := immArg(e, nameAt, action.OpActionName)
		if err != nil {
			return err
		}
		rs, err := c.args(e, at)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B = map[string]state.ActionOp{"push": state.TPush, "pushItems": state.TPushAll, "items": state.TItems}[e.Callee], rs[0], c.name(name.(string))
	default:
		return fmt.Errorf("composition %s", e.Callee)
	}
	c.emit(in)
	return nil
}

// effect translates the call e, whose results are discarded.
func (c *actionCompiler) effect(e *code.Call) error {
	switch e.Callee {
	case "seq.step":
		c.emit(state.ActionInstr{Op: state.TStep})
	case "units.add":
		n, err := intImm(e, 0)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TUnits, A: n})
	case "units.items":
		k, err := intImm(e, 1)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TUnitsOf, A: rs[0], B: k})
	case "units.since":
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TUnitsSince, A: rs[0]})
	case action.OpActionKeep:
		rs, err := c.args(e, 0, 1)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TKeep, A: rs[0], B: rs[1]})
	case action.OpActionSetField:
		rs, err := c.args(e, 0, 1)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TSetField, A: rs[0], B: rs[1], C: c.name(e.Imm.(string))})
	case "made":
		rs, err := c.args(e, 1, 2)
		if err != nil {
			return err
		}
		c.emit(state.ActionInstr{Op: state.TMade, A: rs[0], B: rs[1]})
	default:
		return fmt.Errorf("operation %s for its effect", e.Callee)
	}
	return nil
}

// call translates the call e of one result into dst.
func (c *actionCompiler) call(e *code.Call, dst actionReg) error {
	in := state.ActionInstr{Dst: dst.r}
	switch e.Callee {
	case action.OpActionConst:
		in.Op, in.A = state.TConst, c.constant(e.Imm.(action.Const).Value)
	case action.OpActionRead:
		switch x := e.Imm.(action.Read); x.Kind {
		case action.ReadCapture:
			slot, ok := c.env.Scope.Slots[x.Name]
			if !ok {
				return fmt.Errorf("capture %s is not in scope", x.Name)
			}
			in.Op, in.A = state.TCap, int32(slot)
		case action.ReadLocal:
			in.Op, in.A = state.TLocal, c.name(x.Name)
		case action.ReadVar:
			slot := c.env.Vars.Index(x.Name)
			if slot < 0 {
				return fmt.Errorf("variable %s is not declared", x.Name)
			}
			in.Op, in.A = state.TVar, int32(slot)
		default:
			return fmt.Errorf("read of kind %d", x.Kind)
		}
	case "val.unbox":
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = state.TUnbox, rs[0]
	case "val.fromBool":
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = state.TFromB, rs[0]
	case action.OpActionFromStr:
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = state.TFromT, rs[0]
	case action.OpActionFromObj:
		// A struct, list or tuple is held as the value it is.
		return c.exprInto(e.Args[0], dst)
	case action.OpActionTextEqual:
		rs, err := c.args(e, 0, 1)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B = state.TTextEq, rs[0], rs[1]
	case "conforms":
		ct, err := immArg(e, 0, action.OpActionCastType)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 1)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B = state.TConforms, rs[0], c.cast(ct.(action.Cast))
	case action.OpActionCastError:
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B = state.TCastErr, rs[0], c.cast(e.Imm.(action.Cast))
	case "action.kids":
		in.Op = state.TKids
	case action.OpActionElemsAbove:
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = state.TAbove, rs[0]
	case "hull":
		tuple, err := intImm(e, 1)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 2)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B = state.THull, rs[0], tuple
	case "itemAt":
		right, err := intImm(e, 2)
		if err != nil {
			return err
		}
		rs, err := c.args(e, 0, 1)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B, in.C = state.TItemAt, rs[0], rs[1], right
	case "seq.len":
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = state.TSeqLen, rs[0]
	case action.OpActionCopyList:
		rs, err := c.args(e, 0)
		if err != nil {
			return err
		}
		in.Op, in.A = state.TCopy, rs[0]
	case action.OpActionStruct:
		t := e.Imm.(action.New)
		c.prog.Structs = append(c.prog.Structs, state.ActionStruct{Kind: c.env.Checker.Kinds(nil).TypeKind(t.Type), Fields: t.Fields})
		in.Op, in.A = state.TStruct, int32(len(c.prog.Structs)-1)
	case "cover":
		rs, err := c.args(e, 0, 1, 2)
		if err != nil {
			return err
		}
		in.Op, in.A, in.B, in.C = state.TCover, rs[0], rs[1], rs[2]
	default:
		return fmt.Errorf("operation %s", e.Callee)
	}
	c.emit(in)
	return nil
}

// cast returns the index of the cast x in the cast pool, adding it once: the check and the failure
// of one cast share an entry.
func (c *actionCompiler) cast(x action.Cast) int32 {
	tc := state.ActionCast{Type: c.env.Checker.CastType(x.Key.(*ast.Cast)), Line: x.Line, Col: x.Col}
	for i, y := range c.prog.Casts {
		if y == tc {
			return int32(i)
		}
	}
	c.prog.Casts = append(c.prog.Casts, tc)
	return int32(len(c.prog.Casts) - 1)
}
