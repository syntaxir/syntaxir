package vmlower_test

import (
	"os"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// BenchmarkBytecodeLowering isolates the construction of the module, match code and action code, from
// the Core IR of an already checked grammar after the bytecode pipeline; source parsing, checking,
// the Core IR build and VM setup are out.
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
			p := compiletest.Program(b, string(source), compile.Options{})
			m, err := elaborate.CoreControlled(p, nil)
			if err != nil {
				b.Fatal(err)
			}
			core.Analyze(m)
			if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := vmlower.CompileCore(vmlower.Input{Run: p.Runtime(), Elab: p}, m); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
