package print_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	gobackend "syntaxir.dev/sxir/internal/backend/codegen/golang"
	tsbackend "syntaxir.dev/sxir/internal/backend/codegen/typescript"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// flowPassed runs the Flow passes of the target on the function of m, then the finishing pass, as the planned
// tables do, and returns the module the passes leave. The passes change the function in place.
func flowPassed(m *code.Module, target flowpass.Target) (*code.Module, error) {
	p := pipe.WithConst(flowpass.ObserveFeature, false, pipe.WithConst(flowpass.TargetFeature, target,
		pipe.Then(flowpass.Pipeline(), pipe.Of(flowpass.Finishing))))
	out, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, p, code.At[code.Flow]{}.With(m))
	if err != nil {
		return nil, err
	}
	return out.Module(), nil
}

// TestSuspendedLoweringDemandsItsReads lowers every table of the generator's decision in every mode with
// lowerdata.Options.Suspend, as the TypeScript -stream parser does, and checks every function with
// flow.UncoveredReads: each read of input must be dominated by a demand (flow.inputDemand) on every path with no
// change of position in between. It checks the functions as lowered, and then after the Flow passes of the Go
// and TypeScript targets. The Flow passes refuse a suspended function (flowpass.run) while no form pass is
// demand-aware, so the Go target is refused for every function a form pass applies to, and the refusal must name
// the pass; the TypeScript target runs no form pass, and the verifier (flow.VerifyDemands) checks what it keeps.
// The tables are those the Go decision planned (coresOf), with the Core IR and the options that the generator
// gives each one; the Go and batch lowerings are not changed by Suspend, so this test is the only one that sees
// the demands.
func TestSuspendedLoweringDemandsItsReads(t *testing.T) {
	if testing.Short() {
		t.Skip("lowers the corpus")
	}
	targets := []struct {
		name   string
		target flowpass.Target
	}{{"go", gobackend.FlowTarget}, {"ts", tsbackend.FlowTarget}}
	found := map[string]int{}
	refused := map[string]int{}
	checked := map[string]int{}
	var failures []string
	for _, mode := range coreGenModes {
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
				var lowerings []func() (*code.Module, error)
				if u.Typed {
					// The typed table's flat rules are never refused, and its framed rules are lowered as the plan does.
					for _, r := range m.Rules {
						if flowlower.LocalRule(r) {
							lowerings = append(lowerings, func() (*code.Module, error) { return flowlower.Lower(m, r, r.Name, opts) })
							continue
						}
						if o, ok := r.Body.(*core.OperatorsRule); ok {
							for _, l := range o.Lines() {
								if flowlower.FramedBody(l.Body) {
									lowerings = append(lowerings, func() (*code.Module, error) {
										return flowlower.LowerFrame(m, r, l.Body, l.Scope, l.Body.Attrs().Want, r.Name, opts)
									})
								}
							}
						} else if flowlower.FramedBody(r.Body) {
							lowerings = append(lowerings, func() (*code.Module, error) {
								return flowlower.LowerFrame(m, r, r.Body, r.Scope, r.Want, r.Name, opts)
							})
						}
					}
				} else {
					// The other tables are lowered function by function as the plan of the generator lays them out
					// (flowplan.Plan over all rules), each flat function refused by the plan (flowplan.Refused) in
					// favor of its framed fallback, as flowlower.LowerUnit does in the generator's planning.
					for _, s := range flowplan.Plan(u.IR, u.Rules, nil).Funcs {
						fn := s.Func
						if s.Fallback != nil {
							fb := s.Fallback.Func
							fn.Fallback = &fb
						}
						lowerings = append(lowerings, func() (*code.Module, error) {
							return flowlower.LowerUnit(lowerdata.Unit{Module: m, Opts: opts, Funcs: []lowerdata.Func{fn},
								Refuse: func(_ int, f *code.Func) bool { return flowplan.Refused(f) }})
						})
					}
				}
				for _, lower := range lowerings {
					f, err := lower()
					if err != nil {
						t.Fatalf("%s %s: %v", gen.name, u.Label, err)
					}
					if len(f.Funcs) == 0 {
						// A flat function that the plan refuses and has no fallback for is left out.
						continue
					}
					if err := flow.VerifyModule(f); err != nil {
						t.Fatalf("%s %s: %v", gen.name, u.Label, err)
					}
					for _, name := range flow.UncoveredReads(flow.FuncOf(f)) {
						found[name]++
					}
					for _, tg := range targets {
						g, err := lower()
						if err != nil {
							t.Fatalf("%s %s: %v", gen.name, u.Label, err)
						}
						checked[tg.name]++
						g, err = flowPassed(g, tg.target)
						if err != nil {
							if strings.Contains(err.Error(), "is not demand-aware") {
								refused[tg.name]++
								continue
							}
							failures = append(failures, fmt.Sprintf("%s %s %s: %v", gen.name, u.Label, tg.name, err))
							continue
						}
						if err := flow.VerifyModule(g); err != nil {
							failures = append(failures, fmt.Sprintf("%s %s %s after the passes: %v", gen.name, u.Label, tg.name, err))
						}
						for _, name := range flow.UncoveredReads(flow.FuncOf(g)) {
							found[name]++
						}
					}
				}
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
	for i, msg := range failures {
		if i == 10 {
			t.Errorf("%d more failures", len(failures)-i)
			break
		}
		t.Error(msg)
	}
	t.Logf("functions checked by target: go %d, ts %d; refused as not demand-aware by target: go %d, ts %d",
		checked["go"], checked["ts"], refused["go"], refused["ts"])
	t.Logf("uncovered reads by operation: %s", strings.Join(names, " "))
}
