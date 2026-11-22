package check_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/driver/entry"
	fcheck "syntaxir.dev/sxir/internal/frontend/check"
	corebuild "syntaxir.dev/sxir/internal/frontend/elaborate"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Cancel at a deterministic poll, without timing or global compiler hooks.
type compilePollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *compilePollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func compileTestContext(t *testing.T, polls int) *compilePollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &compilePollContext{Context: ctx, cancel: cancel, left: polls}
}

func compileTestGrammar(t *testing.T) *ast.Grammar {
	t.Helper()
	g, err := enginetest.Parse(codegentest.InferredChain(1024, false, ""))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCompileContextCancellation(t *testing.T) {
	t.Parallel()
	g := compileTestGrammar(t)
	before, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, polls := range []int{1, 3, 12} {
		ctx := compileTestContext(t, polls)
		prog, err := entry.CompileContext(ctx, g, entry.Options{})
		if prog != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("polls=%d: program=%v error=%v", polls, prog, err)
		}
	}
	after, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled compilation mutated source grammar")
	}
}

func TestCompileContextPhaseCancellation(t *testing.T) {
	t.Parallel()
	g := compileTestGrammar(t)
	p, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"static-analysis", "success-proof", "failure-proof", "cut-proof", "site-numbering", "type-checking", "type-inference", "list-constructor-type", "core-build", "core-analysis", "core-passes", "core-pass-guards", "core-pass-scans", "core-verify"} {
		t.Run(phase, func(t *testing.T) {
			ctx := compileTestContext(t, 1)
			control := &cancel.Control{Ctx: ctx}
			defer func() {
				failure := recover()
				cancelled, ok := failure.(cancel.Cancellation)
				if !ok || !errors.Is(cancelled.Err, context.Canceled) {
					t.Fatalf("unexpected unwind: %v", failure)
				}
			}()
			switch phase {
			case "static-analysis":
				corepass.Compute(structuralModule(t, g), control)
			case "success-proof", "failure-proof", "cut-proof":
				items := make([]ir.Node, 1024)
				alts := make([]*ir.Alt, len(items))
				for i := range items {
					items[i] = &ir.Succeed{}
					if phase == "failure-proof" {
						items[i] = &ir.FailNode{}
					}
					alts[i] = &ir.Alt{Body: items[i]}
				}
				var node ir.Node = &ir.Seq{Items: items}
				if phase == "failure-proof" {
					node = &ir.Look{Neg: true, Body: &ir.Choice{Alts: alts}}
				} else if phase == "cut-proof" {
					node = &ir.Opt{Body: node}
				}
				// Only the shared proof traversal can reach the first context
				// checkpoint; no outer analysis loop contributes polling here.
				corepass.Compute(&ir.Module{}, nil).WithControl(control).Succeeds(node)
			case "site-numbering":
				corepass.ComputeSites(structuralModule(t, g), make([]corepass.SiteRule, len(g.Rules())), control)
			case "type-checking":
				var diagnostics diagnostic.ErrorList
				fcheck.CheckTypesControlled(entry.Checked(p).Checker(), &diagnostics, control)
			case "type-inference":
				fcheck.ValueTypes(entry.Checked(p).Checker(), control)
			case "list-constructor-type":
				// The entry poll alone cannot cancel: cancellation must be
				// observed inside the temporary checker resolving Elem.
				var elem ast.TypeExpr = &ast.TypeRef{Name: "Match"}
				for range 1024 {
					elem = &ast.ListType{Elem: elem}
				}
				c := &fcheck.Compiler{Model: entry.Checked(p).Model, Control: control}
				c.CheckActionExpr(&ast.ListConstructor{Elem: elem}, fcheck.NewScope(), nil)
			case "core-build":
				_, _ = corebuild.CoreControlled(entry.Checked(p), control)
			case "core-analysis", "core-passes", "core-pass-guards", "core-pass-scans", "core-verify":
				m, err := corebuild.CoreFrom(entry.Checked(p), false)
				if err != nil {
					t.Fatal(err)
				}
				// The passes and Verify take a module with its attributes computed, as elaborate
				// makes it, and reach the checkpoint through their own polls.
				if phase != "core-analysis" {
					ir.Analyze(m)
				}
				m.Control = control
				switch phase {
				case "core-analysis":
					ir.Analyze(m)
				case "core-passes":
					_, _, _ = corepass.Run(m, corepass.RunOptions{})
				case "core-pass-guards":
					_, _ = pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Of(corepass.GuardsPass), m)
				case "core-pass-scans":
					_, _ = pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Of(corepass.ScansPass), m)
				case "core-verify":
					_ = ir.Verify(m)
				}
			}
			t.Fatal("phase completed without cancellation")
		})
	}
}

func TestCompileContextOrdinaryParity(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`type P = {}; rule main = "a" => P{}`,
		`rule main = #operators { operand [0-9]; level sum { infix left "+" } }`,
		`rule main = "" / "a"`,
		`rule main = ("a" #cut "b") / ""`,
		`rule main = "a" / missing`,
		`type P = { X node[string] }; rule main = "a" => P{X: 1}`,
	} {
		g, err := enginetest.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		ordinary, ordinaryErr := entry.Compile(g, entry.Options{})
		ctx, cancel := context.WithCancel(context.Background())
		controlled, controlledErr := entry.CompileContext(ctx, g, entry.Options{})
		cancel()
		if (ordinaryErr == nil) != (controlledErr == nil) {
			t.Fatalf("error mismatch: %v / %v", ordinaryErr, controlledErr)
		}
		if ordinaryErr != nil {
			if ordinaryErr.Error() != controlledErr.Error() {
				t.Fatalf("diagnostic mismatch: %v / %v", ordinaryErr, controlledErr)
			}
			continue
		}
		for _, rd := range g.Rules() {
			x, ok := ordinary.RuleType(rd.Name)
			y, controlledOK := controlled.RuleType(rd.Name)
			if x != y || ok != controlledOK {
				t.Fatalf("type mismatch: %s / %s", x, y)
			}
		}
		// Cancelling the compile context after success must not cancel execution.
		x, ex := ordinary.Parse("main", "a")
		y, ey := controlled.Parse("main", "a")
		if (ex == nil) != (ey == nil) {
			t.Fatalf("parse error mismatch: %v / %v", ex, ey)
		}
		xj, _ := json.Marshal(x)
		yj, _ := json.Marshal(y)
		if !reflect.DeepEqual(xj, yj) {
			t.Fatal("parse value mismatch")
		}
	}
}

// structuralModule returns the untyped Core IR module of the rules of g, from which the front end
// takes the rule facts and the site numbers.
func structuralModule(t *testing.T, g *ast.Grammar) *ir.Module {
	t.Helper()
	m, err := corebuild.Structural(corebuild.StructuralInput(g, g.Rules(), nil), false)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
