package driver

import (
	"context"
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// FlowTable builds the Node table of the program p at the Flow stage for the Flow route of the bytecode
// VM, the way a generated Go parser that counts logical compute units builds its Node table, so that
// the VM runs the same functions: the same Core IR and lowering options (plandata.TableConfig with
// values and logical units), lowered for Document parses, which resume runs; the same plan with its decisions
// (flowplan.Plan, flowplan.Unit), with every rule and the stream body of each rule that has a #stream
// repetition (flowplan.Stream), since any such rule may start a stream parse; and the parts of the
// pipeline of a table that every backend runs, the Core part and, on the planned table, flowlower and
// the Flow stage, at the default optimizations for the VM's target (flowpass.Bytecode). It is an
// adapter for the VM until its own pipeline is assembled, which runs the plan as a step: until then
// the plan of the one table is made between the two runs.
//
// A failure of elaborate or flowlower is returned as the *pass.StepError that names it; any other
// failure is an internal error.
//
// Its guards are those whose skip makes no call (lowerdata.GuardsEvent): the VM does not make the
// effects of skipped calls (flow.skippedCalls), so it tries an alternative whose evaluation calls
// rules, and every call counts where it happens.
func FlowTable(ctx context.Context, p *frontend.Result) (*plandata.Table[code.Flow], error) {
	cfg := plandata.TableConfig{Values: true, Logical: true, Guards: lowerdata.GuardsEvent}
	errOf := func(err error) error { return assembled{steps: []string{Elaborate, flowlower.Name}}.error(err) }
	m, err := pipe.Execute(ctx, &pipe.Run{Stop: true}, corePart(), common.TableSource{Build: flowplan.TableBuild(p, cfg)})
	if err != nil {
		return nil, fmt.Errorf("Core IR: %w", errOf(err))
	}
	ir := flowplan.TableOf(m, cfg)
	ir.Opts.Document, ir.Opts.Resume = true, true // one module serves Document parses, which resume runs
	rules := append(append([]*check.Rule(nil), p.RuleList...), p.Twins...)
	planned := planRules(rules)
	plan := flowplan.Plan(ir, planned, nil)
	for i, r := range p.RuleList {
		// Any rule with a #stream repetition may start a stream parse, which runs its stream body.
		if r.Stream && !flowplan.Stream(plan, ir, planned[i]) {
			return nil, fmt.Errorf("internal error: rule %s has no stream body", r.Name)
		}
	}
	t, err := pipe.Execute(ctx, &pipe.Run{}, flowLowering(flowpass.Bytecode), flowplan.Unit(plan, ir, nil))
	if err != nil {
		return nil, fmt.Errorf("table rules: %w", errOf(err))
	}
	return t, nil
}

// flowLowering returns the pipeline of a planned table from its lowering to the end of the Flow stage,
// for a backend that runs the functions of the table at the Flow stage and prints the forms
// flowTarget: the steps of lowering before execlower.
func flowLowering(flowTarget flowpass.Target) pipe.Pipeline[plandata.Planned, *plandata.Table[code.Flow]] {
	return pipe.WithDerived(tableConfig, pipe.Then(pipe.Of(flowlower.Lowering), flowStage(flowTarget)))
}

// planRules returns what the plan of a table reads of the compiled rules (plandata.Rule).
func planRules(rules []*check.Rule) []*plandata.Rule {
	out := make([]*plandata.Rule, len(rules))
	for i, r := range rules {
		out[i] = &plandata.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean,
			Novalue: r.Novalue, Leader: r.Leader}
	}
	return out
}
