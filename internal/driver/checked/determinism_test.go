package checked

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// runTimeRun is what one run-time build gives: the outputs of its targets as text, what the
// debugging settings wrote (every dump and the statistics) and the events of its steps, each with
// the counters, the pattern decisions and the dump of its result.
type runTimeRun struct {
	outputs []string
	output  string
	events  []string
}

// observedRunTimeBuild builds the program of s for the backends in one compilation, with every dump
// and the statistics written and an observer that records the events of the steps.
func observedRunTimeBuild(t *testing.T, s *Source, backends []result.Backend) runTimeRun {
	t.Helper()
	var out bytes.Buffer
	d := common.Debug{PrintAfter: []string{"all"}, Stats: true, Output: &out}
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
	targets := make([]driver.Target, len(backends))
	for i, b := range backends {
		targets[i] = driver.Target{Backend: b}
	}
	outs, err := driver.Compile(context.Background(), s.Program(), driver.Options{Settings: s.settings, Debug: d, Observers: []pipe.Observer{w}}, targets...)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, o := range outs {
		switch o := o.(type) {
		case *vmlower.Bytecode:
			texts = append(texts, o.Module.Disassemble())
		case *closure.Module:
			texts = append(texts, ir.Print(o.Core, true))
		default:
			t.Fatalf("output %T", o)
		}
	}
	return runTimeRun{texts, out.String(), events}
}

// sortedPairs returns the entries of m in key order, as text.
func sortedPairs[V any](m map[string]V) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(&b, "%s=%v ", k, m[k])
	}
	return b.String()
}

// A run-time build of the VM and the closures in one compilation is deterministic: run twice, with
// GOMAXPROCS 1 and with every processor, it gives the same bytecode and Core module, dumps, events
// (their numbers, labels, replays, counters, pattern decisions and dumps) and statistics. A grammar
// that inserts missing tokens builds for the closures alone, which the VM does not run. The default
// tier runs the small grammars of testdata/grammars; the long tier runs all of them.
func TestRunTimeBuildIsDeterministic(t *testing.T) {
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
			s, err := Check(g, Options{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			backends := []result.Backend{result.VM, result.Closure}
			if s.Runtime().HasMissing {
				backends = backends[1:]
			}
			runtime.GOMAXPROCS(1)
			one := observedRunTimeBuild(t, s, backends)
			runtime.GOMAXPROCS(n)
			many := observedRunTimeBuild(t, s, backends)
			if !slices.Equal(one.outputs, many.outputs) {
				t.Errorf("the outputs differ between GOMAXPROCS 1 and %d", n)
			}
			if one.output != many.output {
				t.Errorf("the dumps and statistics differ between GOMAXPROCS 1 and %d", n)
			}
			if !slices.Equal(one.events, many.events) {
				t.Errorf("the events differ between GOMAXPROCS 1 and %d", n)
			}
			// The run covers both branches and the Core part that the closures share with the VM.
			all := strings.Join(one.events, "")
			if len(backends) == 2 && (!strings.Contains(one.output, "[vm/") || !strings.Contains(one.output, "[closure/") || !strings.Contains(all, "replayed=true")) {
				t.Errorf("the run observed too little: %d events", len(one.events))
			}
		})
		ran++
	}
	if ran < 3 {
		t.Fatalf("ran %d grammars", ran)
	}
}
