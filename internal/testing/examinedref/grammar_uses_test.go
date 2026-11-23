package examinedref_test

import (
	"reflect"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// grammarUses is what decides whether a route refuses a grammar: #stream in the start rule, which a
// stream parse needs (spec/runtime/streaming.md).
type grammarUses struct {
	stream bool
}

// usesOf finds the attributes of g that decide refusals.
func usesOf(g *ast.Grammar) grammarUses {
	var u grammarUses
	for _, d := range g.Rules() {
		for _, name := range attributeNames(reflect.ValueOf(d.Expr)) {
			u.stream = u.stream || name == "stream" && d.Name == "main"
		}
	}
	return u
}

// attributeNames lists the names of the attributes in the expression tree v, with "stream" for a
// #stream repetition.
func attributeNames(v reflect.Value) []string {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		switch x := v.Interface().(type) {
		case *ast.Attribute:
			return []string{x.Name}
		case *ast.Stream:
			return append([]string{"stream"}, attributeNames(v.Elem())...)
		}
		return attributeNames(v.Elem())
	case reflect.Struct:
		var out []string
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				out = append(out, attributeNames(v.Field(i))...)
			}
		}
		return out
	case reflect.Slice:
		var out []string
		for i := range v.Len() {
			out = append(out, attributeNames(v.Index(i))...)
		}
		return out
	}
	return nil
}

// missingLimit is far above the insertions of the inputs, so that no parse reaches it.
const missingLimit = 1 << 20
