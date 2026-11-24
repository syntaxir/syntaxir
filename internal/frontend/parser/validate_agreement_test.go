package parser

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestValidateAgreesWithParserNames checks that structural validation rejects
// the names the parser rejects, so a valid AST never prints a name that does
// not parse back: the concrete syntax tree type names, a bare node, and the
// field names that read as a capture.
func TestValidateAgreesWithParserNames(t *testing.T) {
	typeGrammar := func(typ ast.TypeExpr) *ast.Grammar {
		return &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.TypeAlias{Name: "T", Type: typ}}}}
	}
	for name, replacement := range valueTypeNames {
		want := name + " is not a type; write " + replacement
		for _, typ := range []ast.TypeExpr{
			&ast.TypeRef{Name: name},
			&ast.TypeApp{Name: name, Args: []ast.TypeExpr{&ast.TypeRef{Name: "int"}}},
		} {
			if err := ast.Validate(typeGrammar(typ)); err == nil || !strings.HasSuffix(err.Error(), want) {
				t.Errorf("%T %s: got %v, want %q", typ, name, err, want)
			}
		}
		if _, err := Parse("type T ~ " + name + "\n"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parse %s: got %v, want %q", name, err, want)
		}
	}
	if err := ast.Validate(typeGrammar(&ast.TypeRef{Name: "node"})); err == nil || !strings.HasSuffix(err.Error(), bareNode) {
		t.Errorf("bare node: got %v, want %q", err, bareNode)
	}
	for _, name := range []string{"x", "value", "startPos", "endPos", "children", "expected", "message", "Value", "X", "$x", "$Value"} {
		action := &ast.FieldAccess{X: &ast.CaptureRef{Name: "n"}, Name: name}
		g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "p", Expr: &ast.Top{}, Action: action}}}}
		err := ast.Validate(g)
		if rejected := err != nil; rejected != captureMember(name) {
			t.Errorf("field %s: validation error %v, parser rejects %v", name, err, captureMember(name))
		}
	}
}
