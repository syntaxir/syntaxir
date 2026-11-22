package engine

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/state"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

const streamStorageGrammar = `rule main = h:"#head\n" #atomic row* #stream $$ {text($h) == "#head\n"}
rule row = k:"x" #atomic vs:value+ "\n"
rule value = v:[a-z]+ #atomic ","`

// The tests of the stream prefix storage are in the state package; these benchmarks choose a
// backend's rule and descriptors, which only the engine can do.

// Measure live parser storage after a large first row followed by small rows.
// Input and compiled-program storage precede the baseline GC. The callback
// samples after earlier commits have crossed the pruning interval; it retains
// only the current small row. Forced GC makes this a retention measurement,
// not a throughput benchmark.
func BenchmarkStreamStorageRetainedHeap(b *testing.B) {
	for _, liveRoot := range []bool{false, true} {
		src := streamStorageGrammar
		if !liveRoot {
			src = strings.ReplaceAll(src, `h:"#head\n" #atomic`, `"#head\n"`)
			src = strings.ReplaceAll(src, ` {text($h) == "#head\n"}`, "")
		}
		g, err := parseTest(src)
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, checkpkg.Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			rule, err := prog.rule(backend, "main", state.Inlined, nil)
			if err != nil {
				b.Fatal(err)
			}
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				text := "#head\n" + "x" + strings.Repeat("abcdefghij,", 5000) + "\n" + strings.Repeat("xa,\n", 300)
				b.Run(fmt.Sprintf("root=%t/%s/%s", liveRoot, backend, unit), func(b *testing.B) {
					var total int64
					for b.Loop() {
						runtime.GC()
						var before, during runtime.MemStats
						runtime.ReadMemStats(&before)
						p := state.NewStreamParser(&prog.sharedProgram, strings.NewReader(text), unit)
						p.Descs = prog.descs(backend, state.Inlined)
						count := 0
						p.Emit = func(n *value.Node) error {
							count++
							if count == 200 {
								runtime.GC()
								runtime.ReadMemStats(&during)
								total += int64(during.HeapAlloc) - int64(before.HeapAlloc)
							}
							runtime.KeepAlive(n)
							return nil
						}
						if _, ok := p.Call(rule, 0); !ok || !p.AtEOF() || count != 301 {
							b.Fatalf("count=%d pos=%d: %v", count, p.Pos, p.SyntaxError())
						}
						runtime.KeepAlive(p)
						runtime.KeepAlive(text)
					}
					b.ReportMetric(float64(total)/float64(b.N), "retained-B")
				})
			}
		}
	}
}

// Compare a large-first stream with uniform small and uniformly wide rows.
// This reports cumulative allocation and throughput; it does not force GC.
func BenchmarkStreamRetentionWorkload(b *testing.B) {
	g, err := parseTest(streamStorageGrammar)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, checkpkg.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if _, err := prog.rule(backend, "main", state.Inlined, nil); err != nil {
			b.Fatal(err)
		}
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			for _, pattern := range []string{"small", "mixed", "wide"} {
				row := "x" + strings.Repeat("abcdefghij,", 5000) + "\n"
				body, count := strings.Repeat("xa,\n", 3000), 3000
				if pattern == "mixed" {
					body = row + body
					count++
				}
				if pattern == "wide" {
					body = strings.Repeat(row, 20)
					count = 20
				}
				text := "#head\n" + body
				b.Run(fmt.Sprintf("%s/%s/%s", pattern, backend, unit), func(b *testing.B) {
					b.SetBytes(int64(len(text)))
					b.ReportAllocs()
					for b.Loop() {
						got := 0
						err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { got++; return nil }, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || got != count {
							b.Fatalf("emitted %d of %d: %v", got, count, err)
						}
					}
				})
			}
		}
	}
}

// A large retained prefix must not turn normal element allocation into
// per-node allocation, and nil callbacks keep whole-repetition behavior.
func BenchmarkStreamPrefixSize(b *testing.B) {
	g, err := parseTest(strings.ReplaceAll(strings.ReplaceAll(streamStorageGrammar, `h:"#head\n" #atomic`, `h:row`), ` {text($h) == "#head\n"}`, ``))
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, checkpkg.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{32, 5000} {
		text := "x" + strings.Repeat("abcdefghij,", size) + "\n" + strings.Repeat("xa,\n", 3000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if _, err := prog.rule(backend, "main", state.Inlined, nil); err != nil {
				b.Fatal(err)
			}
			for _, noCallback := range []bool{false, true} {
				b.Run(fmt.Sprintf("%d/%s/nil=%t", size, backend, noCallback), func(b *testing.B) {
					b.SetBytes(int64(len(text)))
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						var emit func(*value.Node) error
						if !noCallback {
							emit = func(*value.Node) error { count++; return nil }
						}
						// This fixture retains the wide prefix rather than the simple header.
						// The input suffix predicate is disabled in this benchmark's grammar.
						err := prog.ParseStreamWith("main", strings.NewReader(text), emit, ParseOptions{Backend: backend})
						if err != nil || !noCallback && count != 3000 {
							b.Fatalf("count=%d: %v", count, err)
						}
					}
				})
			}
		}
	}
}
