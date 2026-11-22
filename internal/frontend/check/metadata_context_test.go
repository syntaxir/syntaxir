package check

import (
	"context"
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestCompileMetadataCollectionCancellation(t *testing.T) {
	g := &ast.Grammar{}
	a := &ast.Attribute{Name: "error"}
	for range 1024 {
		g.File.Decls = append(g.File.Decls, &ast.TypeDef{Name: "T"}, &ast.RuleDef{Name: "a"})
		a.Args = append(a.Args, "label")
	}
	expr := &ast.Attributed{Expr: &ast.Literal{Value: "a"}, Attrs: []*ast.Attribute{a}}
	for _, phase := range []struct {
		name string
		run  func(*cancel.Control)
	}{
		{"rules", func(c *cancel.Control) { c.Rules(g) }},
		{"types", func(c *cancel.Control) { c.Types(g) }},
		{"call-walk", func(c *cancel.Control) { astfacts.AllCallsControlled(expr, nil, c) }},
		{"type-check-arguments", func(c *cancel.Control) {
			(&ValueInference{control: c}).predicates(expr, &valueEnv{})
		}},
		{"stream-walk", func(c *cancel.Control) { (&Compiler{Control: c}).checkStream(expr) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &cancel.Control{Ctx: compileTestContext(t, 2)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(cancel.Cancellation)
				if !ok || !errors.Is(stopped.Err, context.Canceled) {
					t.Fatalf("collection did not unwind as compilation cancellation: %v", failure)
				}
			}()
			phase.run(control)
		})
	}
}

func TestProjectionMetadataCancellation(t *testing.T) {
	var expr ast.Expr = &ast.Literal{Value: "a"}
	var actionExpr ast.ActionExpr = &ast.StringLit{Value: "a"}
	for range 1024 {
		expr = &ast.Optional{Expr: expr}
		actionExpr = &ast.FieldAccess{Name: "field", X: actionExpr}
	}
	for _, phase := range []struct {
		name string
		run  func(*cancel.Control)
	}{
		{"candidate-walk", func(c *cancel.Control) {
			ruleProjectionsControlled(NewModel(nil, nil), &Rule{Def: &ast.RuleDef{Expr: expr}, Action: &ast.StringLit{}}, c)
		}},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &cancel.Control{Ctx: compileTestContext(t, 1)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(cancel.Cancellation)
				if !ok || !errors.Is(stopped.Err, context.Canceled) {
					t.Fatalf("projection metadata did not stop: %v", failure)
				}
			}()
			phase.run(control)
		})
	}
}

func TestTypeCheckEmptyAttributeCancellation(t *testing.T) {
	attrs := make([]*ast.Attribute, 1024)
	for i := range attrs {
		attrs[i] = &ast.Attribute{Name: "error"}
	}
	control := &cancel.Control{Ctx: compileTestContext(t, 1)}
	defer func() {
		failure := recover()
		stopped, ok := failure.(cancel.Cancellation)
		if !ok || !errors.Is(stopped.Err, context.Canceled) {
			t.Fatalf("empty attribute rows did not stop: %v", failure)
		}
	}()
	(&ValueInference{control: control}).predicates(&ast.Attributed{Expr: &ast.Literal{Value: "a"}, Attrs: attrs}, &valueEnv{})
}
