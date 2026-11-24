package compiletest

import (
	"reflect"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Written returns a copy of g, a grammar in the compiler's normalized form, in the form the syntax
// writes, so that a test can compile a grammar that the compiler made, such as a view of the
// complexity classifier. Normalization holds #stream and #recover as pragma attributes, and
// #recover with its skip expression as an argument, which only the compiler writes and grammar
// validation rejects; Written turns them back into the Stream and Recovery nodes they stand for, in
// the order of the attributes. The other normalized forms are written as they are.
func Written(g *ast.Grammar) *ast.Grammar {
	g = g.Clone()
	written(reflect.ValueOf(g))
	return g
}

var exprType = reflect.TypeFor[ast.Expr]()

// written rewrites, in place, every expression under v that holds a normalized #stream or
// #recover.
func written(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			written(v.Elem())
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Type() == exprType && v.CanSet() {
			if e, ok := v.Interface().(*ast.Attributed); ok {
				v.Set(reflect.ValueOf(writtenAttributed(e)))
			}
		}
		written(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				written(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			written(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			x := v.MapIndex(k)
			if e, ok := x.Interface().(*ast.Attributed); ok {
				w := writtenAttributed(e)
				v.SetMapIndex(k, reflect.ValueOf(w))
				written(reflect.ValueOf(w))
				continue
			}
			written(x)
		}
	}
}

// writtenAttributed returns e with its normalized #stream and #recover attributes written as
// Stream and Recovery nodes, applied in order with the other attributes.
func writtenAttributed(e *ast.Attributed) ast.Expr {
	var out ast.Expr = e.Expr
	var pending []*ast.Attribute
	flush := func() {
		if len(pending) > 0 {
			out = &ast.Attributed{SourceInfo: e.SourceInfo, Expr: out, Attrs: pending}
			pending = nil
		}
	}
	for _, a := range e.Attrs {
		switch {
		case a.Pragma && a.Name == "stream":
			flush()
			out = &ast.Stream{SourceInfo: a.SourceInfo, Expr: out}
		case a.Pragma && a.Name == "recover":
			flush()
			skip, _ := a.Arg("skip")
			out = &ast.Recovery{SourceInfo: a.SourceInfo, Expr: out, Skip: skip.(ast.Expr)}
		default:
			pending = append(pending, a)
		}
	}
	if len(pending) == len(e.Attrs) {
		return e
	}
	flush()
	return out
}
