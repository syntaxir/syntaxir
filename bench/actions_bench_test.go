package bench

import (
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gactionsheavy "syntaxir.dev/sxir/internal/bench/parsers/actionsheavy"
	gactionspred "syntaxir.dev/sxir/internal/bench/parsers/actionspred"
	"syntaxir.dev/sxir/internal/runtime/engine"
)

// actionWorkloads measure the cost of evaluating action expressions. pred runs four predicate action expressions per line and builds
// one int; heavy builds a struct of five fields from maps, folds, filters and comparisons of the words of
// the line. They are separate from workloads because they have no counterpart in another implementation.
var actionWorkloads = []struct {
	name, grammar string
	gen           func(input string) error
	ast           func(input string) error
	rec           func(input string) error
}{
	{"ActionsPredicates", "grammars/actions/pred.sxir",
		func(s string) error { return genErr(gactionspred.Parse(s)) },
		func(s string) error { return genErr(gactionspred.ParseValue(s)) },
		func(s string) error { return gactionspred.Recognize(s) }},
	{"ActionsHeavy", "grammars/actions/heavy.sxir",
		func(s string) error { return genErr(gactionsheavy.Parse(s)) },
		func(s string) error { return genErr(gactionsheavy.ParseValue(s)) },
		func(s string) error { return gactionsheavy.Recognize(s) }},
}

// TestActionWorkloads checks that every action expression workload parses on every backend and in generated Go.
func TestActionWorkloads(t *testing.T) {
	for _, w := range actionWorkloads {
		t.Run(w.name, func(t *testing.T) {
			p := load(t, w.grammar)
			input := inputs.ActionsInput(50)
			for _, be := range backends {
				if _, err := p.Parse(input, engine.WithBackend(be.b)); err != nil {
					t.Fatalf("%s: %v", be.name, err)
				}
			}
			for name, f := range map[string]func(string) error{"generated": w.gen, "generated_ast": w.ast, "recognize": w.rec} {
				if err := f(input); err != nil {
					t.Errorf("%s: %v", name, err)
				}
			}
		})
	}
}

// BenchmarkActions parses the action workloads with every backend, the generated parsers and ParseValue.
func BenchmarkActions(b *testing.B) {
	for _, w := range actionWorkloads {
		b.Run(w.name, func(b *testing.B) {
			p := load(b, w.grammar)
			input := inputs.ActionsInput(2000)
			for _, be := range backends {
				b.Run(be.name, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						if _, err := p.Parse(input, engine.WithBackend(be.b)); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
			for name, f := range map[string]func(string) error{"generated": w.gen, "generated_ast": w.ast, "generated_recognize": w.rec} {
				b.Run(name, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						if err := f(input); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
