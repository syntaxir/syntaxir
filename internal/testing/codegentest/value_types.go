package codegentest

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// ValueTypes decides the relation v : T of spec/proofs/type-soundness.md (Typing of Values) on the
// values that the backends return, for the types of one checked grammar: the conformance tests of
// the type-soundness theorem hold the value of every successful evaluation of a rule r they observe
// to its checked type T_r, and to be well formed. The relation is written from the clauses of the
// proof, one for one, and reads only the types of the grammar: it shares no code with the casts and
// the checks of the backends, so a fault in them does not also hide in it.
//
// It reads a value in one of two forms:
//   - a *value.Node of the engine or of a generated Go parser (CheckNode), which tells every kind of
//     value apart and holds the captures of nodes;
//   - the JSON of a value as value.Node and the TypeScript runtime write it (CheckJSON), which is
//     what the harnesses of the generated parsers print. The JSON writes a tuple and a list both as
//     an array, and every number alike, and leaves out the captures. So an array has the tuple type
//     or the list type that it is checked against, as the type decides which of the two it is; a
//     number has type int when it is an integer, whether the TypeScript runtime held it as a number
//     or a bigint; and the captures of a node are not checked.
type ValueTypes struct {
	env typeEnv
}

// typeEnv is what the relation reads of a grammar: the checked value type of each rule, the declared
// field types of each struct type, and the underlying node type of each named node type.
type typeEnv struct {
	rule   func(name string) (types.Ty, bool)
	fields func(name string) (map[string]types.Ty, bool)
	node   func(name string) (types.NodeTy, bool)
}

// ValueTypesOf returns the relation for the types of the grammar of c, checked with the start rule
// main as the generators check it.
func ValueTypesOf(t testing.TB, c Case) *ValueTypes {
	t.Helper()
	prog := checkMain(t, c)
	rules := prog.Check.ValueTypes()
	structs := map[string]map[string]types.Ty{}
	for name, td := range prog.Types {
		if _, ok := td.(*ast.TypeDef); ok {
			structs[name] = map[string]types.Ty{}
		}
	}
	if prog.Typed != nil {
		for name, fs := range prog.Typed.Fields {
			m := map[string]types.Ty{}
			for _, f := range fs {
				m[f.Name] = f.T
			}
			structs[name] = m
		}
	}
	return &ValueTypes{env: typeEnv{
		rule: func(name string) (types.Ty, bool) { x, ok := rules[name]; return x, ok },
		fields: func(name string) (map[string]types.Ty, bool) {
			m, ok := structs[name]
			return m, ok
		},
		node: prog.Check.NodeTypeOf,
	}}
}

// RuleType returns the checked value type T_r of the rule r, and whether the grammar has the rule.
func (vt *ValueTypes) RuleType(r string) (types.Ty, bool) { return vt.env.rule(r) }

// CheckNode returns nil if the value n that an evaluation of the rule r produced has the type of r
// and is well formed, and otherwise a description of the value and the type.
func (vt *ValueTypes) CheckNode(r string, n *value.Node) error {
	return vt.check(r, (&nodeReader{seen: map[*value.Node]*tv{}}).node(n))
}

// CheckJSON returns CheckNode for a value in its JSON form, decoded into Go values by encoding/json.
func (vt *ValueTypes) CheckJSON(r string, v any) error {
	return vt.check(r, fromJSON(v))
}

// CheckType returns nil if the value n has the type x and is well formed, for a type that no rule
// names, such as a narrower type than a rule's that a negative control checks against.
func (vt *ValueTypes) CheckType(x types.Ty, n *value.Node) error {
	return vt.checkAgainst(x, (&nodeReader{seen: map[*value.Node]*tv{}}).node(n))
}

func (vt *ValueTypes) check(r string, v *tv) error {
	x, ok := vt.env.rule(r)
	if !ok {
		return fmt.Errorf("the grammar has no rule %s", r)
	}
	if err := vt.checkAgainst(x, v); err != nil {
		return fmt.Errorf("rule %s: %w", r, err)
	}
	return nil
}

func (vt *ValueTypes) checkAgainst(x types.Ty, v *tv) error {
	if v != nil && v.bad != "" {
		return fmt.Errorf("the value cannot be read: %s", v.bad)
	}
	r := &relation{env: vt.env, memo: map[relKey]bool{}, open: map[relKey]bool{}}
	has := r.has(v, x)
	if r.err != nil {
		return r.err
	}
	if !has {
		return fmt.Errorf("the value %s does not have the type %s", v, x)
	}
	if err := r.wellFormed(v); err != nil {
		return fmt.Errorf("the value %s is not well formed: %w", v, err)
	}
	return nil
}

// vkind is what a value is, in the value model of the proof (M1).
type vkind uint8

const (
	vInt vkind = iota
	vString
	vBool
	vNode    // a string, tuple or list node: its value (val) is a string, a tuple or a list
	vError   // an error value of #recover
	vMissing // a missing value of #missing, which is also an error value
	vStruct
	vTuple
	vList
	// vArray is an array of the JSON form: a tuple or a list, which the JSON does not tell apart.
	vArray
	// vNumber is a number of the JSON form that is no integer, which no type of the language has
	// but _ and unknown.
	vNumber
)

// tv is a value of the proof's value model, read from one of the forms of ValueTypes. nil is the
// value nil. Values that the engine shares are shared here too, so that reading and checking a
// value with shared parts takes time in its size, not in the number of its paths.
type tv struct {
	k vkind
	// name is the name of a named node type of a node (or ""), and the struct type of a struct.
	name string
	i    int64
	s    string
	b    bool
	// val is the value of a node: a string, a tuple, a list or an array.
	val *tv
	// elems are the elements of a tuple, a list or an array.
	elems []*tv
	// fields are the fields that the constructor of a struct set.
	fields map[string]*tv
	// caps are the captures of a node, when the form holds them (capsKnown).
	caps      map[string]*tv
	capsKnown bool
	// bad describes a part of the value that is none of the model, which makes the whole value fail.
	bad string
}

// String renders v for the descriptions of failures, shortened.
func (v *tv) String() string {
	var b strings.Builder
	v.write(&b, 0)
	s := b.String()
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

func (v *tv) write(b *strings.Builder, depth int) {
	if v == nil {
		b.WriteString("nil")
		return
	}
	if depth > 6 || b.Len() > 400 {
		b.WriteString("...")
		return
	}
	list := func(open, close string, xs []*tv) {
		b.WriteString(open)
		for i, x := range xs {
			if i > 0 {
				b.WriteString(", ")
			}
			x.write(b, depth+1)
		}
		b.WriteString(close)
	}
	switch v.k {
	case vInt:
		fmt.Fprint(b, v.i)
	case vString:
		fmt.Fprintf(b, "%q", v.s)
	case vBool:
		fmt.Fprint(b, v.b)
	case vNumber:
		b.WriteString("number " + v.s)
	case vNode:
		if v.name != "" {
			b.WriteString(v.name)
		}
		b.WriteString("node(")
		v.val.write(b, depth+1)
		b.WriteString(")")
	case vError:
		b.WriteString("error")
	case vMissing:
		b.WriteString("missing")
	case vStruct:
		b.WriteString(v.name + "{")
		for i, k := range slices.Sorted(mapKeys(v.fields)) {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(k + ": ")
			v.fields[k].write(b, depth+1)
		}
		b.WriteString("}")
	case vTuple:
		list("(", ")", v.elems)
	case vList:
		list("[", "]", v.elems)
	case vArray:
		list("array[", "]", v.elems)
	}
}

func mapKeys(m map[string]*tv) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// nodeReader reads a *value.Node into the value model, sharing what the node shares.
type nodeReader struct {
	seen map[*value.Node]*tv
}

// node reads n. A box node (KindValue) is the scalar it holds.
func (r *nodeReader) node(n *value.Node) *tv {
	if n == nil {
		return nil
	}
	if v, ok := r.seen[n]; ok {
		return v
	}
	v := &tv{}
	r.seen[n] = v
	d := nodes.D(n)
	elems := func() []*tv {
		out := make([]*tv, n.Len())
		for i, e := range n.Elems() {
			out[i] = r.node(e)
		}
		return out
	}
	switch n.Kind() {
	case value.KindString, value.KindTuple, value.KindList:
		v.k = vNode
		if d.Kind.IsNodeType() {
			v.name = d.Kind.TypeName()
		}
		switch n.Kind() {
		case value.KindString:
			v.val = &tv{k: vString, s: n.Text()}
		case value.KindTuple:
			v.val = &tv{k: vTuple, elems: elems()}
		default:
			v.val = &tv{k: vList, elems: elems()}
		}
		// The captures of a node, which a tuple node of a rule body holds.
		v.capsKnown = true
		for _, f := range d.Fields {
			if v.caps == nil {
				v.caps = map[string]*tv{}
			}
			v.caps[f.Name] = r.field(f.Value)
		}
	case value.KindError:
		v.k = vError
		if d.Kind.Base == valueimpl.BaseMissing {
			v.k = vMissing
		}
	case value.KindStruct:
		v.k, v.name, v.fields = vStruct, n.Type(), map[string]*tv{}
		for name, x := range n.Fields() {
			v.fields[name] = r.field(x)
		}
	case value.KindTupleValue:
		v.k, v.elems = vTuple, elems()
	case value.KindListValue:
		v.k, v.elems = vList, elems()
	case value.KindValue:
		*v = *r.scalar(n.Value())
	default:
		v.bad = fmt.Sprintf("a node of the kind %s", n.Kind())
	}
	if v.bad == "" {
		v.bad = r.badOf(v)
	}
	return v
}

// field reads the value of a struct field or a capture: a node, an int, a string, a bool or nil.
func (r *nodeReader) field(x any) *tv {
	switch x := x.(type) {
	case *value.Node:
		return r.node(x)
	case nil:
		return nil
	}
	return r.scalar(x)
}

func (r *nodeReader) scalar(x any) *tv {
	switch x := x.(type) {
	case nil:
		// A box that holds no value is no value of the model.
		return &tv{bad: "a box node that holds nil"}
	case int:
		return &tv{k: vInt, i: int64(x)}
	case string:
		return &tv{k: vString, s: x}
	case bool:
		return &tv{k: vBool, b: x}
	case *value.Node:
		return r.node(x)
	}
	return &tv{bad: fmt.Sprintf("a value of the Go type %T", x)}
}

// badOf returns the first unreadable part of the parts of v, which makes v unreadable too.
func (r *nodeReader) badOf(v *tv) string {
	for _, x := range v.parts() {
		if x != nil && x.bad != "" {
			return x.bad
		}
	}
	return ""
}

// parts returns the values that v is made of: the value of a node and its captures, the elements
// of a tuple, a list or an array, and the fields of a struct.
func (v *tv) parts() []*tv {
	var out []*tv
	if v.val != nil {
		out = append(out, v.val)
	}
	out = append(out, v.elems...)
	for _, k := range slices.Sorted(mapKeys(v.fields)) {
		out = append(out, v.fields[k])
	}
	for _, k := range slices.Sorted(mapKeys(v.caps)) {
		out = append(out, v.caps[k])
	}
	return out
}

// fromJSON reads a value in its JSON form, as value.Node writes it (CanonicalEngine lists the
// shapes): a node is an object with "start", "end" and "value", and "type" for a named node type; a
// struct is an object with "type" and "fields"; an error value has "error", and a missing value also
// "expected". The keys of source information ("rule", "captures") are read past: the captures are
// written as their locations, not their values.
func fromJSON(x any) *tv {
	switch x := x.(type) {
	case nil:
		return nil
	case bool:
		return &tv{k: vBool, b: x}
	case string:
		return &tv{k: vString, s: x}
	case float64:
		if x == math.Trunc(x) && math.Abs(x) <= 1<<53 {
			return &tv{k: vInt, i: int64(x)}
		}
		return &tv{k: vNumber, s: fmt.Sprint(x)}
	case json.Number:
		// A number decoded with UseNumber is an int when it is one exactly, beyond the integers that
		// a float64 holds.
		if i, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return &tv{k: vInt, i: i}
		}
		f, err := x.Float64()
		if err != nil {
			return &tv{bad: fmt.Sprintf("the JSON number %s", x)}
		}
		return fromJSON(f)
	case []any:
		v := &tv{k: vArray, elems: make([]*tv, len(x))}
		for i, e := range x {
			v.elems[i] = fromJSON(e)
			if v.elems[i] != nil && v.elems[i].bad != "" && v.bad == "" {
				v.bad = v.elems[i].bad
			}
		}
		return v
	case map[string]any:
		if _, ok := x["error"]; ok {
			if err := onlyKeys(x, "start", "end", "error", "expected"); err != nil {
				return &tv{bad: err.Error()}
			}
			if _, ok := x["expected"]; ok {
				return &tv{k: vMissing}
			}
			return &tv{k: vError}
		}
		if fs, ok := x["fields"]; ok {
			if err := onlyKeys(x, "type", "start", "end", "fields"); err != nil {
				return &tv{bad: err.Error()}
			}
			name, _ := x["type"].(string)
			m, ok := fs.(map[string]any)
			if !ok || name == "" {
				return &tv{bad: fmt.Sprintf("a struct with the keys %v", keysOf(x))}
			}
			v := &tv{k: vStruct, name: name, fields: map[string]*tv{}}
			for k, f := range m {
				v.fields[k] = fromJSON(f)
				if v.fields[k] != nil && v.fields[k].bad != "" && v.bad == "" {
					v.bad = v.fields[k].bad
				}
			}
			return v
		}
		if err := onlyKeys(x, append([]string{"type", "start", "end", "value"}, engineDroppedKeys...)...); err != nil {
			return &tv{bad: err.Error()}
		}
		v := &tv{k: vNode}
		v.name, _ = x["type"].(string)
		switch val := x["value"].(type) {
		case string:
			v.val = &tv{k: vString, s: val}
		case []any:
			v.val = fromJSON(val)
			v.bad = v.val.bad
		default:
			v.bad = fmt.Sprintf("a node whose value is %T", val)
		}
		return v
	}
	return &tv{bad: fmt.Sprintf("a JSON value of the Go type %T", x)}
}

// relation decides v : T for the values of one check.
type relation struct {
	env typeEnv
	// memo holds the decided pairs; open the pairs being decided, which a rule type that names itself
	// without a smaller value meets again (a cycle has no value by the inductive definition).
	memo, open map[relKey]bool
	// err is a type of a kind that the relation has no clause for: the test fails on it rather than
	// decide it, so a kind of type that the checker comes to give gets its clause here.
	err error
}

type relKey struct {
	v *tv
	t string
}

// has reports whether v : x, by the clauses of the proof's definition.
func (r *relation) has(v *tv, x types.Ty) bool {
	key := relKey{v, fmt.Sprintf("%T:%s", x, x)}
	if got, ok := r.memo[key]; ok {
		return got
	}
	if r.open[key] {
		return false
	}
	r.open[key] = true
	got := r.decide(v, x)
	delete(r.open, key)
	r.memo[key] = got
	return got
}

func (r *relation) decide(v *tv, x types.Ty) bool {
	switch x := x.(type) {
	case types.BasicTy:
		switch x {
		case types.TyTop, types.TyAny:
			// v : _ and v : unknown for every v.
			return true
		case types.TyNever:
			// No v has type _|_.
			return false
		case types.TyInt:
			return v != nil && v.k == vInt
		case types.TyString:
			return v != nil && v.k == vString
		case types.TyBool:
			return v != nil && v.k == vBool
		case types.TyNil:
			return v == nil
		case types.TyNode:
			// node[_]: a string, tuple or list node, with or without a name.
			return v != nil && v.k == vNode
		case types.TyErr:
			// An error value, including a missing value.
			return v != nil && (v.k == vError || v.k == vMissing)
		}
	case types.NamedTy:
		if x == types.TyMissing {
			return v != nil && v.k == vMissing
		}
		if x.Kind == 's' {
			return r.isStruct(v, x.Name)
		}
	case types.OptTy:
		// v : *T if v is nil or v : T.
		return v == nil || r.has(v, x.Elem)
	case types.JoinTy:
		// v : T | U if v : T or v : U.
		return slices.ContainsFunc(x.Alts, func(a types.Ty) bool { return r.has(v, a) })
	case types.NodeTy:
		if v == nil || v.k != vNode {
			return false
		}
		if x.Name != "" {
			// v : N if v is a node named N and v : node[U].
			u, ok := r.env.node(x.Name)
			if !ok {
				r.fail(fmt.Errorf("the grammar has no named node type %s", x.Name))
				return false
			}
			return v.name == x.Name && r.has(v, u.Underlying())
		}
		// v : node[T] if the value of v has type T, and each capture that the type records is nil or
		// holds a value of its recorded type.
		if !r.has(v.val, x.Value) {
			return false
		}
		if v.capsKnown {
			for _, c := range x.Caps {
				if y := v.caps[c.Name]; y != nil && !r.has(y, c.T) {
					return false
				}
			}
		}
		return true
	case types.RecordTy:
		// The legacy node type of a tuple node whose fields are its captures.
		if v == nil || v.k != vNode || (v.val.k != vTuple && v.val.k != vArray) {
			return false
		}
		if v.capsKnown {
			for name, ft := range x.Fields {
				if y := v.caps[name]; y != nil && !r.has(y, ft) {
					return false
				}
			}
		}
		return true
	case types.TupleTy:
		// v : (T1, ..., Tn) if v is a tuple of n elements and each v_i : T_i.
		if v == nil || (v.k != vTuple && v.k != vArray) || len(v.elems) != len(x.Elems) {
			return false
		}
		for i, e := range x.Elems {
			if !r.has(v.elems[i], e) {
				return false
			}
		}
		return true
	case types.ListTy:
		// v : []T if v is a list and each element has type T.
		if v == nil || (v.k != vList && v.k != vArray) {
			return false
		}
		for _, e := range v.elems {
			if !r.has(e, x.Elem) {
				return false
			}
		}
		return true
	case types.RuleTy:
		// A type that names a rule stands for the value type of the rule.
		t, ok := r.env.rule(x.Name)
		if !ok {
			r.fail(fmt.Errorf("the grammar has no rule %s", x.Name))
			return false
		}
		return r.has(v, t)
	}
	r.fail(fmt.Errorf("the relation has no clause for the type %s (%T)", x, x))
	return false
}

// isStruct reports whether v : S: v is a struct of type S and every field its constructor set holds
// a value of the field's declared type.
func (r *relation) isStruct(v *tv, name string) bool {
	fields, ok := r.env.fields(name)
	if !ok {
		r.fail(fmt.Errorf("the grammar has no struct type %s", name))
		return false
	}
	if v == nil || v.k != vStruct || v.name != name {
		return false
	}
	for k, f := range v.fields {
		ft, ok := fields[k]
		if !ok || !r.has(f, ft) {
			return false
		}
	}
	return true
}

func (r *relation) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// wellFormed returns an error if a node named N within v does not have type N, or a struct of type
// S within v does not have type S.
func (r *relation) wellFormed(v *tv) error {
	seen := map[*tv]bool{}
	stack := []*tv{v}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == nil || seen[x] {
			continue
		}
		seen[x] = true
		switch {
		case x.k == vNode && x.name != "":
			if _, ok := r.env.node(x.name); !ok {
				return fmt.Errorf("a node is named %s, which is no named node type of the grammar", x.name)
			}
			if !r.has(x, types.NodeTy{Name: x.name}) {
				return fmt.Errorf("the node %s does not have its type %s", x, x.name)
			}
		case x.k == vStruct:
			if _, ok := r.env.fields(x.name); !ok {
				return fmt.Errorf("a struct has the type %s, which is no struct type of the grammar", x.name)
			}
			if !r.isStruct(x, x.name) {
				return fmt.Errorf("the struct %s does not have its type %s", x, x.name)
			}
		}
		if r.err != nil {
			return r.err
		}
		stack = append(stack, x.parts()...)
	}
	return nil
}
