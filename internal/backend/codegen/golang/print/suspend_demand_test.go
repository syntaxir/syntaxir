package print_test

import (
	"sort"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
)

// TestSuspendedLoweringDemandsItsReads lowers every table of the generator's decision with
// lowerdata.Options.Suspend, as the TypeScript -stream parser does, and checks every function with
// flow.UncoveredReads: each read of input must be dominated by a demand (flow.inputDemand) with no change
// of position in between. The tables are those the Go decision planned (coresOf), with the Core IR and the
// options that the generator gives each one; the Go and batch lowerings are not changed by Suspend, so
// this test is the only one that sees the demands.
func TestSuspendedLoweringDemandsItsReads(t *testing.T) {
	if testing.Short() {
		t.Skip("lowers the corpus")
	}
	found := map[string]int{}
	mode := coreGenModes[0]
	for _, gen := range coreGenerations(t, mode) {
		cores := coresOf(gen.gen)
		if cores == nil {
			t.Fatalf("%s: the generation recorded no tables", gen.name)
		}
		for _, u := range cores.Units {
			if u.Table == "rules" && u.NoOperators {
				// A typed-only parser never prints its Node table: its rules are the typed table's (print.NoNodeRules).
				continue
			}
			opts := u.IR.Opts
			opts.Suspend = true
			m := u.IR.M
			check := func(f *code.Module, err error) {
				if err != nil {
					t.Fatalf("%s %s: %v", gen.name, u.Label, err)
				}
				if len(f.Funcs) == 0 {
					// A flat function that the plan refuses and has no fallback for is left out.
					return
				}
				if err := flow.VerifyModule(f); err != nil {
					t.Fatalf("%s %s: %v", gen.name, u.Label, err)
				}
				for _, name := range flow.UncoveredReads(flow.FuncOf(f)) {
					found[name]++
				}
			}
			if u.Typed {
				// The typed table's flat rules are never refused, and its framed rules are lowered as the plan does.
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
				continue
			}
			// The other tables are lowered function by function as the plan of the generator lays them out
			// (flowplan.Plan over all rules), each flat function refused by the plan (flowplan.Refused) in
			// favor of its framed fallback, as flowlower.LowerUnit does in the generator's planning.
			for _, s := range flowplan.Plan(u.IR, u.Rules, nil).Funcs {
				fn := s.Func
				if s.Fallback != nil {
					fb := s.Fallback.Func
					fn.Fallback = &fb
				}
				check(flowlower.LowerUnit(lowerdata.Unit{Module: m, Opts: opts, Funcs: []lowerdata.Func{fn},
					Refuse: func(_ int, f *code.Func) bool { return flowplan.Refused(f) }}))
			}
		}
	}
	var names []string
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Errorf("%s: %d reads of input with no dominating demand", n, found[n])
	}
	t.Logf("uncovered reads by operation: %s", strings.Join(names, " "))
}
