package bench

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Short inputs: the latency of a parse of at most 1 KB, which a parser that serves requests, reads
// configuration or checks a field pays per call, and in which the fixed cost of a parse (options,
// pooled state, the first rule calls) is a large share of the time.

// shortSizes are the input sizes of the short-input benchmarks.
var shortSizes = []struct {
	name string
	size int
}{{"64B", 64}, {"256B", 256}, {"1KB", 1024}}

// coldSize is the input size of the cold first parse.
const coldSize = 1024

// shortInput returns the largest input that w's generator makes at most size bytes long, at a
// smaller scale than its benchmark input, or "" if that input is shorter than a quarter of size: the
// generator's units (a JSON object, an XML book, a Minilang function) are too coarse for that size,
// and the workload has no row there.
func shortInput(w *workload, size int) string {
	// The input shrinks as the scale grows: find the smallest scale whose input fits.
	lo, hi := 1, 1<<22
	if len(w.input(hi)) > size {
		return ""
	}
	for lo < hi {
		mid := lo + (hi-lo)/2
		if len(w.input(mid)) <= size {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	in := w.input(lo)
	if len(in) < size/4 {
		return ""
	}
	return in
}

// shortEntry is an entry point that a short-input benchmark measures.
type shortEntry struct {
	name string
	run  func(string) error
}

// shortEntries returns the entry points of w's generated parser (Parse in code points, ParseValue,
// Recognize) and the standard library parser it compares with, if any.
func shortEntries(w *workload) []shortEntry {
	es := []shortEntry{{"Parse", func(s string) error { return w.gen(s, false) }}}
	if w.ast != nil {
		es = append(es, shortEntry{"ParseValue", w.ast})
	}
	if w.rec != nil {
		es = append(es, shortEntry{"Recognize", w.rec})
	}
	if w.std != nil {
		es = append(es, shortEntry{w.stdName, w.std})
	}
	return es
}

// shortWorkloads returns the workloads that parse without errors: the recovery workload measures
// invalid input, which has its own benchmarks.
func shortWorkloads() []*workload {
	var ws []*workload
	for i := range workloads {
		if !workloads[i].partial {
			ws = append(ws, &workloads[i])
		}
	}
	return ws
}

// TestShortInputs checks the inputs of the short-input benchmarks: every workload has the 1 KB input,
// each input has between a quarter of its size and its size, and every entry point accepts it.
func TestShortInputs(t *testing.T) {
	for _, w := range shortWorkloads() {
		for _, s := range shortSizes {
			in := shortInput(w, s.size)
			if in == "" {
				if s.size == coldSize {
					t.Errorf("%s has no %s input", w.name, s.name)
				}
				continue
			}
			if len(in) > s.size || len(in) < s.size/4 {
				t.Errorf("%s %s: %d bytes", w.name, s.name, len(in))
			}
			for _, e := range shortEntries(w) {
				if err := e.run(in); err != nil {
					t.Errorf("%s %s %s: %v", w.name, s.name, e.name, err)
				}
			}
		}
	}
}

// BenchmarkShort parses the short inputs of every workload with the generated parser's entry points
// and the standard library parser, steady state: the parser's pooled state is warm.
func BenchmarkShort(b *testing.B) {
	for _, w := range shortWorkloads() {
		for _, s := range shortSizes {
			in := shortInput(w, s.size)
			if in == "" {
				continue
			}
			for _, e := range shortEntries(w) {
				b.Run(w.name+"/"+s.name+"/"+e.name, func(b *testing.B) {
					b.SetBytes(int64(len(in)))
					b.ReportAllocs()
					for b.Loop() {
						if err := e.run(in); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

// coldEnv names the case that a process started by BenchmarkCold runs (WORKLOAD/ENTRY).
const coldEnv = "SXIR_BENCH_COLD"

// BenchmarkCold measures the first parse in a fresh process: each iteration starts the test binary
// again, which parses the 1 KB input of one workload once with one entry point and reports the time,
// bytes and allocations of that parse. The time is the first call's, with the work a process does on
// first use (empty pools, lazily built tables, the standard library's caches); it excludes the start
// of the runtime and the initialization of the packages, which a process pays whether it parses or
// not. ns/op, B/op and allocs/op are the means over the processes.
func BenchmarkCold(b *testing.B) {
	bin, err := os.Executable()
	if err != nil {
		b.Fatal(err)
	}
	for _, w := range shortWorkloads() {
		for _, e := range shortEntries(w) {
			name := w.name + "/" + e.name
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs() // replaced by the child's, as are the times
				var ns, mem, allocs float64
				for i := 0; i < b.N; i++ {
					r, err := runColdProcess(bin, name)
					if err != nil {
						b.Fatal(err)
					}
					ns += r[0]
					mem += r[1]
					allocs += r[2]
				}
				n := float64(b.N)
				b.ReportMetric(ns/n, "ns/op")
				b.ReportMetric(mem/n, "B/op")
				b.ReportMetric(allocs/n, "allocs/op")
			})
		}
	}
}

// runColdProcess starts the test binary for one cold case and returns the time in nanoseconds, the
// bytes and the allocations of its parse.
func runColdProcess(bin, name string) ([3]float64, error) {
	var r [3]float64
	cmd := exec.Command(bin, "-test.run", "^$")
	cmd.Env = append(os.Environ(), coldEnv+"="+name)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return r, fmt.Errorf("cold %s: %w\n%s", name, err, errOut.String())
	}
	if _, err := fmt.Sscanf(out.String(), "cold %g %g %g", &r[0], &r[1], &r[2]); err != nil {
		return r, fmt.Errorf("cold %s: output %q: %w", name, out.String(), err)
	}
	return r, nil
}

// runCold runs the cold case named by coldEnv in this process, if one is named, and reports whether
// it did. The input is built first; then the first parse of the process is measured.
func runCold() bool {
	name := os.Getenv(coldEnv)
	if name == "" {
		return false
	}
	wname, ename, _ := strings.Cut(name, "/")
	for _, w := range shortWorkloads() {
		if w.name != wname {
			continue
		}
		for _, e := range shortEntries(w) {
			if e.name != ename {
				continue
			}
			in := shortInput(w, coldSize)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			err := e.run(in)
			d := time.Since(start)
			runtime.ReadMemStats(&after)
			if err != nil {
				fmt.Fprintf(os.Stderr, "cold %s: %v\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("cold %d %d %d\n", d.Nanoseconds(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
			return true
		}
	}
	fmt.Fprintf(os.Stderr, "cold %s: no such case\n", name)
	os.Exit(1)
	return true
}

// TestColdProcess runs one cold case in a fresh process, as BenchmarkCold does, and checks what it
// reports.
func TestColdProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the test binary")
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := runColdProcess(bin, "JSON/Parse")
	if err != nil {
		t.Fatal(err)
	}
	if r[0] <= 0 || r[1] <= 0 || r[2] <= 0 {
		t.Errorf("first parse: %v ns, %v B, %v allocs", r[0], r[1], r[2])
	}
	if _, err := runColdProcess(bin, "JSON/NoSuchEntry"); err == nil {
		t.Error("an unknown case ran")
	}
}
