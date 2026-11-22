package generate_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// generationRun is what one generation gives: the files of each target, what the debugging settings
// wrote (every dump and the statistics) and the events of its steps, each with the counters, the
// pattern decisions and the dump of its result.
type generationRun struct {
	files  [][]common.GeneratedFile
	output string
	events []string
}

// observedGeneration generates the Go and the TypeScript parser of g in one compilation, with every
// dump and the statistics written and an observer that records the events of the steps.
func observedGeneration(t *testing.T, name string, g *ast.Grammar) generationRun {
	t.Helper()
	var out bytes.Buffer
	d := common.Debug{PrintAfter: []string{"all"}, Stats: true, Output: &out}
	goT := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, Types: true, Recognize: true, Document: true, Observability: true, Debug: d}
	tsT := common.GenOptions{Lib: common.LibImport, LibModule: "./lib.ts", Types: true, Recognize: true, Debug: d}
	var events []string
	w := &pipetest.Watch{Print: true, Fn: func(e pipe.Event, r *pipe.Result) {
		var dump strings.Builder
		if r.Print != nil {
			r.Print(&dump)
		}
		patterns := map[string]string{}
		for k, v := range r.Patterns {
			patterns[k] = fmt.Sprint(v)
		}
		events = append(events, fmt.Sprintf("%d %s [%s] replayed=%t changed=%t counters=%v patterns=%v err=%v\n%s",
			e.Seq, e.Step.Instance, e.Label, e.Replayed, r.Changed, sortedPairs(r.Counters), sortedPairs(patterns), r.Err, dump.String()))
	}}
	files, err := generate.TargetsObserved(g, driver.Options{Observers: []pipe.Observer{w}},
		driver.Target{Backend: result.Go, Gen: goT}, driver.Target{Backend: result.TypeScript, Gen: tsT})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return generationRun{files, out.String(), events}
}

// sortedPairs returns the entries of m in key order, as text.
func sortedPairs[V any](m map[string]V) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(&b, "%s=%v ", k, m[k])
	}
	return b.String()
}

// A generation of several backends is deterministic: run twice, with GOMAXPROCS 1 and with every
// processor, it gives the same files, dumps, events (their numbers, labels, replays, counters,
// pattern decisions and dumps) and statistics. The default tier runs the small grammars of
// testdata/grammars; the long tier runs all of them.
func TestGenerationIsDeterministic(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	n := runtime.GOMAXPROCS(0)
	if n == 1 {
		t.Skip("one processor: GOMAXPROCS 1 and N are the same setting")
	}
	ran := 0
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !codegentest.Long() && len(src) > 20_000 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			g, err := compiletest.Parse(string(src))
			if err != nil {
				t.Fatal(err)
			}
			runtime.GOMAXPROCS(1)
			one := observedGeneration(t, name, g)
			runtime.GOMAXPROCS(n)
			many := observedGeneration(t, name, g)
			if !reflect.DeepEqual(one.files, many.files) {
				t.Errorf("the files differ between GOMAXPROCS 1 and %d", n)
			}
			if one.output != many.output {
				t.Errorf("the dumps and statistics differ between GOMAXPROCS 1 and %d", n)
			}
			if !slices.Equal(one.events, many.events) {
				t.Errorf("the events differ between GOMAXPROCS 1 and %d", n)
			}
			// The run covers both branches and a shared step that the second use replays.
			all := strings.Join(one.events, "")
			if !strings.Contains(one.output, "[go/") || !strings.Contains(one.output, "[ts/") || !strings.Contains(all, "replayed=true") {
				t.Errorf("the run observed too little: %d events", len(one.events))
			}
		})
		ran++
	}
	if ran < 3 {
		t.Fatalf("ran %d grammars", ran)
	}
}
