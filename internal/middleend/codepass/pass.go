// Package codepass holds the passes of Code IR that do not depend on a dialect (designs 065 and 072):
// transformations of functions that keep their behavior and that every backend prints the result of.
//
// Specialize makes the copy of a function for one set of features, in which every feature is a
// constant: the hand-written variants of one mechanism for each build (ordinary, budget-controlled,
// observed, node or typed values) become specializations of one composition. Canonicalize folds
// constants and removes code that cannot run, which is what makes a specialization lean; the pass
// canonicalize does the same in the pipeline, at the Flow and the Exec stage.
package codepass

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
)

// Specialize returns a copy of the function f of the module m named name and specialized for the
// features on, which must be features of m: each Feature expression becomes the constant true if
// its feature is in on and false otherwise, and the copy is canonicalized (Canonicalize). The copy's
// Features are on, so that it may call the operations of those features and no others.
func Specialize(m *code.Module, f *code.Func, name string, on []string) (*code.Func, error) {
	for _, ft := range on {
		if !slices.Contains(m.Features, ft) {
			return nil, fmt.Errorf("specialize %s: undeclared feature %s", f.Name, ft)
		}
	}
	c := Clone(f)
	c.Name, c.Features = name, slices.Clone(on)
	c.Body = mapBlock(c.Body, func(x code.Expr) code.Expr {
		if ft, ok := x.(*code.Feature); ok {
			return &code.Const{Type: code.TBool, Value: slices.Contains(on, ft.Name)}
		}
		return x
	})
	Canonicalize(c)
	return c, nil
}

// Clone returns a deep copy of f.
func Clone(f *code.Func) *code.Func {
	c := *f
	c.Params = slices.Clone(f.Params)
	c.Results = slices.Clone(f.Results)
	c.Locals = slices.Clone(f.Locals)
	c.Features = slices.Clone(f.Features)
	c.Body = mapBlock(f.Body, func(x code.Expr) code.Expr { return x })
	return &c
}

// mapBlock returns a copy of b in which every expression is replaced by what fn returns for it,
// after its subexpressions are replaced. Immediate operands are shared, not copied.
func mapBlock(b *code.Block, fn func(code.Expr) code.Expr) *code.Block {
	if b == nil {
		return nil
	}
	var x func(code.Expr) code.Expr
	x = func(e code.Expr) code.Expr {
		switch e := e.(type) {
		case nil:
			return nil
		case *code.Const:
			c := *e
			return fn(&c)
		case *code.Local:
			c := *e
			return fn(&c)
		case *code.Reg:
			c := *e
			return fn(&c)
		case *code.Feature:
			c := *e
			return fn(&c)
		case *code.Unary:
			return fn(&code.Unary{Op: e.Op, X: x(e.X)})
		case *code.Binary:
			return fn(&code.Binary{Op: e.Op, X: x(e.X), Y: x(e.Y)})
		case *code.Convert:
			return fn(&code.Convert{To: e.To, X: x(e.X)})
		case *code.Call:
			return fn(call(e, x))
		}
		panic(fmt.Sprintf("codepass: unknown expression %T", e))
	}
	set := func(s *code.Set) *code.Set {
		if s == nil {
			return nil
		}
		c := &code.Set{Dst: slices.Clone(s.Dst)}
		for _, v := range s.Values {
			c.Values = append(c.Values, x(v))
		}
		return c
	}
	var blk func(*code.Block) *code.Block
	blk = func(b *code.Block) *code.Block {
		if b == nil {
			return nil
		}
		out := &code.Block{Label: b.Label}
		for _, st := range b.Stmts {
			var n code.Stmt
			switch st := st.(type) {
			case *code.Block:
				n = blk(st)
			case *code.Loop:
				n = &code.Loop{Label: st.Label, Init: set(st.Init), Cond: x(st.Cond), Post: set(st.Post), Body: blk(st.Body)}
			case *code.If:
				n = &code.If{Init: set(st.Init), Cond: x(st.Cond), Then: blk(st.Then), Else: blk(st.Else)}
			case *code.Protect:
				n = &code.Protect{Body: blk(st.Body), Finally: blk(st.Finally)}
			case *code.Let:
				n = &code.Let{Names: slices.Clone(st.Names), Value: x(st.Value)}
			case *code.Set:
				n = set(st)
			case *code.Do:
				n = &code.Do{Call: call(st.Call, x)}
			case *code.Return:
				r := &code.Return{}
				for _, v := range st.Values {
					r.Values = append(r.Values, x(v))
				}
				n = r
			case *code.Assert:
				n = &code.Assert{Cond: x(st.Cond), Msg: st.Msg}
			case *code.Exit:
				c := *st
				n = &c
			case *code.Break:
				c := *st
				n = &c
			case *code.Continue:
				c := *st
				n = &c
			case *code.Comment:
				c := *st
				n = &c
			default:
				panic(fmt.Sprintf("codepass: unknown statement %T", st))
			}
			out.Stmts = append(out.Stmts, n)
		}
		return out
	}
	return blk(b)
}

func call(c *code.Call, x func(code.Expr) code.Expr) *code.Call {
	n := &code.Call{Callee: c.Callee, Imm: c.Imm}
	if c.Fn != nil {
		n.Fn = x(c.Fn)
	}
	for _, a := range c.Args {
		n.Args = append(n.Args, x(a))
	}
	return n
}
