//go:build sxir_long

package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestRunSitesCorpusMatchWithoutRecords checks that the grammars of testdata/grammars give, on their
// sample inputs and on prefixes of them that fail in the middle, byte-identical results with the
// records of run sites taken from the first walk on and with records off, on every backend: a walk
// that takes a record gives what one that evaluates its iterations gives.
func TestRunSitesCorpusMatchWithoutRecords(t *testing.T) {
	t.Parallel()
	grammars, _ := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if len(grammars) == 0 {
		t.Fatal("no corpus grammars")
	}
	stats := new(state.HotStats)
	for _, g := range grammars {
		src, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		prog := enginetest.MustCompile(t, string(src))
		texts, _ := filepath.Glob(filepath.Join(filepath.Dir(g), "*.txt"))
		var inputs []string
		for _, f := range texts {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			in := string(b)
			inputs = append(inputs, in, in[:len(in)/2], in[:len(in)*3/4])
		}
		for _, in := range inputs {
			for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
				o := engine.ParseOptions{Backend: b, Expectations: true, Unit: input.Bytes, Tune: state.Tuning{RunHot: 1, ScanHot: 1, Stats: stats}}
				n, err := prog.ParseWith(prog.DefaultStartRule, in, o)
				with := codegentest.ResultJSON(n, err)
				o.Tune.SkipOff = true
				n, err = prog.ParseWith(prog.DefaultStartRule, in, o)
				without := codegentest.ResultJSON(n, err)
				if with != without {
					t.Errorf("%s, %s, input of %d bytes %q...: records change the result\n with    %.300s\n without %.300s",
						filepath.Base(g), b, len(in), strings.SplitN(in, "\n", 2)[0], with, without)
				}
			}
		}
	}
	if stats.RunHotTurns.Load() == 0 {
		t.Error("no run site turned hot: the records were never taken")
	}
}
