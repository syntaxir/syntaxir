package pass

import (
	"fmt"
	"slices"
	"strconv"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The passes of the Action module of a Go table.
//
// The typed table of a Go parser builds the values of the typed runtime, whose library makes a struct
// value by the constructor of its type (go-struct-call) and compares a field of a value without
// boxing it (field-compare); the Node and recognizer tables do neither (TypedFeature). Go prints the
// action of a flat rule in place, into a local of the rule (go-in-place).

// ActionUnit is the unit of an Action pass: an Action module, or one that holds the function of
// one site (code.ActionFuncs).
type ActionUnit = code.At[code.Action]

// TypedFeature reports that the actions of the table build the values of the typed runtime.
var TypedFeature = pipe.NewFeature("typed-values", strconv.FormatBool, strconv.ParseBool)

// The names of the Action passes of Go.
const (
	PassFieldCompare = "field-compare"
	PassStructCall   = "go-struct-call"
	PassInPlace      = "go-in-place"
)

// InPlaceResult is the local that the function of a rule's action in place assigns the value to
// (action.Site.Result), which the printer renames where it prints the function.
const InPlaceResult = "tr"

func init() {
	pass.Register(pass.Flag{Name: PassFieldCompare, Kind: pass.KindActionPass, Default: 1,
		Doc: "typed Go compares fields in the actions and predicates of typed rules without boxing string fields"})
}

// siteOf returns the function of a unit of one site and its attributes.
func siteOf(u ActionUnit) (*code.Func, *action.Site) {
	f := u.Module().Funcs[0]
	return f, action.SiteOf(f)
}

// FieldCompare is the pass field-compare: in the typed table, a comparison (==, !=, <, <=, >, >=)
// of which an operand reads a field of a value, x.F, is a call of action.compare on operands, the
// field read by action.fieldOperand and any other operand made one by action.operand, so that a
// string field is compared without making a value of it.
var FieldCompare = pipe.New(pipe.Def[ActionUnit, ActionUnit]{
	Decl: pipe.Decl{Name: PassFieldCompare, Features: []pipe.FeatureID{TypedFeature.ID()},
		Obligation: "Each comparison has the value and the failure of the comparison of its operands' values."},
	In: code.ActionStage, Out: code.ActionStage,
	Applies: func(c *pipe.Ctx[ActionUnit]) bool { return pipe.Feature(c, TypedFeature) },
	Run: func(_ *pipe.Ctx[ActionUnit], u ActionUnit) (ActionUnit, pass.PreservedSet, error) {
		f, _ := siteOf(u)
		if fieldCompare(f) == 0 {
			return u, pass.PreserveAll(), nil
		}
		return u, pass.PreserveNone(), nil
	},
})

// comparison reports whether op is the code of a comparison operator of binary (action.BinaryOps).
func comparison(op int64) bool {
	if op < 0 || int(op) >= len(action.BinaryOps) {
		return false
	}
	switch action.BinaryOps[op] {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// fieldCompare rewrites the comparisons of f that read a field and returns their number.
func fieldCompare(f *code.Func) int {
	// The definitions of the locals that the reads of members let with their failures.
	members := map[string]*code.Let{}
	code.Walk(f.Body, func(s code.Stmt) {
		if l, ok := s.(*code.Let); ok && len(l.Names) == 2 {
			if c, ok := l.Value.(*code.Call); ok && c.Callee == "member" && len(c.Args) == 5 {
				members[l.Names[0]] = l
			}
		}
	}, nil)
	// fieldRead returns the definition of the read x.F that the operand e holds, if it reads a field.
	fieldRead := func(e code.Expr) *code.Let {
		x, ok := e.(*code.Local)
		if !ok {
			return nil
		}
		l := members[x.Name]
		if l == nil {
			return nil
		}
		name, ok := l.Value.(*code.Call).Args[3].(*code.Call)
		if !ok || name.Callee != action.OpActionName {
			return nil
		}
		s, _ := name.Imm.(string)
		if s != "" && s[0] == '$' {
			return nil
		}
		return l
	}
	nm := action.NewNamer(f)
	rename := map[string]string{}
	n := 0
	code.Walk(f.Body, func(s code.Stmt) {
		l, ok := s.(*code.Let)
		if !ok || len(l.Names) != 2 {
			return
		}
		c, ok := l.Value.(*code.Call)
		if !ok || c.Callee != "binary" || len(c.Args) != 3 {
			return
		}
		op, ok := c.Args[0].(*code.Const)
		if !ok {
			return
		}
		v, ok := op.Value.(int64)
		if !ok || !comparison(v) {
			return
		}
		lx, ly := fieldRead(c.Args[1]), fieldRead(c.Args[2])
		if lx == nil && ly == nil {
			return
		}
		operand := func(e code.Expr, read *code.Let) code.Expr {
			if read == nil {
				return code.C(action.OpActionOperand, e)
			}
			m := read.Value.(*code.Call)
			k := nm.Next()
			to, tf := action.LocalName("to", k), action.LocalName("tf", k)
			rename[read.Names[0]], rename[read.Names[1]] = to, tf
			read.Names = []string{to, tf}
			read.Value = &code.Call{Callee: action.OpActionFieldOperand, Args: []code.Expr{m.Args[2]}, Imm: m.Args[3].(*code.Call).Imm}
			return code.L(to)
		}
		x, y := operand(c.Args[1], lx), operand(c.Args[2], ly)
		l.Value = &code.Call{Callee: action.OpActionCompare, Args: []code.Expr{x, y}, Imm: action.BinaryOps[v]}
		n++
	}, nil)
	if n > 0 {
		renameLocals(f, rename)
	}
	return n
}

// renameLocals renames the reads of the locals of f by rename, once for each Local node.
func renameLocals(f *code.Func, rename map[string]string) {
	seen := map[*code.Local]bool{}
	code.Walk(f.Body, nil, func(e code.Expr) {
		if l, ok := e.(*code.Local); ok && !seen[l] {
			seen[l] = true
			if r, ok := rename[l.Name]; ok {
				l.Name = r
			}
		}
	})
}

// StructCall is the step go-struct-call: in the typed table, a struct literal that the lowering made of
// its fields (action.Site.Structs) is one call of the constructor of its type (action.newN), which
// takes the field values in the order declared, nil for a field not given. The values are evaluated
// in the order written: a value that the lowering let stays let if the fields are not in the order
// declared, unless it is a local or a constant; in the order declared, each value is passed as the
// lowering computed it. The typed library has no other form of a struct.
var StructCall = pipe.New(pipe.Def[ActionUnit, ActionUnit]{
	Decl: pipe.Decl{Name: PassStructCall, Required: true, Features: []pipe.FeatureID{TypedFeature.ID()},
		Obligation: "Each struct literal makes the struct of the field values that it made of its fields."},
	In: code.ActionStage, Out: code.ActionStage,
	// A required step runs on every table: it changes the struct literals of the typed table alone.
	Run: func(c *pipe.Ctx[ActionUnit], u ActionUnit) (ActionUnit, pass.PreservedSet, error) {
		f, s := siteOf(u)
		if !pipe.Feature(c, TypedFeature) || len(s.Structs) == 0 {
			return u, pass.PreserveAll(), nil
		}
		nm := action.NewNamer(f)
		for _, lit := range s.Structs {
			if err := structCall(u.Module(), f, nm, lit); err != nil {
				return u, pass.PreserveNone(), fmt.Errorf("%s: %w", f.Name, err)
			}
		}
		next := *s
		next.Structs = nil
		f.Attrs = &next
		return u, pass.PreserveNone(), nil
	},
})

// settled reports whether e is a local or a constant, which is evaluated already.
func settled(e code.Expr) bool {
	switch e := e.(type) {
	case *code.Local, *code.Const:
		return true
	case *code.Call:
		return e.Callee == action.OpActionConst
	}
	return false
}

// structCall makes the struct literal lit of f one call of the constructor of its type, declared in m.
func structCall(m *code.Module, f *code.Func, nm *action.Namer, lit action.StructLit) error {
	found := false
	var made string
	var err error
	code.EachList(f.Body, func(stmts []code.Stmt) []code.Stmt {
		if found || err != nil {
			return stmts
		}
		j := slices.IndexFunc(stmts, func(s code.Stmt) bool {
			l, ok := s.(*code.Let)
			return ok && len(l.Names) == 1 && l.Names[0] == lit.Node
		})
		if j < 0 {
			return stmts
		}
		found = true
		var out []code.Stmt
		out, made, err = structCallAt(m, stmts, j, nm, lit)
		return out
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no struct %s", lit.Node)
	}
	// The value of the struct, which the lowering read as the object of the struct, is the value
	// that the constructor returns.
	code.RewriteExprs(f.Body, func(e code.Expr) code.Expr {
		if c, ok := e.(*code.Call); ok && c.Callee == action.OpActionFromObj && len(c.Args) == 1 {
			if x, ok := c.Args[0].(*code.Local); ok && x.Name == lit.Node {
				return code.L(made)
			}
		}
		return e
	})
	return nil
}

// structCallAt rewrites the statements stmts, of which j lets the struct of lit, and returns them with
// the local that holds the constructed value.
func structCallAt(m *code.Module, stmts []code.Stmt, j int, nm *action.Namer, lit action.StructLit) ([]code.Stmt, string, error) {
	n := len(lit.Lets)
	// The struct, its first flag, a set and a cover for each field, and made.
	if j+2+2*n >= len(stmts) {
		return nil, "", fmt.Errorf("the struct %s ends early", lit.Node)
	}
	newCall, ok := stmts[j].(*code.Let).Value.(*code.Call)
	if !ok || newCall.Callee != action.OpActionStruct {
		return nil, "", fmt.Errorf("%s is not a struct", lit.Node)
	}
	names := make([]string, n)
	vals := make([]code.Expr, n)
	for i := range n {
		d, ok := stmts[j+2+2*i].(*code.Do)
		if !ok || d.Call.Callee != action.OpActionSetField || len(d.Call.Args) != 2 {
			return nil, "", fmt.Errorf("field %d of the struct %s is not set", i, lit.Node)
		}
		names[i], _ = d.Call.Imm.(string)
		vals[i] = d.Call.Args[1]
	}
	if d, ok := stmts[j+2+2*n].(*code.Do); !ok || d.Call.Callee != "made" {
		return nil, "", fmt.Errorf("the struct %s is not made", lit.Node)
	}
	// The order of the fields as written, against the declared order.
	inOrder, next := true, 0
	for _, name := range names {
		for next < len(lit.Declared) && lit.Declared[next] != name {
			next++
		}
		if next == len(lit.Declared) {
			inOrder = false
			break
		}
		next++
	}
	// The lets of the values that are passed as they are.
	drop := map[int]bool{}
	for i, let := range lit.Lets {
		if let == "" {
			continue
		}
		k := slices.IndexFunc(stmts[:j], func(s code.Stmt) bool {
			l, ok := s.(*code.Let)
			return ok && len(l.Names) == 1 && l.Names[0] == let
		})
		if k < 0 {
			return nil, "", fmt.Errorf("the value of field %s of the struct %s is not let", names[i], lit.Node)
		}
		if v := stmts[k].(*code.Let).Value; inOrder || settled(v) {
			vals[i], drop[k] = v, true
		}
	}
	args := make([]code.Expr, len(lit.Declared))
	for i, d := range lit.Declared {
		args[i] = &code.Call{Callee: action.OpActionConst, Imm: action.Const{Value: nil}}
		if k := slices.Index(names, d); k >= 0 {
			args[i] = vals[k]
		}
	}
	name := action.OpActionNew + strconv.Itoa(len(lit.Declared))
	if m.Op(name) == nil {
		m.AddOp(action.NewOp(len(lit.Declared)))
	}
	made := action.LocalName("tv", nm.Next())
	var out []code.Stmt
	for k, s := range stmts[:j] {
		if !drop[k] {
			out = append(out, s)
		}
	}
	out = append(out, &code.Let{Names: []string{made}, Value: &code.Call{Callee: name, Args: args, Imm: newCall.Imm}})
	out = append(out, stmts[j+3+2*n:]...)
	return out, made, nil
}

// InPlace is the step go-in-place: for the function of each rule's action, it adds the function
// that evaluates the action in place (action.Site.Result): the value assigned to the local
// InPlaceResult last, and a failure raised by action.raise.
var InPlace = pipe.New(pipe.Def[ActionUnit, ActionUnit]{
	Decl: pipe.Decl{Name: PassInPlace, Required: true,
		Obligation: "The function in place of a rule's action assigns the value that the action returns and raises the failure that it returns."},
	In: code.ActionStage, Out: code.ActionStage,
	Run: func(_ *pipe.Ctx[ActionUnit], u ActionUnit) (ActionUnit, pass.PreservedSet, error) {
		m := u.Module()
		var add []*code.Func
		for _, f := range m.Funcs {
			s := action.SiteOf(f)
			if s == nil || s.Kind != action.SiteRule || s.Result != "" {
				continue
			}
			g, err := inPlace(f, s)
			if err != nil {
				return u, pass.PreserveNone(), err
			}
			add = append(add, g)
		}
		if len(add) == 0 {
			return u, pass.PreserveAll(), nil
		}
		m.Funcs = append(m.Funcs, add...)
		return u, pass.PreserveNone(), nil
	},
})

// InPlaceFunc returns the name of the function in place of the rule's action whose function is name.
func InPlaceFunc(name string) string { return name + "_result" }

// inPlace returns the function in place of the function f of a rule's action, with the attributes s.
func inPlace(f *code.Func, s *action.Site) (*code.Func, error) {
	g := code.CloneFunc(f)
	g.Name = InPlaceFunc(f.Name)
	g.Results = nil
	g.Locals = append([]code.Param{code.P(InPlaceResult, code.THandle(exec.Datum))}, g.Locals...)
	at := *s
	at.Result = InPlaceResult
	g.Attrs = &at
	stmts := g.Body.Stmts
	last, ok := stmts[len(stmts)-1].(*code.Return)
	if !ok || len(last.Values) != 2 {
		return nil, fmt.Errorf("%s: the function does not end by returning its value", f.Name)
	}
	stmts[len(stmts)-1] = code.Assign(InPlaceResult, last.Values[0])
	failed := false
	code.EachList(g.Body, func(stmts []code.Stmt) []code.Stmt {
		for i, st := range stmts {
			r, ok := st.(*code.Return)
			if !ok {
				continue
			}
			if len(r.Values) != 2 {
				failed = true
				continue
			}
			// A failure ends the parse.
			stmts[i] = &code.Do{Call: code.C(action.OpActionRaise, r.Values[1])}
		}
		return stmts
	})
	if failed {
		return nil, fmt.Errorf("%s: a return without a value and a failure", f.Name)
	}
	return g, nil
}
