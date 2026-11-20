//go:build !zp_budget_controls_reference

package engine

import (
	"testing"

	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/frontend/parser"
	"zeroparse.dev/zp/internal/testing/starttest"
	"zeroparse.dev/zp/pkg/runtime"
	zpinput "zeroparse.dev/zp/pkg/runtime/input"
)

// TestTracedUnitsEqualUntraced checks that a traced parse charges exactly the units of an untraced one
// on every backend, as spec/runtime/parsing.md requires of the charges of the same grammar and input:
// the smallest sufficient compute limit of the grammar below is the same with and without a tracer on
// the closure backend and both bytecode VMs. TestTracedChargeMutantsFail is its negative control.
func TestTracedUnitsEqualUntraced(t *testing.T) {
	g, err := parser.Parse("rule main = a a $$\nrule a = \"x\"\n")
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(starttest.MarkAll(g), compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// smallest returns the smallest compute limit under which the parse succeeds.
	smallest := func(back Backend, traced bool) uint64 {
		for n := uint64(1); n < 200; n++ {
			o := ParseOptions{Backend: back, Unit: zpinput.Bytes, Limits: runtime.Limits{Compute: n}}
			if traced {
				o.Trace = func(TraceEvent) {}
			}
			if _, err := prog.ParseWith("main", "xx", o); err == nil {
				return n
			}
		}
		t.Fatalf("%v: no limit below 200 lets the parse succeed", back)
		return 0
	}
	for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if plain, traced := smallest(back, false), smallest(back, true); plain != traced {
			t.Errorf("%v: %d units untraced, %d traced", back, plain, traced)
		}
	}
}
