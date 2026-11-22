package checked

import (
	"os"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// BenchmarkRuntimeBackendCompile measures what a program compiled at run time costs its backends
// on first use, for each grammar of testdata/grammars: the bytecode module (Source.VMInput, which
// plans and lowers the Node table, and vmlower.Compile) and the Core IR of the closures
// (closureCore). Parsing and checking the grammar are out.
func BenchmarkRuntimeBackendCompile(b *testing.B) {
	files, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(files) == 0 {
		b.Fatalf("no bundled grammars: %v", err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}
		p := compiletest.Program(b, string(src), check.Options{})
		name := filepath.Base(filepath.Dir(f))
		b.Run(name+"/bytecode", func(b *testing.B) {
			if p.HasMissing {
				b.Skip("no bytecode form")
			}
			b.ReportAllocs()
			for b.Loop() {
				in, err := Of(p, pass.Settings{}).VMInput()
				if err != nil {
					b.Fatal(err)
				}
				if _, _, err := vmlower.Compile(in); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(name+"/closure", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := closureCore(p, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
