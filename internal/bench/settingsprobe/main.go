//go:build sxir_settingsprobe

// Command settingsprobe measures a generated Go parser for the settings part of bench/metrics
// (docs/performance.md, "Deterministic Metrics"). It is built only by that part, with the build tag
// sxir_settingsprobe and an overlay (go build -overlay) that supplies the parser as the package gen of
// this directory and the file apis.go, which declares
//
//	var apis = map[string]func(string) error{...} // the APIs of the parser, by name
//	func parseValue(string) (any, error)          // ParseValue of the parser
//
// so that one program measures every parser and the size of its binary differs from another's only
// by the parser.
//
//	settingsprobe -what allocs INPUT...    # with GOMAXPROCS=1
//	settingsprobe -what retained INPUT...  # with GOGC=off and GOMAXPROCS=1
//
// With -what allocs it writes, for each API, the objects and bytes that a call allocates on each input
// (LeastAllocs of package linearity), summed over the inputs, and the number of inputs on which the
// call returns an error. With -what retained it writes the live heap that the values of ParseValue of
// the inputs keep after a collection, summed over the inputs. The output is one JSON object.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"slices"

	"syntaxir.dev/sxir/internal/testing/linearity"
)

// The warm runs and the measurements of a call, as TestMetrics of package bench takes them.
const (
	warmRuns    = 20
	allocRounds = 5
)

type apiResult struct {
	Objects uint64 `json:"objects"`
	Bytes   uint64 `json:"bytes"`
	Errors  int    `json:"errors"`
}

type result struct {
	APIs            map[string]apiResult `json:"apis,omitempty"`
	RetainedBytes   int64                `json:"retained_bytes"`
	RetainedObjects int64                `json:"retained_objects"`
}

func run() error {
	what := flag.String("what", "allocs", "allocs or retained")
	flag.Parse()
	var inputs []string
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		inputs = append(inputs, string(data))
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no inputs")
	}
	var r result
	switch *what {
	case "allocs":
		r.APIs = map[string]apiResult{}
		names := make([]string, 0, len(apis))
		for name := range apis {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			call := apis[name]
			var a apiResult
			for _, in := range inputs {
				if call(in) != nil {
					a.Errors++
				}
				o, b := linearity.LeastAllocs(warmRuns, allocRounds, func() { _ = call(in) })
				a.Objects += o
				a.Bytes += b
			}
			r.APIs[name] = a
		}
	case "retained":
		for _, in := range inputs {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			v, _ := parseValue(in)
			runtime.GC()
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(v)
			r.RetainedBytes += int64(after.HeapAlloc) - int64(before.HeapAlloc)
			r.RetainedObjects += int64(after.HeapObjects) - int64(before.HeapObjects)
		}
	default:
		return fmt.Errorf("-what: want allocs or retained, not %q", *what)
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "settingsprobe:", err)
		os.Exit(1)
	}
}
