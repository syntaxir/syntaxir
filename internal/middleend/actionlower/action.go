// Package actionlower lowers action expressions to the Action dialect.
//
// An action expression (an action, a predicate or the value of a binding) becomes a function of Code IR that calls
// the compositions of the operators of action expressions (binary, unary, logic, member, length, text, convert,
// conforms) of the abstract machine, in the order the action expression evaluates its operands. Each call returns its value
// and a failure; the failure is checked right after the call. The function returns the action expression's value and
// a failure, as a composition does: a failure ends the evaluation and the function returns it, for its
// caller to handle (a predicate fails, an action ends the parse). The logical operators && and || are
// branches: the right operand is evaluated only if the left one does not decide the result.
//
// The leaves of an action expression (constants, captures, locals and variables), the context of the evaluation and
// the static operands of the compositions (a member's name, a node type, the checked type of a cast)
// are the leaves of the Action dialect (action.DeclareLeaves): primitive operations, each with a typed
// immediate operand, that the target prints in place.
//
// A built-in function that takes a function (map, filter, foldl, foldr) is a loop over the elements of
// its list argument: each step counts the visit of the element (seq.step) when the target counts
// compute units (Options.Visits), binds the parameters of the lambda to locals of the loop and evaluates
// its body in place.
package actionlower

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
	ir "syntaxir.dev/sxir/internal/ir/core"
)

// CtxParam is the parameter of a lowered action expression: the context the action expression is applied to. An action expression's results are those of
// the callable type Action (action.Action) unless the action expression is evaluated in place of an action (Options.Result).
const CtxParam = "c"

// Compositions are the compositions of the parser VM that lowered action expressions call.
var Compositions = []string{"binary", "unary", "logic", "member", "length", "text", "convert", "conforms",
	"push", "pushItems", "hull", "cover", "made", "items", "itemAt", "filterKeep"}

// Lowered is an action expression lowered to Exec IR.
type Lowered struct {
	// Module declares what Func calls: the compositions of the operators of action expressions with what they call,
	// and the operations of the leaves.
	Module *code.Module
	Func   *code.Func
	// Result is the local of Func (Func.Locals) that holds the action expression's value at the end of Func's body,
	// or "" if the body returns it.
	Result string
}

// Options configures Lower.
type Options struct {
	// Result, if not empty, names a local of the lowered function (Func.Locals) that its body assigns
	// the action expression's value to last, and a failure ends the parse by action.raise; otherwise the body returns
	// the value and the failure. A printer that prints the body without the function, in place of an
	// action, declares the local around it.
	Result string
	// StructCall makes a struct literal one call of the target's constructor of its type (action.newN),
	// with the field values evaluated in the order written and passed in declaration order, nil for a
	// field not given. Otherwise the struct is made of its fields by action.struct, action.setField, cover
	// and made.
	StructCall bool
	// Visits counts the step of each element that a loop of map, filter or a fold visits (seq.step):
	// the visit is a logical compute unit (design record 076).
	Visits bool
	// FieldCompare makes a comparison (==, !=, <, <=, >, >=) of which an operand reads a field a call
	// of action.compare on operands, reading such a field with action.fieldOperand, so that a string field
	// is compared without making a value of it.
	FieldCompare bool
	// TextDispatch makes a predicate that compares the text of one capture with string literals,
	// text($x) == "a" || text($x) == "b" ..., test the text against the literals at once with
	// action.textIn when the parse runs without limit controls, and evaluate the action expression as written
	// otherwise. It applies to an action expression that the function returns (Result is empty).
	TextDispatch bool
	// Proj are the projected captures of the rule of the action expression, by name, with the capture of an element
	// that each stands for (the projections of the rule's view, common.Rule.Projections). A call map($x, (e) => e.$f) of a projected capture
	// x whose elements stand for f takes the elements of x as they are (action.copyList).
	Proj map[string]string
}

// Lower lowers the action expression t. It reports an error if t is not an action expression that the front end builds: a
// built-in other than those of the language, one with arguments of the wrong number, a lambda that
// is not the function argument of map, filter, foldl or foldr, or a parameter read outside the body
// of its lambda.
func Lower(t ir.ActionExpr, o Options) (*Lowered, error) {
	if !wellFormed(t) {
		return nil, fmt.Errorf("lower: an action expression that the front end does not build: %s", ir.ActionExprString(t))
	}
	m, err := actionModule()
	if err != nil {
		return nil, err
	}
	nOps, nFuncs := len(m.Ops), len(m.Funcs)
	datum, failT := code.THandle(exec.Datum), code.THandle(action.Fail)
	var results []code.Type
	if o.Result == "" {
		results = []code.Type{datum, failT}
	}
	l := &actionLowerer{m: m, structCall: o.StructCall, visits: o.Visits, proj: o.Proj,
		params: map[*ir.Lambda][]code.Expr{}, inPlace: o.Result != "", fieldCompare: o.FieldCompare}
	fn := m.AddFunc("action", []code.Param{code.P(CtxParam, code.THandle(action.Act))}, results, nil, func(b *code.Builder) {
		var res code.Expr
		if o.Result != "" {
			res = b.Var(o.Result, datum)
		}
		if x, lits, ok := textMembership(t); ok && o.TextDispatch && res == nil {
			b.If(code.C(action.OpActionUncontrolled), func(b *code.Builder) {
				s := l.fallible(b, "ts", code.C("text", imm(action.OpActionRead, readOf(x))))
				var in code.Expr = &code.Call{Callee: action.OpActionTextIn, Args: []code.Expr{s}, Imm: lits}
				if len(lits) > 1 {
					// A target may print the binding as one switch; one literal stays an equality.
					in = b.Let(l.local("tin"), in)
				}
				b.Return(code.C("val.fromBool", in), code.Null(failT))
			})
		}
		v := l.value(b, t)
		if res != nil {
			b.Set(o.Result, v)
		} else {
			b.Return(v, code.Null(failT))
		}
	})
	// The module the action expression extends was checked once when it was built.
	if err := code.VerifyExtension(m, nOps, nFuncs); err != nil {
		return nil, fmt.Errorf("lower action expression: %w", err)
	}
	if o.Result == "" {
		if err := checkActionSignature(m, fn); err != nil {
			return nil, err
		}
	}
	return &Lowered{Module: m, Func: fn, Result: o.Result}, nil
}

// checkActionSignature reports an error unless the parameters and results of f are those of the callable
// type Action of m, which is how the compositions apply an action expression: the lowered action expression is the definition of what an
// application of a Action runs.
func checkActionSignature(m *code.Module, f *code.Func) error {
	h := m.HandleType(action.Action)
	if h == nil || h.Callable == nil {
		return fmt.Errorf("lower action expression: the module declares no callable type %s", action.Action)
	}
	sig := h.Callable
	same := len(f.Params) == len(sig.Params) && len(f.Results) == len(sig.Results)
	for i := 0; same && i < len(f.Params); i++ {
		same = f.Params[i].Type == sig.Params[i]
	}
	for i := 0; same && i < len(f.Results); i++ {
		same = f.Results[i] == sig.Results[i]
	}
	if !same {
		return fmt.Errorf("lower action expression: the function %s does not have the signature of %s", f.Name, action.Action)
	}
	return nil
}

// wellFormed reports whether t is an action expression that Lower lowers: the built-ins of the language with their
// arguments, lambdas only as their function arguments, and parameters only in the bodies of their
// lambdas.
func wellFormed(t ir.ActionExpr) bool {
	ok := true
	args := map[*ir.Lambda]bool{} // the lambdas that are the function arguments of built-ins
	ir.WalkActionExpr(t, func(x ir.ActionExpr) {
		switch x := x.(type) {
		case *ir.Const, *ir.SlotRef, *ir.VarRef, *ir.Member, *ir.Binary, *ir.Unary, *ir.Convert, *ir.Cast,
			*ir.Tuple, *ir.New, *ir.Local:
		case *ir.Lambda:
			// WalkAction visits a built-in before its arguments; a parameter is read only in the body of
			// its lambda.
			ok = ok && args[x]
		case *ir.Builtin:
			switch x.Func {
			case "len", "text":
				ok = ok && len(x.Args) == 1 && plain(x.Args[0])
			case "list", "concat":
				for _, a := range x.Args {
					ok = ok && plain(a)
				}
			case "map", "filter":
				ok = ok && len(x.Args) == 2 && plain(x.Args[0]) && lambda(x.Args[1], 1)
			case "foldl", "foldr":
				ok = ok && len(x.Args) == 3 && plain(x.Args[0]) && plain(x.Args[1]) && lambda(x.Args[2], 2)
			default:
				ok = false
			}
			if n := len(x.Args); ok && n > 0 {
				if f, isLambda := x.Args[n-1].(*ir.Lambda); isLambda {
					args[f] = true
				}
			}
		default:
			ok = false
		}
	})
	return ok && scoped(t, nil)
}

// scoped reports whether every parameter that t reads is read in the body of its lambda; in holds
// the lambdas that enclose t.
func scoped(t ir.ActionExpr, in []*ir.Lambda) bool {
	switch x := t.(type) {
	case *ir.Local:
		return x.Kind != ir.LocalParam || slices.Contains(in, x.Lambda)
	case *ir.Lambda:
		in = append(in[:len(in):len(in)], x)
	}
	for _, c := range ir.ActionExprChildren(t) {
		if !scoped(c, in) {
			return false
		}
	}
	return true
}

// plain reports whether t is not a lambda.
func plain(t ir.ActionExpr) bool {
	_, ok := t.(*ir.Lambda)
	return !ok
}

// lambda reports whether t is a lambda with n parameters.
func lambda(t ir.ActionExpr, n int) bool {
	f, ok := t.(*ir.Lambda)
	return ok && len(f.Params) == n
}

var (
	actionBaseOnce sync.Once
	actionBase     *code.Module
	actionBaseErr  error
)

// actionModule returns a new module for one lowered action expression: the declarations of the parser VM, the
// operations of the leaves, and the compositions of the operators of action expressions with every function they
// call.
func actionModule() (*code.Module, error) {
	actionBaseOnce.Do(func() {
		base := compose.Build()
		m := &code.Module{Name: "action", Features: base.Features, Components: slices.Clip(base.Components),
			Registers: base.Registers, Handles: slices.Clip(base.Handles), Ops: slices.Clip(base.Ops)}
		action.DeclareLeaves(m)
		seen := map[string]bool{}
		var add func(name string)
		add = func(name string) {
			f := base.Func(name)
			if f == nil || seen[name] {
				return
			}
			seen[name] = true
			code.Walk(f.Body, nil, func(e code.Expr) {
				if c, ok := e.(*code.Call); ok {
					add(c.Callee)
				}
			})
			m.Funcs = append(m.Funcs, f)
		}
		for _, name := range Compositions {
			if base.Func(name) == nil {
				actionBaseErr = fmt.Errorf("lower: no composition %s", name)
				return
			}
			add(name)
		}
		m.Funcs = slices.Clip(m.Funcs)
		if err := code.Verify(m); err != nil {
			actionBaseErr = fmt.Errorf("lower: the module of action expressions: %w", err)
			return
		}
		actionBase = m
	})
	if actionBaseErr != nil {
		return nil, actionBaseErr
	}
	// The slices are clipped, so that what an action expression adds to its module (action.newN, its function) does not
	// write into those of the base, which other action expressions share.
	m := *actionBase
	m.Ops, m.Funcs = slices.Clip(m.Ops), slices.Clip(m.Funcs)
	return &m, nil
}

type actionLowerer struct {
	m          *code.Module
	n          int
	structCall bool
	visits     bool
	proj       map[string]string
	// params holds the values of the parameters of each lambda whose body is being lowered.
	params map[*ir.Lambda][]code.Expr
	// inPlace is set when the action expression is evaluated in place of an action (Options.Result).
	inPlace      bool
	fieldCompare bool
}

// raise ends the evaluation of the action expression with the failure f: the function returns it, or, in place of
// an action, action.raise ends the parse.
func (l *actionLowerer) raise(b *code.Builder, f code.Expr) {
	if l.inPlace {
		b.Do(action.OpActionRaise, f)
		return
	}
	b.Return(code.Null(code.THandle(exec.Datum)), f)
}

// local returns a fresh name of a local with the prefix p. A local is named when the statement that
// defines it is appended, after the statements that compute its value, so that the numbers of the
// locals follow the order in which their definitions appear.
func (l *actionLowerer) local(p string) string {
	l.n++
	return p + strconv.Itoa(l.n)
}

// fallible lets the results of the call c of a composition that returns a value and a failure,
// raises the failure (raise), and returns the value.
func (l *actionLowerer) fallible(b *code.Builder, prefix string, c *code.Call) code.Expr {
	n := l.local("")
	r := b.LetN([]string{prefix + n, "tf" + n}, c)
	b.If(code.Bin("!=", r[1], code.Null(code.THandle(action.Fail))), func(b *code.Builder) { l.raise(b, r[1]) })
	return r[0]
}

func imm(callee string, x any) *code.Call { return &code.Call{Callee: callee, Imm: x} }

// opCode returns the code of the operator sym in the list of operators.
func opCode(list []string, sym string) code.Expr {
	i := slices.Index(list, sym)
	if i < 0 {
		panic("lower: unknown operator " + sym)
	}
	return code.Int(int64(i))
}

// value lowers t, appending the statements that compute its operands to b, and returns its value: a
// local or the call of a leaf.
func (l *actionLowerer) value(b *code.Builder, t ir.ActionExpr) code.Expr {
	switch t := t.(type) {
	case *ir.Const:
		return imm(action.OpActionConst, action.Const{Value: t.Value})
	case *ir.Local:
		if t.Kind == ir.LocalParam {
			// A parameter may hold a box, which stands for its value.
			return code.C("val.unbox", l.params[t.Lambda][slices.Index(t.Lambda.Params, t.Name)])
		}
		return imm(action.OpActionRead, readOf(t))
	case *ir.SlotRef:
		return imm(action.OpActionRead, readOf(t))
	case *ir.VarRef:
		// Reading a variable charges the budget, so it is read where the action expression reads it, before the
		// operands that follow.
		return b.Let(l.local("tv"), imm(action.OpActionRead, readOf(t)))
	case *ir.Member:
		x := l.value(b, t.X)
		kind, field := memberKind(t.Name)
		return l.fallible(b, "tv", code.C("member", code.L(CtxParam), code.Int(int64(kind)), x,
			imm(action.OpActionName, t.Name), imm(action.OpActionField, action.Field{Name: t.Name, Field: field})))
	case *ir.Unary:
		x := l.value(b, t.X)
		return l.fallible(b, "tv", code.C("unary", opCode(action.UnaryOps, t.Op), x))
	case *ir.Binary:
		switch t.Op {
		case "&&", "||":
			return l.logic(b, t)
		case "==", "!=":
			if lx, ok := textArg(t.L); ok {
				if rx, ok := textArg(t.R); ok {
					// Two texts compare as strings, without making a value of either.
					ls := l.fallible(b, "ts", code.C("text", l.value(b, lx)))
					rs := l.fallible(b, "ts", code.C("text", l.value(b, rx)))
					var eq code.Expr = code.C(action.OpActionTextEqual, ls, rs)
					if t.Op == "!=" {
						eq = code.Not(eq)
					}
					return code.C("val.fromBool", eq)
				}
			}
		}
		if l.fieldCompare && comparison(t.Op) && (fieldRead(t.L) || fieldRead(t.R)) {
			x := l.operand(b, t.L)
			y := l.operand(b, t.R)
			return l.fallible(b, "tv", &code.Call{Callee: action.OpActionCompare, Args: []code.Expr{x, y}, Imm: t.Op})
		}
		x := l.value(b, t.L)
		y := l.value(b, t.R)
		return l.fallible(b, "tv", code.C("binary", opCode(action.BinaryOps, t.Op), x, y))
	case *ir.Tuple:
		return l.sequence(b, true, "tuple", l.values(b, t.Elems))
	case *ir.New:
		return l.structLiteral(b, t)
	case *ir.Builtin:
		switch t.Func {
		case "list":
			return l.sequence(b, false, "list", l.values(b, t.Args))
		case "concat":
			if fusable(t) {
				// The elements of the arguments are pushed as they are made, without making the lists of
				// the arguments.
				base := b.Let(l.local("tm"), code.C(action.OpActionKids))
				l.concatParts(b, t)
				return l.hull(b, false, base)
			}
			vals := l.values(b, t.Args)
			base := b.Let(l.local("tm"), code.C(action.OpActionKids))
			for _, v := range vals {
				l.check(b, code.C("pushItems", code.L(CtxParam), imm(action.OpActionName, "concat"), v))
			}
			return l.hull(b, false, base)
		case "map":
			if x, ok := l.projected(t); ok {
				items := l.fallible(b, "ti", code.C("items", imm(action.OpActionName, "map"), l.value(b, x)))
				return b.Let(l.local("tv"), code.C(action.OpActionCopyList, items))
			}
			list := l.value(b, t.Args[0])
			base := b.Let(l.local("tm"), code.C(action.OpActionKids))
			l.mapPush(b, list, t.Args[1].(*ir.Lambda))
			return l.hull(b, false, base)
		case "filter":
			list := l.value(b, t.Args[0])
			base := b.Let(l.local("tm"), code.C(action.OpActionKids))
			f := t.Args[1].(*ir.Lambda)
			items := l.fallible(b, "ti", code.C("items", imm(action.OpActionName, "filter"), list))
			l.loop(b, items, func(b *code.Builder, i code.Expr) {
				v := l.call(b, f, code.C("itemAt", items, i, code.BoolC(false)))
				keep := l.fallible(b, "tb", code.C("filterKeep", v))
				b.If(keep, func(b *code.Builder) { b.Do(action.OpActionKeep, items, i) })
			})
			return l.hull(b, false, base)
		case "foldl", "foldr":
			init := l.value(b, t.Args[0])
			list := l.value(b, t.Args[1])
			f := t.Args[2].(*ir.Lambda)
			items := l.fallible(b, "ti", code.C("items", imm(action.OpActionName, t.Func), list))
			acc := l.local("ta")
			b.Let(acc, init)
			l.loop(b, items, func(b *code.Builder, i code.Expr) {
				b.Set(acc, l.call(b, f, code.L(acc), code.C("itemAt", items, i, code.BoolC(t.Func == "foldr"))))
			})
			return code.L(acc)
		}
		x := l.value(b, t.Args[0])
		if t.Func == "len" {
			return l.fallible(b, "tv", code.C("length", x))
		}
		return code.C(action.OpActionFromStr, l.fallible(b, "ts", code.C("text", x)))
	case *ir.Convert:
		x := l.value(b, t.X)
		return l.fallible(b, "tv", code.C("convert", imm(action.OpActionNodeType, t.NodeType), x))
	case *ir.Cast:
		if t.Check == nil {
			// Every value fits the type.
			return l.value(b, t.X)
		}
		v := l.value(b, t.X)
		x := b.Let(l.local("tc"), v)
		b.If(code.Not(code.C("conforms", imm(action.OpActionCastType, castOf(t)), x)), func(b *code.Builder) {
			l.raise(b, &code.Call{Callee: action.OpActionCastError, Args: []code.Expr{x}, Imm: castOf(t)})
		})
		return x
	}
	panic(fmt.Sprintf("lower: action %T", t))
}

// values lowers the action expressions ts in order and returns their values.
func (l *actionLowerer) values(b *code.Builder, ts []ir.ActionExpr) []code.Expr {
	vals := make([]code.Expr, len(ts))
	for i, t := range ts {
		vals[i] = l.value(b, t)
	}
	return vals
}

// check lets the failure that the call c of a composition returns and raises it.
func (l *actionLowerer) check(b *code.Builder, c *code.Call) {
	f := b.Let(l.local("tf"), c)
	b.If(code.Bin("!=", f, code.Null(code.THandle(action.Fail))), func(b *code.Builder) { l.raise(b, f) })
}

// sequence makes the tuple value, if tuple, or the list value of vals, the values of the elements that
// the literal or the built-in function fn gives: it pushes each, and the hull takes them over.
func (l *actionLowerer) sequence(b *code.Builder, tuple bool, fn string, vals []code.Expr) code.Expr {
	base := b.Let(l.local("tm"), code.C(action.OpActionKids))
	l.pushes(b, fn, vals)
	return l.hull(b, tuple, base)
}

// pushes pushes vals as elements of the list or the tuple that fn makes.
func (l *actionLowerer) pushes(b *code.Builder, fn string, vals []code.Expr) {
	for _, v := range vals {
		l.check(b, code.C("push", code.L(CtxParam), imm(action.OpActionName, fn), v))
	}
}

// hull returns the tuple value, if tuple, or the list value of the elements pushed since base.
func (l *actionLowerer) hull(b *code.Builder, tuple bool, base code.Expr) code.Expr {
	kids := b.Let(l.local("tk"), code.C(action.OpActionElemsAbove, base))
	return code.C(action.OpActionFromObj, b.Let(l.local("tn"), code.C("hull", code.L(CtxParam), code.BoolC(tuple), kids)))
}

// concatParts pushes the elements of the arguments of the fusable concat call t: those of a list call
// as list pushes them, those of a map call as map pushes them, and those of a concat call by its own
// arguments. The calls themselves are not subexpressions that are evaluated, and charge no step for one.
func (l *actionLowerer) concatParts(b *code.Builder, t *ir.Builtin) {
	for _, a := range t.Args {
		call := a.(*ir.Builtin)
		switch call.Func {
		case "list":
			vals := l.values(b, call.Args)
			if l.visits {
				// Logical units: the list that list would build, which the fusion does not build.
				b.Do("units.add", code.Int(int64(len(vals))))
			}
			l.pushes(b, "list", vals)
		case "map":
			if x, ok := l.projected(call); ok {
				// The elements of the projected capture are pushed as concat pushes those of an argument.
				v := l.value(b, x)
				if l.visits {
					// Logical units: the elements that map would visit and the list it would build.
					b.Do("units.items", v, code.Int(2))
				}
				l.check(b, code.C("pushItems", code.L(CtxParam), imm(action.OpActionName, "concat"), v))
				break
			}
			l.mapPushFused(b, l.value(b, call.Args[0]), call.Args[1].(*ir.Lambda), true)
		case "concat":
			var base code.Expr
			if l.visits {
				base = b.Let(l.local("tm"), code.C(action.OpActionKids))
			}
			l.concatParts(b, call)
			if l.visits {
				// Logical units: the list that the inner concat would build.
				b.Do("units.since", base)
			}
		default:
			panic("lower: concat part " + call.Func)
		}
	}
}

// fusable reports whether every argument of the concat call t is a call of list or map or a fusable
// concat, whose elements concatParts pushes as they are made, as check.Fusable decides.
func fusable(t *ir.Builtin) bool {
	for _, a := range t.Args {
		call, ok := a.(*ir.Builtin)
		switch {
		case ok && call.Func == "list", ok && call.Func == "map" && len(call.Args) == 2:
		case ok && call.Func == "concat" && fusable(call):
		default:
			return false
		}
	}
	return true
}

// mapPush pushes the values that the lambda f returns for the elements of list as the elements of the
// list that map makes.
func (l *actionLowerer) mapPush(b *code.Builder, list code.Expr, f *ir.Lambda) {
	l.mapPushFused(b, list, f, false)
}

// mapPushFused is mapPush, for the part of a fused concat if fused: under logical units, each element
// also counts the element of the list that map would build.
func (l *actionLowerer) mapPushFused(b *code.Builder, list code.Expr, f *ir.Lambda, fused bool) {
	items := l.fallible(b, "ti", code.C("items", imm(action.OpActionName, "map"), list))
	l.loop(b, items, func(b *code.Builder, i code.Expr) {
		v := l.call(b, f, code.C("itemAt", items, i, code.BoolC(false)))
		if fused && l.visits {
			b.Do("seq.step")
		}
		l.check(b, code.C("push", code.L(CtxParam), imm(action.OpActionName, "map"), v))
	})
}

// loop appends a loop over the elements items, which items returned, whose body charges the step of
// the element and then runs body with the index of the element.
func (l *actionLowerer) loop(b *code.Builder, items code.Expr, body func(b *code.Builder, i code.Expr)) {
	i := l.local("tj")
	b.Let(i, code.Int(0))
	b.For(l.local("each"), nil, code.Lt(code.L(i), code.C("seq.len", items)), code.Assign(i, code.Add(code.L(i), code.Int(1))), func(b *code.Builder) {
		if l.visits {
			b.Do("seq.step")
		}
		body(b, code.L(i))
	})
}

// call evaluates the body of the lambda f with its parameters bound to args, each let to a local, and
// returns its value.
func (l *actionLowerer) call(b *code.Builder, f *ir.Lambda, args ...code.Expr) code.Expr {
	vals := make([]code.Expr, len(args))
	for i, a := range args {
		vals[i] = b.Let(l.local("tp"), a)
	}
	l.params[f] = vals
	body := l.value(b, f.Body)
	v := b.Let(l.local("tv"), body)
	delete(l.params, f)
	return v
}

// projected reports whether the map call t is map($x, (e) => e.$f) of a projected capture x (Proj)
// whose elements stand for the capture f, and returns the read of x.
func (l *actionLowerer) projected(t *ir.Builtin) (*ir.SlotRef, bool) {
	if l.proj == nil || len(t.Args) != 2 {
		return nil, false
	}
	x, ok := t.Args[0].(*ir.SlotRef)
	if !ok || l.proj[x.Name] == "" {
		return nil, false
	}
	f, ok := t.Args[1].(*ir.Lambda)
	if !ok || len(f.Params) != 1 {
		return nil, false
	}
	m, ok := f.Body.(*ir.Member)
	if !ok {
		return nil, false
	}
	e, ok := m.X.(*ir.Local)
	if !ok || e.Kind != ir.LocalParam || e.Lambda != f {
		return nil, false
	}
	return x, m.Field == l.proj[x.Name]
}

// inDeclaredOrder reports whether the struct literal t gives its fields in the order its type declares
// them.
func inDeclaredOrder(t *ir.New) bool {
	next := 0
	for _, f := range t.Fields {
		for next < len(t.Declared) && t.Declared[next] != f.Name {
			next++
		}
		if next == len(t.Declared) {
			return false
		}
		next++
	}
	return true
}

// structLiteral lowers the struct literal t. The field values are evaluated in the order written. The
// struct is then one call of the constructor of its type (StructCall), or made of its fields in that
// order: a step charged for each, the field set and its value covered by the range of the struct.
func (l *actionLowerer) structLiteral(b *code.Builder, t *ir.New) code.Expr {
	// A value that is not a local or a constant is let before the next one is evaluated when the
	// constructor call takes the values in another order than written, since the call evaluates its
	// arguments in the order declared. Without the constructor, each value is used twice (set and
	// covered), so such a value is let once: a read is then made, and charged, once, as the action expression
	// reads it. A local or a constant is evaluated already and is used as it is. Otherwise the values are
	// used as they are; what a field leaves for its use to evaluate (a read, a constant) does not depend on
	// the fields after it.
	let := !l.structCall || !inDeclaredOrder(t)
	vals := make([]code.Expr, len(t.Fields))
	for i, f := range t.Fields {
		vals[i] = l.value(b, f.Value)
		if let && !settled(vals[i]) {
			vals[i] = b.Let(l.local("tf"), vals[i])
		}
	}
	if l.structCall {
		name := action.OpActionNew + strconv.Itoa(len(t.Declared))
		if l.m.Op(name) == nil {
			l.m.AddOp(action.NewOp(len(t.Declared)))
		}
		args := make([]code.Expr, len(t.Declared))
		for i, d := range t.Declared {
			args[i] = imm(action.OpActionConst, action.Const{Value: nil})
			for j, f := range t.Fields {
				if f.Name == d {
					args[i] = vals[j]
				}
			}
		}
		return b.Let(l.local("tv"), &code.Call{Callee: name, Args: args, Imm: newOf(t)})
	}
	n := b.Let(l.local("tn"), &code.Call{Callee: action.OpActionStruct, Imm: newOf(t)})
	first := l.local("tfirst")
	b.Let(first, code.BoolC(true))
	for i, f := range t.Fields {
		b.Append(&code.Do{Call: &code.Call{Callee: action.OpActionSetField, Args: []code.Expr{n, vals[i]}, Imm: f.Name}})
		b.Set(first, code.C("cover", n, vals[i], code.L(first)))
	}
	b.Do("made", code.L(CtxParam), n, code.L(first))
	return code.C(action.OpActionFromObj, n)
}

// settled reports whether the expression e is a local or a constant, which can be used more than
// once without being evaluated again.
func settled(e code.Expr) bool {
	switch e := e.(type) {
	case *code.Local, *code.Const:
		return true
	case *code.Call:
		return e.Callee == action.OpActionConst
	}
	return false
}

// logic lowers l && r and l || r: logic checks each operand that is evaluated, and the right one is
// evaluated only if the left one does not decide the result.
func (l *actionLowerer) logic(b *code.Builder, t *ir.Binary) code.Expr {
	op := opCode(action.LogicalOps, t.Op)
	left := l.fallible(b, "tb", code.C("logic", op, l.value(b, t.L)))
	and := t.Op == "&&"
	r := b.Let(l.local("tv"), code.C("val.fromBool", code.BoolC(!and)))
	cond := left
	if !and {
		cond = code.Not(left)
	}
	b.If(cond, func(b *code.Builder) {
		right := l.fallible(b, "tb", code.C("logic", op, l.value(b, t.R)))
		b.Set(r.(*code.Local).Name, code.C("val.fromBool", right))
	})
	return r
}

// comparison reports whether op is a comparison operator.
func comparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// fieldRead reports whether t reads a field of a value, x.F, not a capture.
func fieldRead(t ir.ActionExpr) bool {
	m, ok := t.(*ir.Member)
	return ok && (m.Name == "" || m.Name[0] != '$')
}

// operand lowers t, an operand of a comparison (FieldCompare): a field read is read by
// action.fieldOperand, any other action expression is a value.
func (l *actionLowerer) operand(b *code.Builder, t ir.ActionExpr) code.Expr {
	if !fieldRead(t) {
		return code.C(action.OpActionOperand, l.value(b, t))
	}
	m := t.(*ir.Member)
	x := l.value(b, m.X)
	return l.fallible(b, "to", &code.Call{Callee: action.OpActionFieldOperand, Args: []code.Expr{x}, Imm: m.Name})
}

// textMembership reports whether t compares the text of one capture with string literals,
// text($x) == "a" || ..., with the capture and the distinct literals in the order written.
func textMembership(t ir.ActionExpr) (ir.ActionExpr, []string, bool) {
	var x ir.ActionExpr
	var name string
	var lits []string
	var walk func(ir.ActionExpr) bool
	walk = func(t ir.ActionExpr) bool {
		b, ok := t.(*ir.Binary)
		if ok && b.Op == "||" {
			return walk(b.L) && walk(b.R)
		}
		if !ok || b.Op != "==" {
			return false
		}
		arg, ok := textArg(b.L)
		lit, isConst := b.R.(*ir.Const)
		if !ok || !isConst {
			arg, ok = textArg(b.R)
			lit, isConst = b.L.(*ir.Const)
		}
		if !ok || !isConst {
			return false
		}
		s, isStr := lit.Value.(string)
		var n string
		switch c := arg.(type) {
		case *ir.SlotRef:
			n = c.Name
		case *ir.Local:
			if c.Kind == ir.LocalParam {
				return false
			}
			n = c.Name
		default:
			return false
		}
		if !isStr || (name != "" && n != name) {
			return false
		}
		x, name = arg, n
		if !slices.Contains(lits, s) {
			lits = append(lits, s)
		}
		return true
	}
	if !walk(t) {
		return nil, nil, false
	}
	return x, lits, true
}

// textArg returns the argument of t if t is a call text(x).
func textArg(t ir.ActionExpr) (ir.ActionExpr, bool) {
	if c, ok := t.(*ir.Builtin); ok && c.Func == "text" && len(c.Args) == 1 {
		return c.Args[0], true
	}
	return nil, false
}

// memberKind returns the kind of the member read name (action.MemberKinds) and the name of the field or
// capture that it reads.
func memberKind(name string) (int, string) {
	prefix := -1
	for code, k := range action.MemberKinds {
		switch {
		case code == action.MemberCapture:
			prefix = code
		case k.Spelling != "" && name == k.Spelling:
			return code, name
		}
	}
	if prefix >= 0 {
		if field, ok := strings.CutPrefix(name, action.MemberKinds[prefix].Spelling); ok {
			return prefix, field
		}
	}
	return action.MemberField, name
}

// readOf returns the immediate operand of action.read of the capture, local or variable t.
func readOf(t ir.ActionExpr) action.Read {
	switch t := t.(type) {
	case *ir.SlotRef:
		return action.Read{Kind: action.ReadCapture, Name: t.Name}
	case *ir.Local:
		return action.Read{Kind: action.ReadLocal, Name: t.Name}
	case *ir.VarRef:
		return action.Read{Kind: action.ReadVar, Name: t.Name}
	}
	panic(fmt.Sprintf("lower: a read of %T", t))
}

// castOf returns the immediate operand of action.castType and action.castError of the cast t.
func castOf(t *ir.Cast) action.Cast {
	return action.Cast{Check: t.Check, Key: t.Key, Line: t.Line, Col: t.Col}
}

// newOf returns the immediate operand of action.struct and action.newN of the struct literal t.
func newOf(t *ir.New) action.New {
	return action.New{Type: t.Type, Fields: len(t.Fields), Final: t.Final}
}
