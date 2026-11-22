package examinedref

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The values of the action language (spec/language/actions.md and types.md). Predicates decide
// matches, and they read the values that matching determines (spec/language/parser-expressions.md,
// Values and the Concrete Syntax Tree) and the values of Value Actions, so the reference evaluates
// both by the specification. It does not use the producers' evaluator of actions, which runs on the
// state of a parser.

// value is nil, an int64, a string, a bool, a *node, a list (a list value), a tuple (the value of a
// tuple node), a *structValue or a *lambda.
type value any

type list []value
type tuple []value

// nodeKind tells the nodes of the reference apart.
type nodeKind int

const (
	stringNode  nodeKind = iota // a literal, class, `.` or #atomic: the matched text
	tupleNode                   // a sequence, the unit, an iteration with captures, an operator
	listNode                    // a repetition
	errorNode                   // a range that #recover skipped
	missingNode                 // a token that #missing inserted
)

// node is a value with the range of input it covers.
type node struct {
	kind       nodeKind
	start, end int
	str        string  // the matched text of a string node
	elems      []value // the elements of a tuple or list node
	caps       *capture
	name       string // the named node type of the rule that made it, if any
	expected   string // the inserted text of a missing node
	message    string // the recovered error of a missing node
}

// structValue is a value of a struct type: the fields that its constructor sets. A field that it
// does not set reads as the zero value of its declared type.
type structValue struct {
	typ    string
	fields map[string]value
}

// lambda is a function argument of foldl, foldr, map or filter.
type lambda struct {
	params []string
	body   ast.ActionExpr
}

// binding is a variable binding; the newest is first and shadows older ones.
type binding struct {
	name string
	val  value
	next *binding
}

func (b *binding) lookup(name string) (value, bool) {
	for ; b != nil; b = b.next {
		if b.name == name {
			return b.val, true
		}
	}
	return nil, false
}

// capture is a capture of the current scope; the newest is first.
type capture struct {
	name string
	val  value
	next *capture
}

func (c *capture) lookup(name string) (value, bool) {
	for ; c != nil; c = c.next {
		if c.name == name {
			return c.val, true
		}
	}
	return nil, false
}

// since returns the captures that c holds beyond base, newest first.
func (c *capture) since(base *capture) *capture {
	var out []*capture
	for ; c != nil && c != base; c = c.next {
		out = append(out, c)
	}
	var r *capture
	for i := len(out) - 1; i >= 0; i-- {
		r = &capture{name: out[i].name, val: out[i].val, next: r}
	}
	return r
}

// errAction is a run-time action error: it fails the predicate that raised it, and it makes a parse
// that runs a Value Action that raises it fail.
var errAction = errors.New("action error")

// actionEnv is what an action expression reads: the variables, the captures of its scope and the
// lambda parameters.
type actionEnv struct {
	ev     *evaluator
	vars   *binding
	caps   *capture
	params *binding
}

// eval evaluates an action expression.
func (a *actionEnv) eval(x ast.ActionExpr) (value, error) {
	switch x := x.(type) {
	case *ast.ActionGroup:
		return a.eval(x.Action)
	case *ast.IntLit:
		return int64(x.Value), nil
	case *ast.StringLit:
		return x.Value, nil
	case *ast.BoolLit:
		return x.Value, nil
	case *ast.NilLit:
		return nil, nil
	case *ast.CaptureRef:
		if x.Param {
			// A lambda parameter, written without $.
			v, ok := a.params.lookup(x.Name)
			if !ok {
				return nil, fmt.Errorf("examinedref: lambda parameter %s is not bound", x.Name)
			}
			return v, nil
		}
		v, _ := a.caps.lookup(x.Name)
		return v, nil
	case *ast.VarRef:
		if v, ok := a.params.lookup(x.Name); ok {
			return v, nil
		}
		if v, ok := a.vars.lookup(x.Name); ok {
			return v, nil
		}
		if v, ok := a.ev.initial[x.Name]; ok {
			return v, nil
		}
		return nil, fmt.Errorf("examinedref: variable %s is not declared", x.Name)
	case *ast.Lambda:
		return &lambda{params: x.Params, body: x.Body}, nil
	case *ast.Unary:
		v, err := a.eval(x.X)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case "-":
			if i, ok := v.(int64); ok {
				return -i, nil
			}
		case "!":
			if b, ok := v.(bool); ok {
				return !b, nil
			}
		}
		return nil, errAction
	case *ast.Binary:
		return a.binary(x)
	case *ast.FieldAccess:
		v, err := a.eval(x.X)
		if err != nil {
			return nil, err
		}
		return a.member(v, x.Name)
	case *ast.Cast:
		v, err := a.eval(x.X)
		if err != nil {
			return nil, err
		}
		return a.cast(v, x.Type)
	case *ast.Call:
		return a.call(x)
	case *ast.TupleLit:
		var t tuple
		for _, e := range x.Elems {
			v, err := a.eval(e)
			if err != nil {
				return nil, err
			}
			t = append(t, v)
		}
		return t, nil
	case *ast.Constructor:
		if len(x.Args) > 0 {
			return a.convertTo(x)
		}
		sv := &structValue{typ: x.Type, fields: map[string]value{}}
		for _, f := range x.Fields {
			v, err := a.eval(f.Value)
			if err != nil {
				return nil, err
			}
			sv.fields[f.Name] = v
		}
		return sv, nil
	case *ast.ListConstructor:
		var l list
		for _, e := range x.Items {
			v, err := a.eval(e)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
		}
		return l, nil
	}
	return nil, &UnsupportedError{Construct: fmt.Sprintf("%T in an action", x)}
}

func (a *actionEnv) binary(x *ast.Binary) (value, error) {
	l, err := a.eval(x.L)
	if err != nil {
		return nil, err
	}
	switch x.Op {
	case "&&", "||":
		b, ok := l.(bool)
		if !ok {
			return nil, errAction
		}
		if (x.Op == "&&") != b {
			return b, nil
		}
		r, err := a.eval(x.R)
		if err != nil {
			return nil, err
		}
		if rb, ok := r.(bool); ok {
			return rb, nil
		}
		return nil, errAction
	}
	r, err := a.eval(x.R)
	if err != nil {
		return nil, err
	}
	switch x.Op {
	case "[]":
		i, ok := r.(int64)
		if !ok {
			return nil, errAction
		}
		var elems []value
		switch l := l.(type) {
		case list:
			elems = l
		case tuple:
			elems = l
		case nil:
		default:
			return nil, errAction
		}
		if i < 0 || i >= int64(len(elems)) {
			return nil, errAction
		}
		return elems[i], nil
	case "==", "!=":
		eq, ok := equal(l, r)
		if !ok {
			return nil, errAction
		}
		return eq == (x.Op == "=="), nil
	case "<", "<=", ">", ">=":
		var c int
		switch l := l.(type) {
		case int64:
			ri, ok := r.(int64)
			if !ok {
				return nil, errAction
			}
			c = compareInt(l, ri)
		case string:
			rs, ok := r.(string)
			if !ok {
				return nil, errAction
			}
			// Strings are ordered by code point, which is the order of their UTF-8 bytes.
			c = strings.Compare(l, rs)
		default:
			return nil, errAction
		}
		switch x.Op {
		case "<":
			return c < 0, nil
		case "<=":
			return c <= 0, nil
		case ">":
			return c > 0, nil
		}
		return c >= 0, nil
	case "+":
		if ls, ok := l.(string); ok {
			if rs, ok := r.(string); ok {
				return ls + rs, nil
			}
			return nil, errAction
		}
		fallthrough
	case "-", "*", "/", "%":
		li, ok1 := l.(int64)
		ri, ok2 := r.(int64)
		if !ok1 || !ok2 {
			return nil, errAction
		}
		switch x.Op {
		case "+":
			return li + ri, nil
		case "-":
			return li - ri, nil
		case "*":
			return li * ri, nil
		}
		if ri == 0 {
			return nil, errAction
		}
		if ri == -1 {
			// The quotient wraps; the remainder is 0.
			if x.Op == "/" {
				return -li, nil
			}
			return int64(0), nil
		}
		if x.Op == "/" {
			return li / ri, nil
		}
		return li % ri, nil
	}
	return nil, &UnsupportedError{Construct: "operator " + x.Op + " in an action"}
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// equal compares two values as == does; ok is false for a comparison that is an error.
func equal(l, r value) (eq, ok bool) {
	if l == nil || r == nil {
		return l == nil && r == nil, true
	}
	switch l := l.(type) {
	case int64:
		ri, ok := r.(int64)
		return ok && l == ri, ok
	case string:
		rs, ok := r.(string)
		return ok && l == rs, ok
	case bool:
		rb, ok := r.(bool)
		return ok && l == rb, ok
	}
	return false, false
}

// member reads a field of a constructed struct, .Start, .End, .Value, the fields of a missing value,
// or a capture of a node.
func (a *actionEnv) member(v value, name string) (value, error) {
	if sv, ok := v.(*structValue); ok {
		if f, ok := sv.fields[name]; ok {
			return f, nil
		}
		// An unset field reads as the zero value of its declared type; a field that the type does not
		// declare, or a capture, is a runtime action error.
		if t, ok := a.ev.structFields[sv.typ][name]; ok {
			return a.ev.zeroValue(t)
		}
		return nil, errAction
	}
	n, ok := v.(*node)
	if !ok {
		return nil, errAction
	}
	switch name {
	case "Start":
		return int64(n.start), nil
	case "End":
		return int64(n.end), nil
	case "Value":
		switch n.kind {
		case stringNode:
			return n.str, nil
		case tupleNode:
			return tuple(n.elems), nil
		case listNode:
			return list(n.elems), nil
		}
		return nil, nil
	case "expected":
		if n.kind == missingNode {
			return n.expected, nil
		}
	case "message":
		if n.kind == missingNode {
			return n.message, nil
		}
	}
	capName := strings.TrimPrefix(name, "$")
	if r, _ := utf8.DecodeRuneInString(capName); capName != "" && (strings.HasPrefix(name, "$") || !unicode.IsUpper(r)) {
		c, _ := n.caps.lookup(capName)
		return c, nil
	}
	return nil, &UnsupportedError{Construct: "field " + name + " in an action"}
}

// cast checks v against a type that matching determines.
func (a *actionEnv) cast(v value, t ast.TypeExpr) (value, error) {
	ok, err := a.fits(v, t)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errAction
	}
	return v, nil
}

func (a *actionEnv) fits(v value, t ast.TypeExpr) (bool, error) {
	switch t := t.(type) {
	case *ast.TypeGroup:
		return a.fits(v, t.Type)
	case *ast.JoinType:
		for _, m := range t.Types {
			ok, err := a.fits(v, m)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	case *ast.OptionalType:
		if v == nil {
			return true, nil
		}
		return a.fits(v, t.Elem)
	case *ast.MeetType:
		for _, m := range t.Types {
			ok, err := a.fits(v, m)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case *ast.TupleType:
		tv, ok := v.(tuple)
		if !ok || len(tv) != len(t.Types) {
			return false, nil
		}
		for i, e := range tv {
			if ok, err := a.fits(e, t.Types[i]); err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case *ast.ListType:
		l, ok := v.(list)
		if !ok {
			return false, nil
		}
		for _, e := range l {
			if ok, err := a.fits(e, t.Elem); err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case *ast.TypeApp:
		n, ok := v.(*node)
		if t.Name != "node" || len(t.Args) != 1 {
			break
		}
		if !ok {
			return false, nil
		}
		return a.underlyingFits(n, t.Args[0])
	case *ast.TypeRef:
		switch t.Name {
		case "_":
			return true, nil
		case "int":
			_, ok := v.(int64)
			return ok, nil
		case "string":
			_, ok := v.(string)
			return ok, nil
		case "bool":
			_, ok := v.(bool)
			return ok, nil
		case "error":
			n, ok := v.(*node)
			return ok && (n.kind == errorNode || n.kind == missingNode), nil
		case "missing":
			n, ok := v.(*node)
			return ok && n.kind == missingNode, nil
		}
		if a.ev.nodeTypes[t.Name] {
			n, ok := v.(*node)
			return ok && n.name == t.Name, nil
		}
		if _, ok := a.ev.structFields[t.Name]; ok {
			sv, ok := v.(*structValue)
			return ok && sv.typ == t.Name, nil
		}
		if alias, ok := a.ev.aliases[t.Name]; ok {
			return a.fits(v, alias)
		}
		if t.Name == "any" {
			return true, nil
		}
	}
	return false, &UnsupportedError{Construct: fmt.Sprintf("a cast to %T in an action", t)}
}

// underlyingFits checks the value of a node against U of node[U].
func (a *actionEnv) underlyingFits(n *node, u ast.TypeExpr) (bool, error) {
	if r, ok := u.(*ast.TypeRef); ok && r.Name == "_" {
		return n.kind != errorNode && n.kind != missingNode, nil
	}
	switch n.kind {
	case stringNode:
		return a.fits(n.str, u)
	case listNode:
		return a.fits(list(n.elems), u)
	case tupleNode:
		return a.fits(tuple(n.elems), u)
	}
	// A recovered value has no value.
	return false, nil
}

// call evaluates a built-in function.
func (a *actionEnv) call(x *ast.Call) (value, error) {
	args := make([]value, len(x.Args))
	for i, e := range x.Args {
		v, err := a.eval(e)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	switch x.Func {
	case "len":
		if len(args) != 1 {
			break
		}
		switch v := args[0].(type) {
		case nil:
			return int64(0), nil
		case string:
			return int64(a.ev.in.length(v)), nil
		case list:
			return int64(len(v)), nil
		case *node:
			switch v.kind {
			case stringNode:
				return int64(v.end - v.start), nil
			case tupleNode, listNode:
				return int64(len(v.elems)), nil
			}
			return int64(0), nil
		}
		return nil, errAction
	case "text":
		if len(args) != 1 {
			break
		}
		switch v := args[0].(type) {
		case nil:
			return "", nil
		case string:
			return v, nil
		case *node:
			if v.kind == stringNode {
				return v.str, nil
			}
			if v.kind == missingNode {
				return "", nil
			}
			return a.ev.in.slice(v.start, v.end), nil
		}
		return nil, errAction
	case "list":
		return list(args), nil
	case "concat":
		var out list
		for _, v := range args {
			out = append(out, elements(v)...)
		}
		if out == nil {
			out = list{}
		}
		return out, nil
	case "map", "filter":
		if len(args) != 2 {
			break
		}
		f, ok := args[1].(*lambda)
		if !ok {
			return nil, errAction
		}
		out := list{}
		for _, e := range elements(args[0]) {
			r, err := a.apply(f, e)
			if err != nil {
				return nil, err
			}
			if x.Func == "map" {
				out = append(out, r)
				continue
			}
			b, ok := r.(bool)
			if !ok {
				return nil, errAction
			}
			if b {
				out = append(out, e)
			}
		}
		return out, nil
	case "foldl", "foldr":
		if len(args) != 3 {
			break
		}
		f, ok := args[2].(*lambda)
		if !ok {
			return nil, errAction
		}
		acc := args[0]
		elems := elements(args[1])
		for i := range elems {
			e := elems[i]
			if x.Func == "foldr" {
				e = elems[len(elems)-1-i]
			}
			r, err := a.apply(f, acc, e)
			if err != nil {
				return nil, err
			}
			acc = r
		}
		return acc, nil
	}
	return nil, &UnsupportedError{Construct: "function " + x.Func + " in an action"}
}

// elements returns the elements of a list value, a list or tuple node, or nothing for nil and a
// recovered value.
func elements(v value) []value {
	switch v := v.(type) {
	case list:
		return v
	case tuple:
		return v
	case *node:
		if v.kind == tupleNode || v.kind == listNode {
			return v.elems
		}
	}
	return nil
}

// apply calls a lambda with its parameters bound to args.
func (a *actionEnv) apply(f *lambda, args ...value) (value, error) {
	if len(f.params) != len(args) {
		return nil, errAction
	}
	inner := *a
	for i, p := range f.params {
		inner.params = &binding{name: p, val: args[i], next: inner.params}
	}
	v, err := inner.eval(f.body)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// actionValue is the value of a Value Action with the variables vars and the captures caps. A runtime
// error aborts the parse; the reference records where (Result.ActionErrors) and goes on with nil.
func (ev *evaluator) actionValue(act ast.ActionExpr, vars *binding, caps *capture) value {
	v, err := (&actionEnv{ev: ev, vars: vars, caps: caps}).eval(act)
	var uns *UnsupportedError
	switch {
	case errors.As(err, &uns):
		ev.fail(err)
	case err != nil:
		ev.actionErrors = append(ev.actionErrors, len(ev.exits))
	}
	return v
}

// convertTo evaluates N{x}, the conversion of the node x to the named node type N: the node keeps its
// range and content and takes the name N (spec/language/types.md, Named Node Types).
func (a *actionEnv) convertTo(x *ast.Constructor) (value, error) {
	if len(x.Args) != 1 || len(x.Fields) > 0 {
		return nil, fmt.Errorf("examinedref: constructor %s takes one positional argument", x.Type)
	}
	v, err := a.eval(x.Args[0])
	if err != nil {
		return nil, err
	}
	n, ok := v.(*node)
	if !ok || n.kind == errorNode || n.kind == missingNode {
		return nil, errAction
	}
	c := *n
	c.name = x.Type
	return &c, nil
}

// zeroValue is the zero value of the type t (spec/language/types.md, Zero Values). The zero value of
// a struct is the struct with no field set.
func (ev *evaluator) zeroValue(t ast.TypeExpr) (value, error) {
	switch t := t.(type) {
	case *ast.TypeGroup:
		return ev.zeroValue(t.Type)
	case *ast.ListType:
		return list{}, nil
	case *ast.OptionalType:
		return nil, nil
	case *ast.TupleType:
		out := tuple{}
		for _, e := range t.Types {
			v, err := ev.zeroValue(e)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case *ast.TypeRef:
		switch t.Name {
		case "int":
			return int64(0), nil
		case "string":
			return "", nil
		case "bool":
			return false, nil
		case "_":
			return nil, nil
		}
		if _, ok := ev.structFields[t.Name]; ok {
			return &structValue{typ: t.Name, fields: map[string]value{}}, nil
		}
		if alias, ok := ev.aliases[t.Name]; ok {
			return ev.zeroValue(alias)
		}
	}
	return nil, fmt.Errorf("examinedref: type %T has no zero value", t)
}
