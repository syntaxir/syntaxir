package print_test

import (
	"sort"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
)

// suspendGaps are the input-reading operations that the lowering of a suspended stream does not demand
// yet, each with the change that removes it. The corpus here is the typed table only: matchClass, hasLit,
// skipTerminal and skipClasses are not emitted by it, so they are mapped against the recognizer and Node tables
// in C2 before they can be listed or removed. The list shrinks to nothing as the demands land; the test fails
// both for an uncovered read that is not listed and for a listed operation with no uncovered read.
var suspendGaps = map[string]string{
	flow.OpPeek:          "C1: one-character demand (flow.inputDemand, character form) before peek",
	flow.OpGuardLitFails: "C1: guard of a literal, demand before it",
	flow.OpSkipVisit:     "C3: skip walk, demand per character inside the loop",
	flow.OpScanReach:     "C3: scan walk, demand per character inside the loop",
}

// TestSuspendedLoweringDemandsItsReads lowers the corpus with lowerdata.Options.Suspend, as the TypeScript
// -stream parser does, and checks every function with flow.UncoveredReads: each read of input must be
// dominated by a demand (flow.inputDemand) with no change of position in between. The Go and batch lowerings
// are not changed by Suspend, so this test is the only one that sees the demands.
func TestSuspendedLoweringDemandsItsReads(t *testing.T) {
	if testing.Short() {
		t.Skip("lowers the corpus")
	}
	found := map[string]int{}
	mode := coreGenModes[0]
	for _, gen := range coreGenerations(t, mode) {
		g := gen.gen
		m, err := typedCore(g, corepass.RunOptions{})
		if err != nil {
			t.Fatalf("%s: %v", gen.name, err)
		}
		// The typed table of the mode, lowered as the corpus test lowers it, with the demands on.
		opts := typedOptions(g, m)
		opts.Suspend = true
		opts.Inserts = flowlower.Inserts(m)
		check := func(f *code.Module, err error) {
			if err != nil {
				t.Fatalf("%s: %v", gen.name, err)
			}
			if err := flow.VerifyModule(f); err != nil {
				t.Fatalf("%s: %v", gen.name, err)
			}
			for _, name := range flow.UncoveredReads(flow.FuncOf(f)) {
				found[name]++
			}
		}
		for _, r := range m.Rules {
			if flowlower.LocalRule(r) {
				check(flowlower.Lower(m, r, r.Name, opts))
				continue
			}
			if o, ok := r.Body.(*core.OperatorsRule); ok {
				for _, l := range o.Lines() {
					if flowlower.FramedBody(l.Body) {
						check(flowlower.LowerFrame(m, r, l.Body, l.Scope, l.Body.Attrs().Want, r.Name, opts))
					}
				}
			} else if flowlower.FramedBody(r.Body) {
				check(flowlower.LowerFrame(m, r, r.Body, r.Scope, r.Want, r.Name, opts))
			}
		}
	}
	var names []string
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := suspendGaps[n]; !ok {
			t.Errorf("%s: %d reads of input with no dominating demand", n, found[n])
		}
	}
	for n, why := range suspendGaps {
		if found[n] == 0 {
			t.Errorf("%s is listed as a gap (%s) but no read of it is uncovered: remove the entry", n, why)
		}
	}
	t.Logf("uncovered reads by operation: %s", strings.Join(names, " "))
}
