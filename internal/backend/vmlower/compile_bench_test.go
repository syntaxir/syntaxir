package vmlower_test

import (
	"os"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/vmlowertest"
)

// BenchmarkBytecodeLowering isolates the construction of the module, its tables, Flow bodies and action
// code, from the planned Node table of an already checked grammar; source parsing, checking, the
// table's build and VM setup are out. The lowering does not change its input, so each iteration
// lowers the same table.
func BenchmarkBytecodeLowering(b *testing.B) {
	for _, c := range []struct{ name, path string }{
		{"JSON", "../../../testdata/grammars/json/json.sxir"},
		{"Minilang", "../../../examples/minilang/minilang.sxir"},
	} {
		b.Run(c.name, func(b *testing.B) {
			source, err := os.ReadFile(c.path)
			if err != nil {
				b.Fatal(err)
			}
			in := vmInput(b, compiletest.Program(b, string(source), check.Options{}))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, _, err := vmlowertest.Lower(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
