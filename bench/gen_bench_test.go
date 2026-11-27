package bench

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// corpusTargets returns the Go and the TypeScript target of each grammar of testdata/grammars, by
// its name, with the options of testdata/grammars/generate.txt, those of the ready-made parsers:
// the TypeScript target takes the flags that it supports.
func corpusTargets(tb testing.TB) map[string][2]driver.Target {
	f, err := os.Open("../testdata/grammars/generate.txt")
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	out := map[string][2]driver.Target{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		goO := common.GenOptions{Module: "example.com/" + fields[0], Lib: common.LibImport}
		tsO := common.GenOptions{Lib: common.LibImport, LibModule: "./lib.ts"}
		for i := 1; i < len(fields); i++ {
			switch fields[i] {
			case "-types":
				goO.Types, tsO.Types = true, true
			case "-typed-only":
				goO.TypedOnly = true
			case "-no-limits":
				goO.NoLimitControls, tsO.NoLimitControls = true, true
			case "-recognize":
				goO.Recognize, tsO.Recognize = true, true
			case "-expectations":
				goO.Expectations, tsO.Expectations = true, true
			case "-nodoc":
				goO.OmitPackageDoc = true
			case "-lib":
				if i++; i < len(fields) && fields[i] != "import" {
					tb.Fatalf("generate.txt: %s: -lib %s", fields[0], fields[i])
				}
			default:
				tb.Fatalf("generate.txt: %s: the flag %s has no option here", fields[0], fields[i])
			}
		}
		out[fields[0]] = [2]driver.Target{{Backend: result.Go, Gen: goO}, {Backend: result.TypeScript, Gen: tsO}}
	}
	if err := s.Err(); err != nil {
		tb.Fatal(err)
	}
	return out
}

// BenchmarkGeneration measures the generation of the parsers of testdata/grammars from the parsed
// grammar, with the options of their ready-made parsers: the Go parser alone, the TypeScript parser
// alone, and both from one compilation ("go+ts"), which checks the grammar once and shares the
// steps whose inputs are equal. It is the generation-time gate of the pass pipeline
// (docs/performance.md). It lives in this package so that the timed
// measurements outside the repository can run it with the other benchmarks.
func BenchmarkGeneration(b *testing.B) {
	targets := corpusTargets(b)
	paths, err := filepath.Glob("../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		b.Fatalf("no grammars: %v", err)
	}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		ts, ok := targets[name]
		if !ok {
			b.Fatalf("generate.txt has no options for %s", name)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		g, err := compiletest.Parse(string(src))
		if err != nil {
			b.Fatal(err)
		}
		for _, c := range []struct {
			name    string
			targets []driver.Target
		}{
			{"go", ts[:1]},
			{"ts", ts[1:]},
			{"go+ts", ts[:]},
		} {
			b.Run(name+"/"+c.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := generate.Targets(g, append([]driver.Target(nil), c.targets...)...); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkRuntimeBackendCompile measures what a program compiled at run time costs its backends
// on first use, for each grammar of testdata/grammars: the bytecode module (Source.Module, which
// runs the VM pipeline of the driver) and the Core module of the closures (Source.Core, which runs the
// closure pipeline). Parsing and checking the grammar are out.
func BenchmarkRuntimeBackendCompile(b *testing.B) {
	files, err := filepath.Glob("../testdata/grammars/*/*.sxir")
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
				if _, err := checked.Of(p, pass.Settings{}).Module(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(name+"/closure", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := checked.Of(p, pass.Settings{}).Core(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGenerationEmbeddedLibrary measures the generation of the Go parsers of testdata/grammars
// with a copy of the runtime library inside the package (the library embed form), which prunes the
// library to what each parser can run. It is the generation-time gate of that pruning.
func BenchmarkGenerationEmbeddedLibrary(b *testing.B) {
	targets := corpusTargets(b)
	paths, err := filepath.Glob("../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		b.Fatalf("no grammars: %v", err)
	}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		ts, ok := targets[name]
		if !ok {
			b.Fatalf("generate.txt has no options for %s", name)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		g, err := compiletest.Parse(string(src))
		if err != nil {
			b.Fatal(err)
		}
		target := ts[0]
		target.Gen.Lib = common.LibEmbed
		target.Gen = codegentest.WithLib(target.Gen)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := generate.Targets(g, target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
