package frontend_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/factstest"
)

// variants are the compiler options that change the Core IR of a grammar.
var variants = []struct {
	name string
	opts check.Options
}{
	{"parse", check.Options{}},
	{"recognize", check.Options{Recognition: check.RecognizeWithStreamValues}},
	{"no projections", check.Options{NoProjections: true}},
	{"default value", check.Options{DefaultValue: true}},
}

// TestRunMatchesCompile requires, for every corpus grammar and variant, that Run's module, once its
// facts are asked for, prints as the module built directly from the compiled program with its facts,
// that the facts cover the module's rules and that it has no fact error.
func TestRunMatchesCompile(t *testing.T) {
	t.Parallel()
	for _, path := range factstest.Corpus {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			g, err := compiletest.Parse(factstest.Read(t, path))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range variants {
				prog, err := compiletest.Compile(g, v.opts)
				if err != nil {
					continue // the grammar does not compile with this variant
				}
				want, err := elaborate.CoreFrom(prog, false)
				if err != nil {
					t.Fatal(err)
				}
				corepass.Compute(want, nil) // Run's module has its facts set
				res, err := frontend.Run(g, v.opts, checkfacts.Source{}, nil)
				if err != nil {
					t.Fatalf("%s: %v", v.name, err)
				}
				facts := corepass.FactsOf(res.Analyses, res.Module) // sets the module's facts, as Compute does
				if got, want := core.Print(res.Module, true), core.Print(want, true); got != want {
					t.Errorf("%s: module differs:\n%s\nwant\n%s", v.name, got, want)
				}
				if len(facts.Rules) != len(res.Module.Rules) {
					t.Errorf("%s: facts for %d rules, module has %d", v.name, len(facts.Rules), len(res.Module.Rules))
				}
				if errs := facts.Errors(res.Module, true); len(errs) != 0 {
					t.Errorf("%s: fact errors %v", v.name, errs)
				}
			}
		})
	}
}

// TestRunReportsCompileErrors requires Run to return the compiler's error list as is.
func TestRunReportsCompileErrors(t *testing.T) {
	t.Parallel()
	g, err := parser.Parse("type T = { A string }\n@start\nrule main = t $$\nrule t = \"a\" => T{A: 1}")
	if err != nil {
		t.Fatal(err)
	}
	_, want := compiletest.Compile(g, check.Options{})
	if want == nil {
		t.Fatal("the grammar compiles")
	}
	res, got := frontend.Run(g, check.Options{}, checkfacts.Source{}, nil)
	if res != nil || got == nil || got.Error() != want.Error() {
		t.Fatalf("Run = %v, %v; want the error %v", res, got, want)
	}
}
