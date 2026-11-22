package state

import (
	"unsafe"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// actionRegs holds the registers of the action programs that a parser evaluates, in four files (see
// ActionProgram). An evaluation takes its registers above the current tops and gives them back when
// it returns.
type actionRegs struct {
	a []any
	n []int
	s [][]*value.Node
	t []string
}

func (r *actionRegs) bytes() uint64 {
	return uint64(cap(r.a))*anyBytes + uint64(cap(r.n))*intBytes +
		uint64(cap(r.s))*uint64(unsafe.Sizeof([]*value.Node(nil))) + uint64(cap(r.t))*uint64(unsafe.Sizeof(""))
}

// growRegs extends s by n registers. The registers above the tops are zero: Eval clears what it
// used before it gives them back.
func growRegs[T any](s []T, n int32) []T {
	l := len(s) + int(n)
	if l > cap(s) {
		s = append(s[:cap(s)], make([]T, l-cap(s))...)
	}
	return s[:l]
}

// Eval evaluates the action expression in the context c and returns its value, or the failure of the action expression. It
// is the action expression's Evaluator.
func (t *ActionProgram) Eval(c *EvalCtx) (any, error) {
	p := c.P
	r := &p.actionExprs
	ab, nb, sb, tb := len(r.a), len(r.n), len(r.s), len(r.t)
	r.a, r.n, r.s, r.t = growRegs(r.a, t.NA), growRegs(r.n, t.NN), growRegs(r.s, t.NS), growRegs(r.t, t.NT)
	a, n, s, ts := r.a[ab:], r.n[nb:], r.s[sb:], r.t[tb:]
	v, err := t.run(c, a, n, s, ts)
	clear(a)
	clear(s)
	clear(ts)
	r.a, r.n, r.s, r.t = r.a[:ab], r.n[:nb], r.s[:sb], r.t[:tb]
	return v, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (t *ActionProgram) run(c *EvalCtx, a []any, n []int, s [][]*value.Node, ts []string) (any, error) {
	p := c.P
	code := t.Code
	var err error
	for pc := 0; ; pc++ {
		in := &code[pc]
		switch in.Op {
		case TNop:
		case TConst:
			a[in.Dst] = t.Consts[in.A]
		case TIntK:
			n[in.Dst] = int(in.A)
		case TMoveA:
			a[in.Dst] = a[in.A]
		case TMoveN:
			n[in.Dst] = n[in.A]
		case TMoveS:
			s[in.Dst] = s[in.A]
		case TMoveT:
			ts[in.Dst] = ts[in.A]
		case TCap:
			a[in.Dst] = nodes.UnboxNode(c.Frame.Vals[in.A])
		case TLocal:
			v, ok := c.local(t.Names[in.A])
			if !ok {
				return nil, p.ActionError("undefined capture $", t.Names[in.A])
			}
			a[in.Dst] = v
		case TVar:
			a[in.Dst] = p.VarValue(int(in.A))
		case TUnbox:
			a[in.Dst] = unboxed(a[in.A])
		case TFromB:
			a[in.Dst] = n[in.A] != 0
		case TFromT:
			a[in.Dst] = ts[in.A]
		case TNot:
			n[in.Dst] = 1 - n[in.A]
		case TLt:
			n[in.Dst] = b2i(n[in.A] < n[in.B])
		case TAdd:
			n[in.Dst] = n[in.A] + n[in.B]
		case TEqN:
			n[in.Dst] = b2i(n[in.A] == n[in.B])
		case TIsNil:
			n[in.Dst] = b2i(a[in.A] == nil)
		case TMember:
			m := &t.Members[in.B]
			if a[in.Dst], err = p.actionMember(c, m.Kind, a[in.A], m.Name, m.Field); err != nil {
				return nil, err
			}
		case TUnary:
			if a[in.Dst], err = p.actionUnary(int(in.B), a[in.A]); err != nil {
				return nil, err
			}
		case TBinary:
			if a[in.Dst], err = p.actionBinary(int(in.C), a[in.A], a[in.B]); err != nil {
				return nil, err
			}
		case TLogic:
			var b bool
			if b, err = p.actionLogic(int(in.B), a[in.A]); err != nil {
				return nil, err
			}
			n[in.Dst] = b2i(b)
		case TText:
			if ts[in.Dst], err = p.actionText(a[in.A]); err != nil {
				return nil, err
			}
		case TTextEq:
			n[in.Dst] = b2i(p.TextEqual(ts[in.A], ts[in.B]))
		case TLength:
			if a[in.Dst], err = p.actionLength(a[in.A]); err != nil {
				return nil, err
			}
		case TConvert:
			if a[in.Dst], err = p.actionConvert(t.NodeTypes[in.B], a[in.A]); err != nil {
				return nil, err
			}
		case TConforms:
			n[in.Dst] = b2i(p.actionConforms(t.Casts[in.B].Type, a[in.A]))
		case TCastErr:
			ct := &t.Casts[in.B]
			a[in.Dst] = p.CastError(a[in.A], ct.Type, c.Start, ct.Line, ct.Col)
		case TKids:
			n[in.Dst] = len(p.KidStack)
		case TPush:
			if err = p.actionPush(c, t.Names[in.B], a[in.A]); err != nil {
				return nil, err
			}
		case TPushAll:
			if err = p.actionPushItems(c, t.Names[in.B], a[in.A]); err != nil {
				return nil, err
			}
		case TAbove:
			s[in.Dst] = p.ElemsAbove(n[in.A])
		case THull:
			p.countUnits(len(s[in.A]))
			a[in.Dst] = p.actionHull(c, in.B != 0, s[in.A])
		case TItems:
			if s[in.Dst], err = p.actionItems(t.Names[in.B], a[in.A]); err != nil {
				return nil, err
			}
		case TItemAt:
			a[in.Dst] = p.actionItemAt(s[in.A], n[in.B], in.C != 0)
		case TSeqLen:
			n[in.Dst] = len(s[in.A])
		case TStep:
			p.Spend()
		case TKeep:
			p.KidStack = append(p.KidStack, s[in.A][n[in.B]])
		case TFilter:
			var b bool
			if b, err = p.actionFilterKeep(a[in.A]); err != nil {
				return nil, err
			}
			n[in.Dst] = b2i(b)
		case TCopy:
			// The elements that map visits and those of the list that it builds.
			p.countUnits(2 * len(s[in.A]))
			a[in.Dst] = c.NewList(s[in.A])
		case TStruct:
			st := &t.Structs[in.A]
			a[in.Dst] = p.NewNode(nodes.Data{Kind: st.Kind, Fields: p.fields(st.Fields)})
		case TSetField:
			p.SetValueField(&nodes.D(a[in.A].(*value.Node)).Fields, t.Names[in.C], a[in.B])
		case TField:
			x := a[in.A].(*value.Node)
			p.SetValueField(&nodes.D(x).Fields, t.Names[in.C], a[in.B])
			n[in.Dst] = b2i(p.actionCover(x, a[in.B], n[in.Dst] != 0))
		case TCover:
			n[in.Dst] = b2i(p.actionCover(a[in.A].(*value.Node), a[in.B], n[in.C] != 0))
		case TMade:
			p.actionMade(c, a[in.A].(*value.Node), n[in.B] != 0)
		case TJump:
			pc = int(in.Dst) - 1
		case TJumpF:
			if n[in.A] == 0 {
				pc = int(in.Dst) - 1
			}
		case TLoop:
			n[in.C], n[in.B] = len(s[in.A]), 0
			if n[in.C] == 0 {
				pc = int(in.Dst) - 1
			}
		case TEndLoop:
			n[in.B]++
			if n[in.B] < n[in.C] {
				pc = int(in.Dst) - 1
			}
		case TRet:
			return a[in.A], nil
		case TRetErr:
			return nil, a[in.A].(error)
		case TUnits:
			p.countUnits(int(in.A))
		case TUnitsOf:
			if items, ok := valItems(a[in.A]); ok {
				p.countUnits(int(in.B) * len(items))
			}
		case TUnitsSince:
			p.countUnits(len(p.KidStack) - n[in.A])
		default:
			panic("core: action instruction " + string(rune('0'+in.Op)))
		}
	}
}

// local returns the value of the Operators Rule local name ($lhs, $rhs, $op or $value), with a box
// opened, and false if c has none of that name. Reading a local is no event: it counts nothing.
func (c *EvalCtx) local(name string) (any, bool) {
	for l := c.Locals; l != nil; l = l.Next {
		if l.Name == name {
			return nodes.UnboxValue(l.Val), true
		}
	}
	return nil, false
}
