package check

import (
	"os"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func BenchmarkCharacterRangeValidation(b *testing.B) {
	for _, tc := range []struct{ name, path string }{{"JSON", "../../../testdata/grammars/json/json.sxir"}, {"Go", "../../../testdata/grammars/golang/golang.sxir"}, {"YAML", "../../../testdata/grammars/yaml/yaml.sxir"}} {
		b.Run(tc.name, func(b *testing.B) {
			path := tc.path
			if tc.name == "YAML" && os.Getenv("SXIR_BENCH_YAML_GRAMMAR") != "" {
				path = os.Getenv("SXIR_BENCH_YAML_GRAMMAR")
			}
			source, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			g, err := parseTest(string(source))
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Validate", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := ast.Validate(g); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Compile", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := compileStatic(g, Options{}, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
