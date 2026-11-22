package examinedref

import (
	"reflect"
	"strings"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// GrammarAlphabet returns the characters that the grammar names: those of its literals and the bounds of
// its character class ranges. Perturbations substitute and append them, so that a changed slot is likely
// to change what a terminal at that slot does.
func GrammarAlphabet(g *ast.Grammar) string {
	var b strings.Builder
	walkAST(g, func(v any) {
		switch x := v.(type) {
		case *ast.Literal:
			b.WriteString(x.Value)
		case *ast.CharClass:
			for _, r := range x.Ranges {
				b.WriteRune(r.Lo)
				b.WriteRune(r.Hi)
			}
		}
	})
	return b.String()
}

// ReadsVariables reports whether the grammar reads a parser variable that it declares (var): the value of
// such a variable can come from input that a call's examined range does not include (CheckLockstep).
func ReadsVariables(g *ast.Grammar) bool {
	declared := map[string]bool{}
	for _, v := range g.Vars() {
		declared[v.Name] = true
	}
	reads := false
	walkAST(g, func(v any) {
		if r, ok := v.(*ast.VarRef); ok && declared[r.Name] {
			reads = true
		}
	})
	return reads
}

// walkAST calls f with every pointer to a struct reachable from g, once each.
func walkAST(g *ast.Grammar, f func(any)) {
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			if v.Elem().Kind() == reflect.Struct && v.CanInterface() {
				f(v.Interface())
			}
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for it := v.MapRange(); it.Next(); {
				walk(it.Value())
			}
		}
	}
	walk(reflect.ValueOf(g))
}
