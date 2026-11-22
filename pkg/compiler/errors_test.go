package compiler_test

import (
	"errors"
	"fmt"
	"testing"

	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// entryPoints call every function of the package that compiles a grammar source.
var entryPoints = map[string]func(grammar.Source) error{
	"Compile": func(s grammar.Source) error { _, err := compiler.Compile(s); return err },
	"NewReferenceParser": func(s grammar.Source) error {
		_, err := compiler.NewReferenceParser(s)
		return err
	},
	"Marshal":      func(s grammar.Source) error { _, err := compiler.Marshal(s, 0); return err },
	"OutputSchema": func(s grammar.Source) error { _, err := compiler.OutputSchema(s); return err },
	"GenerateGo": func(s grammar.Source) error {
		_, err := compiler.GenerateGo(s, compiler.GoConfig{Module: "example.com/p"})
		return err
	},
	"GenerateTypeScript": func(s grammar.Source) error {
		_, err := compiler.GenerateTypeScript(s, compiler.TSConfig{})
		return err
	},
}

func TestCheckErrors(t *testing.T) {
	src := grammar.FromString("g.sxir", "@start\nrule main = undefined\nrule other = \"a\" { x }\n")
	want := "2:13: undefined rule undefined\n3:20: undefined variable x; declare it with var x at the top level"
	for name, call := range entryPoints {
		err := call(src)
		errs, ok := err.(grammar.CheckErrors)
		if !ok || err.Error() != want {
			t.Errorf("%s: %T %q, want grammar.CheckErrors %q", name, err, err, want)
			continue
		}
		var viaAs grammar.CheckErrors
		var one *grammar.CheckError
		if !errors.As(err, &viaAs) || len(viaAs) != 2 || errors.As(err, &one) {
			t.Errorf("%s: errors.As gives the list and not one error", name)
		}
		if errs[0].Pos != (ast.Pos{Line: 2, Col: 13}) || errs[0].Msg != "undefined rule undefined" {
			t.Errorf("%s: first error %+v", name, errs[0])
		}
	}
}

// TestCheckErrorsOfOtherErrors checks that the errors of loading, of the options and of the start
// rule are not check errors that the source holds.
func TestCheckErrorsOfOtherErrors(t *testing.T) {
	var errs grammar.CheckErrors
	_, err := compiler.Compile(grammar.FromString("g.sxir", "rule main = (\n"))
	var syntax grammar.SyntaxErrors
	if !errors.As(err, &syntax) || errors.As(err, &errs) {
		t.Errorf("syntax error: %T %v", err, err)
	}
	ok := grammar.FromString("g.sxir", "@start\nrule main = \"a\"\n")
	if _, err := compiler.Compile(ok, compiler.WithOptimize(compiler.Optimize{Profile: "fastest"})); err == nil || errors.As(err, &errs) {
		t.Errorf("unknown profile: %T %v", err, err)
	}
}

// TestCheckErrorWithoutPosition checks the check error of a grammar AST without positions.
func TestCheckErrorWithoutPosition(t *testing.T) {
	g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{
		&ast.RuleDef{Name: "main", Attrs: []*ast.Attribute{{Name: "start"}}, Expr: &ast.Ref{Name: "undefined"}},
	}}}
	_, err := compiler.CompileGrammar(g)
	errs, ok := err.(grammar.CheckErrors)
	if !ok || len(errs) != 1 || errs[0].Pos.IsValid() || errs.Error() != errs[0].Msg {
		t.Fatalf("%T %q, want one check error without a position", err, err)
	}
}

func ExampleCompile_checkErrors() {
	_, err := compiler.Compile(grammar.FromString("g.sxir", "@start\nrule main = item+\n"))
	var errs grammar.CheckErrors
	if errors.As(err, &errs) {
		for _, e := range errs {
			fmt.Printf("g.sxir:%d:%d: %s\n", e.Pos.Line, e.Pos.Col, e.Msg)
		}
	}
	// Output:
	// g.sxir:2:13: undefined rule item
}
