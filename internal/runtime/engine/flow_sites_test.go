package engine

import (
	"math/rand"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestFlowBodySites runs grammars with skip-run, run and scan sites through both VM models with
// every rule running its Flow body: the trees and the errors must be those of the closure backend. The run sites turn hot at a short span, so the walks of the Flow bodies record and take the
// rest from records (RUNBEGIN, SKIPVISIT, SKIPHIT, SKIPVALUE, SKIPEND, SKIPKIDS), and the long runs
// of the inputs reach the records of scan sites (SCANREACH, SCANEND, SCANKIDS). The scan sites keep
// the span of the Flow lowering (flow.ScanHotSpan), which the runtime's must equal.
func TestFlowBodySites(t *testing.T) {
	t.Parallel()
	stats := new(state.HotStats)
	tune := state.Tuning{RunHot: 2, Stats: stats}
	type grammar struct{ Name, Src, Alphabet string }
	var gs []grammar
	for _, g := range codegentest.RunSiteGrammars {
		gs = append(gs, grammar(g))
	}
	for _, g := range codegentest.RunValueGrammars {
		gs = append(gs, grammar(g))
	}
	for _, g := range codegentest.ScanSiteGrammars {
		gs = append(gs, grammar(g))
	}
	rng := rand.New(rand.NewSource(149))
	ops := map[bytecode.FlowOp]int{}
	var turns, hits int64
	for _, g := range gs {
		old, flows, n := flowProgramsOf(t, g.Src, func(string) bool { return true }, func(_ string, f *bytecode.Flow) {
			for _, in := range f.Code {
				ops[in.Op]++
			}
		})
		if n == 0 {
			t.Logf("%s: no rule has a Flow body yet", g.Name) // as for bindings, which have no instructions yet
			continue
		}
		inputs := append(codegentest.SkipRunInputs(g.Alphabet, 100, rng), codegentest.ScanSiteInputs(g.Alphabet, 100, rng)...)
		for _, c := range g.Alphabet {
			inputs = append(inputs, strings.Repeat(string(c), 80), strings.Repeat(string(c), 40)+strings.Repeat(g.Alphabet, 20))
		}
		for _, in := range inputs {
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				ot, oe, _ := parseChargedTuned(t, old, in, Closure, tune)
				t0, h0 := stats.RunHotTurns.Load(), stats.ScanHits.Load()
				ft, fe, _ := parseChargedTuned(t, flows, in, b, tune)
				turns += stats.RunHotTurns.Load() - t0
				hits += stats.ScanHits.Load() - h0
				if ot != ft || oe != fe {
					t.Fatalf("%s, %v, input %q:\n closure %s %s\n flow    %s %s", g.Name, b, in, ot, oe, ft, fe)
				}
			}
		}
	}
	for _, op := range []bytecode.FlowOp{
		bytecode.FlowRunHot, bytecode.FlowRunBegin, bytecode.FlowRunWalked, bytecode.FlowSkipVisit,
		bytecode.FlowSkipHit, bytecode.FlowSkipValue, bytecode.FlowSkipEnd, bytecode.FlowSkipKids,
		bytecode.FlowScanReachAt, bytecode.FlowScanEndAt, bytecode.FlowScanKids,
	} {
		if ops[op] == 0 {
			t.Errorf("no Flow body has %v", op)
		}
	}
	if turns == 0 {
		t.Error("no run site turned hot in the parses of the Flow bodies")
	}
	if hits == 0 {
		t.Error("no walk of a scan site took a record in the parses of the Flow bodies")
	}
}
