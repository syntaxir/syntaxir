package fmt_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/analysis/complexity"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	sxirfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// shapeRun is the outcome of a parse: its result with the value shape, and the units reserved.
func shapeRun(t *testing.T, g *ast.Grammar, input string, back engine.Backend) (string, uint64) {
	t.Helper()
	prog, err := entry.Compile(starttest.MarkAll(g), entry.Options{Compile: check.Options{SourceInfo: true}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var used uint64
	o := engine.ParseOptions{Backend: back, Unit: sxirinput.Bytes, Expectations: true}
	o.Trace = func(e engine.TraceEvent) { used = shapeUnits(e) }
	n, err := prog.ParseWith("main", input, o)
	return codegentest.ResultJSON(n, err), used
}

// shapeCases are grammars whose cost view, or whose hand-built expression, holds a sequence of one
// item or a choice of one sequence, with inputs that succeed and inputs that fail.
var shapeCases = []struct {
	name   string
	src    string
	inputs []string
}{
	{"cut that keeps a tuple of one element",
		"rule main = x:(\"abc\" #cut / \"z\") {len($x) == 1} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"\n",
		[]string{"abcx", "zx", "abc", "zq", "", "x"}},
	{"a cut that no predicate reads",
		"rule main = x:(\"abc\" #cut / \"z\") {true} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"\n",
		[]string{"abcx", "zx", "abc", ""}},
}

// TestFormatKeepsShapeOfViewRoundTrip checks that the cost view of a grammar, printed as grammar
// text and read back, has the value shape and the cost of the view itself, on inputs that succeed
// and inputs that fail. The cost is the units of each backend; the specification's Cost
// (spec/runtime/parsing.md) has no term for `_` and `_|_`, so the units must be equal.
func TestFormatKeepsShapeOfViewRoundTrip(t *testing.T) {
	for _, c := range shapeCases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parser.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			cs, err := complexity.NewCostSource(g, nil)
			if err != nil {
				t.Fatal(err)
			}
			v := complexity.Build(&complexity.Source{Module: cs.Module, Facts: cs.Facts}, complexity.Options{}, nil)
			rules, err := v.Grammar(cs.Rules)
			if err != nil {
				t.Fatal(err)
			}
			byName := map[string]*ast.RuleDef{}
			for _, r := range rules {
				byName[r.Name] = r
			}
			view := *cs.Grammar
			view.File.Decls = nil
			for _, d := range cs.Grammar.File.Decls {
				if r, ok := d.(*ast.RuleDef); ok && byName[r.Name] != nil {
					d = byName[r.Name]
				}
				view.File.Decls = append(view.File.Decls, d)
			}
			text := sxirfmt.Format(&view)
			back, err := parser.Parse(text)
			if err != nil {
				t.Fatalf("the printed view does not parse: %v\n%s", err, text)
			}
			shapeRoundTrip(t, &view, back, c.inputs, text)
		})
	}
}

// shapeRoundTrip checks that the grammars want and got give the same result and the same cost on
// every input, with every backend.
func shapeRoundTrip(t *testing.T, want, got *ast.Grammar, inputs []string, text string) {
	t.Helper()
	for _, back := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, in := range inputs {
			wr, wu := shapeRun(t, want, in, back)
			gr, gu := shapeRun(t, got, in, back)
			if wr != gr || wu != gu {
				t.Errorf("%v on %q: original %s (%d units), reprinted %s (%d units)\n%s", back, in, wr, wu, gr, gu, text)
			}
		}
	}
}

// TestFormatKeepsShapeOfOneItemSequences checks the same of expressions that the syntax cannot
// write directly: a sequence of one item and a choice of one sequence, in a sequence, at the top
// of a rule and as the only alternative of a rule, on inputs that match and inputs that fail.
func TestFormatKeepsShapeOfOneItemSequences(t *testing.T) {
	lit := func(s string) ast.Expr { return &ast.Literal{Value: s} }
	one := func(e ast.Expr) ast.Expr { return &ast.Seq{Items: []ast.Expr{e}} }
	cs := func(es ...ast.Expr) ast.Expr { return &ast.Choice{Alts: []ast.Expr{&ast.Seq{Items: es}}} }
	for _, c := range []struct {
		name   string
		expr   ast.Expr
		inputs []string
	}{
		{"one-item sequence at the top", one(lit("a")), []string{"a", "b", ""}},
		{"one-item sequence in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), one(lit("b")), lit("c")}}, []string{"abc", "abd", "a"}},
		{"choice of one sequence in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), cs(lit("b"), lit("c")), lit("d")}}, []string{"abcd", "abxd", "abc", "a"}},
		{"choice of one sequence at the top", cs(lit("b"), lit("c")), []string{"bc", "bx", ""}},
		{"choice of one one-item sequence in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), cs(lit("b")), lit("c")}}, []string{"abc", "axc"}},
		{"choice of one sequence in a repetition", &ast.Seq{Items: []ast.Expr{&ast.Repeat{Expr: cs(lit("b"), lit("c")), Min: 0, Max: -1}, lit("d")}}, []string{"bcbcd", "bcbd", "d"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "main", Expr: c.expr}}}}
			text := sxirfmt.Format(g)
			back, err := parser.Parse(text)
			if err != nil {
				t.Fatalf("the printed grammar does not parse: %v\n%s", err, text)
			}
			shapeRoundTrip(t, g, back, c.inputs, text)
		})
	}
}
