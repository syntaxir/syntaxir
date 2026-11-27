package codegentest

import (
	"encoding/json"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/types"
)

// TestValueTypesClauses holds each clause of the relation v : T (spec/proofs/type-soundness.md,
// Typing of Values) to a value that it accepts and one that it rejects, in the JSON form and, where
// the JSON form tells apart what the clause reads, in the model that a node reads into.
func TestValueTypesClauses(t *testing.T) {
	str := types.NodeTy{Value: types.TyString}
	id := types.NodeTy{Value: types.TyString, Name: "Id"}
	tree := types.NodeTy{Name: "Tree"}
	vt := &ValueTypes{env: typeEnv{
		rule: func(name string) (types.Ty, bool) {
			switch name {
			case "main":
				return str, true
			case "loop":
				// A rule whose type names itself without a smaller value has no value.
				return types.JoinTy{Alts: []types.Ty{types.RuleTy{Name: "loop"}, types.TyNever}}, true
			}
			return nil, false
		},
		fields: func(name string) (map[string]types.Ty, bool) {
			if name == "S" {
				return map[string]types.Ty{"N": types.TyInt, "T": str}, true
			}
			return nil, false
		},
		node: func(name string) (types.NodeTy, bool) {
			switch name {
			case "Id":
				return id, true
			case "Tree":
				// type Tree = node[[]Tree]: recursive through a node, whose value is smaller.
				return types.NodeTy{Value: types.ListTy{Elem: tree}, Name: "Tree"}, true
			}
			return types.NodeTy{}, false
		},
	}}
	node := func(v string) string { return `{"start":0,"end":1,"value":` + v + `}` }
	named := func(name, v string) string { return `{"start":0,"end":1,"type":"` + name + `","value":` + v + `}` }
	caps := types.NodeTy{Value: types.TupleTy{Elems: []types.Ty{str}}, Caps: []types.CapIndex{{Name: "x", Paths: [][]int{{0}}, T: str}}}
	for _, tc := range []struct {
		name string
		t    types.Ty
		yes  []string
		no   []string
	}{
		{"_ and unknown hold every value", types.TyTop, []string{`1`, `null`, node(`"a"`), `{"start":0,"end":1,"error":"m"}`}, nil},
		{"unknown", types.TyAny, []string{`"a"`, `[1, "a"]`}, nil},
		{"_|_ holds none", types.TyNever, nil, []string{`null`, `1`, node(`"a"`)}},
		{"int", types.TyInt, []string{`0`, `-7`}, []string{`"1"`, `true`, `null`, `1.5`, node(`"1"`)}},
		{"string", types.TyString, []string{`""`, `"a"`}, []string{`1`, `null`, node(`"a"`)}},
		{"bool", types.TyBool, []string{`false`}, []string{`0`, `"true"`, `null`}},
		{"nil", types.TyNil, []string{`null`}, []string{`0`, `""`, `[]`}},
		{"*T is nil or a T", types.OptTy{Elem: types.TyInt}, []string{`null`, `3`}, []string{`"3"`}},
		{"T | U is a T or a U", types.JoinTy{Alts: []types.Ty{types.TyInt, str}}, []string{`1`, node(`"a"`)}, []string{`"a"`, `null`, `true`}},
		{"node[T] has a value of T", str, []string{node(`"a"`), named("Id", `"a"`)}, []string{`"a"`, node(`["a"]`), `{"start":0,"end":1,"error":"m"}`, `null`}},
		{"node[_] is any node, and no error", types.TyNode, []string{node(`"a"`), node(`[]`), named("Id", `"a"`)}, []string{`{"start":0,"end":1,"error":"m"}`, `"a"`, `[]`}},
		{"a named node type is a node of its name", id, []string{named("Id", `"a"`)}, []string{node(`"a"`), named("Other", `"a"`), named("Id", `["a"]`)}},
		{"a recursive named node type", tree, []string{named("Tree", `[]`), named("Tree", `[`+named("Tree", `[]`)+`]`)}, []string{named("Tree", `[`+node(`[]`)+`]`), named("Tree", `"a"`)}},
		{"a struct type", types.NamedTy{Name: "S", Kind: 's'},
			[]string{`{"type":"S","start":0,"end":1,"fields":{}}`, `{"type":"S","start":0,"end":1,"fields":{"N":1,"T":` + node(`"a"`) + `}}`},
			[]string{`{"type":"S","start":0,"end":1,"fields":{"N":"1"}}`, `{"type":"S","start":0,"end":1,"fields":{"T":"a"}}`, `{"type":"S","start":0,"end":1,"fields":{"Z":1}}`, `{"type":"R","start":0,"end":1,"fields":{}}`, `null`}},
		{"a tuple type has its arity and its element types", types.TupleTy{Elems: []types.Ty{types.TyInt, types.TyString}}, []string{`[1, "a"]`}, []string{`[1]`, `[1, "a", 2]`, `["a", 1]`, `null`}},
		{"[]T has elements of T", types.ListTy{Elem: types.TyInt}, []string{`[]`, `[1, 2]`}, []string{`[1, "a"]`, `null`, `1`}},
		{"error is an error or a missing value", types.TyErr, []string{`{"start":0,"end":1,"error":"m"}`, `{"start":0,"end":0,"error":"m","expected":"x"}`}, []string{node(`"a"`), `null`}},
		{"missing is a missing value", types.TyMissing, []string{`{"start":0,"end":0,"error":"m","expected":"x"}`}, []string{`{"start":0,"end":1,"error":"m"}`, node(`"a"`)}},
		{"a rule type is the type of the rule", types.RuleTy{Name: "main"}, []string{node(`"a"`)}, []string{`"a"`}},
		{"a rule whose type names itself has no value", types.RuleTy{Name: "loop"}, nil, []string{node(`"a"`), `null`}},
		{"well formed: a named node within a value of _ has its type", types.ListTy{Elem: types.TyTop}, []string{`[` + named("Id", `"a"`) + `]`}, []string{`[` + named("Id", `["a"]`) + `]`, `[` + named("Nope", `"a"`) + `]`}},
		{"well formed: a struct within a value of _ has its type", types.TyTop, []string{`{"type":"S","start":0,"end":1,"fields":{"N":1}}`}, []string{`{"type":"S","start":0,"end":1,"fields":{"N":"1"}}`}},
		{"captures are not in the JSON form", caps, []string{node(`[` + node(`"a"`) + `]`)}, nil},
	} {
		for _, want := range []bool{true, false} {
			values := tc.yes
			if !want {
				values = tc.no
			}
			for _, s := range values {
				var v any
				if err := json.Unmarshal([]byte(s), &v); err != nil {
					t.Fatalf("%s: %s: %v", tc.name, s, err)
				}
				err := vt.checkAgainst(tc.t, fromJSON(v))
				if (err == nil) != want {
					t.Errorf("%s: %s : %s is %v, want %v (%v)", tc.name, s, tc.t, err == nil, want, err)
				}
			}
		}
	}
	// What only the model of a node tells apart: tuples from lists, a string node from a string box,
	// and the captures of a node.
	a := &tv{k: vNode, val: &tv{k: vString, s: "a"}}
	for _, tc := range []struct {
		name string
		t    types.Ty
		v    *tv
		want bool
	}{
		{"a tuple is no list", types.ListTy{Elem: types.TyInt}, &tv{k: vTuple, elems: []*tv{{k: vInt}}}, false},
		{"a list is no tuple", types.TupleTy{Elems: []types.Ty{types.TyInt}}, &tv{k: vList, elems: []*tv{{k: vInt}}}, false},
		{"a list is a list", types.ListTy{Elem: types.TyInt}, &tv{k: vList, elems: []*tv{{k: vInt}}}, true},
		{"a tuple node is no list node", types.NodeTy{Value: types.ListTy{Elem: str}}, &tv{k: vNode, val: &tv{k: vTuple, elems: []*tv{a}}}, false},
		{"a capture of its recorded type", caps, &tv{k: vNode, val: &tv{k: vTuple, elems: []*tv{a}}, capsKnown: true, caps: map[string]*tv{"x": a}}, true},
		{"a capture that is nil", caps, &tv{k: vNode, val: &tv{k: vTuple, elems: []*tv{a}}, capsKnown: true, caps: map[string]*tv{"x": nil}}, true},
		{"a capture of another type", caps, &tv{k: vNode, val: &tv{k: vTuple, elems: []*tv{a}}, capsKnown: true, caps: map[string]*tv{"x": {k: vInt}}}, false},
		{"an unreadable part fails the value", types.TyTop, &tv{k: vList, elems: []*tv{{bad: "x"}}, bad: "x"}, false},
		{"an exact JSON int beyond float64 is an int", types.TyInt, fromJSON(json.Number("9223372036854775807")), true},
		{"a JSON number with a fraction is no int", types.TyInt, fromJSON(json.Number("1.5")), false},
	} {
		if err := vt.checkAgainst(tc.t, tc.v); (err == nil) != tc.want {
			t.Errorf("%s: %s : %s is %v, want %v (%v)", tc.name, tc.v, tc.t, err == nil, tc.want, err)
		}
	}
	// A kind of type without a clause fails the check rather than being decided.
	if err := vt.checkAgainst(types.BasicTy("float"), &tv{k: vInt}); err == nil || !strings.Contains(err.Error(), "no clause") {
		t.Errorf("a type without a clause gives %v, want an error that names it", err)
	}
}
