package engine

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime"
)

// TestEmittedStepsAdvance checks that every step the compiler emits finds the position past the one
// of its loop's last step: the bytecode machines parse the inputs of the generator corpus and the
// samples of the testdata grammars, as a parse, a recognition, a traced parse and a Document parse
// before and after an edit, under no compute limit, and none of them ends with the error of a step
// without progress. TestEmittedFlowsHaveBoundedCycles checks the other half of the guarantee, that
// every cycle of the emitted bodies passes through a step.
func TestEmittedStepsAdvance(t *testing.T) {
	t.Parallel()
	type corpusCase struct {
		name, src string
		inputs    []string
	}
	var cases []corpusCase
	for _, c := range codegentest.GenCorpus(t) {
		cases = append(cases, corpusCase{c.Name, c.Src, c.Inputs})
	}
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("testdata grammars: %v", err)
	}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, corpusCase{filepath.Base(filepath.Dir(path)), string(src), grammarCorpus(t, path)})
	}
	var stepping atomic.Int32
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			prog := mustCompile(t, c.src)
			if !slices.ContainsFunc(slices.Collect(maps.Values(prog.Module().Flows)), func(f *bytecode.Flow) bool {
				return f != nil && slices.ContainsFunc(f.Code, func(in bytecode.FlowInstr) bool { return in.Op == bytecode.FlowStep })
			}) {
				return
			}
			stepping.Add(1)
			check := func(what string, err error) {
				t.Helper()
				if err != nil && strings.Contains(err.Error(), "invalid bytecode") {
					t.Errorf("%s: %v", what, err)
				}
			}
			unlimited := runtime.Limits{Compute: runtime.Unlimited}
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				for _, in := range c.inputs {
					for _, mode := range allModes {
						_, err := prog.ParseWith("", in, servedBy(mode, ParseOptions{Backend: b, Limits: unlimited}))
						check(fmt.Sprintf("mode %d %q", mode, strings.ToValidUTF8(in[:min(len(in), 40)], "")), err)
					}
					d, err := prog.NewDocumentWith("", in, ParseOptions{Backend: b, Limits: unlimited})
					if err != nil {
						continue
					}
					_, err = d.Parse()
					check("document", err)
					mid := len([]rune(in)) / 2
					if err := d.Edit(mid, mid, " "); err == nil {
						_, err = d.Parse()
						check("document after an edit", err)
					}
				}
			}
		})
	}
	t.Cleanup(func() {
		if stepping.Load() == 0 {
			t.Error("no grammar of the corpus steps")
		}
		t.Logf("%d of %d grammars step", stepping.Load(), len(cases))
	})
}
