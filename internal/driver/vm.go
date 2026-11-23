package driver

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The pipeline of a VM target:
//
//	with target=vm {
//	  each vm-tables {
//	    core-config { ... }
//	  }
//	  dispatch
//	  plan!
//	  each vm-planned-tables {
//	    with table-config {
//	      flowlower!
//	      within module@flow { ... }
//	    }
//	  }
//	  vmlower!
//	}
//
// vm-tables gives the one table that the VM runs, the Node table labeled rules, and its join gives
// what the program is planned from (vmCores). The pass dispatch chooses the guards that the table
// keeps (flowplan.Dispatching), plan! plans it, and vm-planned-tables lowers the
// planned table to the end of the Flow stage, for the forms of the bytecode (flowpass.Bytecode), and
// joins it with the program into vm-input, which vmlower! lowers to the bytecode module.

// vmTarget is a VM target as its splits read it (codegen.TargetFeature): the checked program whose
// bytecode it builds, and whether its Core module records the origin map, as the compilation decides
// (origin).
type vmTarget struct {
	prog   *frontend.Result
	origin bool
}

// Backend returns the name of the VM backend.
func (*vmTarget) Backend() string { return result.VM.String() }

// vmTable is the label of the one table of a VM target, the Node table.
const vmTable = "rules"

// vmTableConfig is the table of a VM target, which is the Node table of a generated Go parser with
// Documents that counts the reference variant of logical compute units, so that the VM runs the same
// functions: the same Core IR and lowering options, values and logical units, lowered for Document
// parses, which resume runs. Its guards are those whose skip makes no call (lowerdata.GuardsEvent):
// the VM does not make the effects of skipped calls (flow.skippedCalls), so it tries an alternative
// whose evaluation calls rules, and every call counts where it happens.
var vmTableConfig = plandata.TableConfig{Values: true, Logical: true, Guards: lowerdata.GuardsEvent, Document: true, Resume: true}

// vmTargetOf returns the VM target of the scope of x, for the split split.
func vmTargetOf(x pipe.SplitCtx, split string) (*vmTarget, error) {
	target := pipe.SplitFeature(x, codegen.TargetFeature)
	t, ok := target.(*vmTarget)
	if !ok {
		return nil, fmt.Errorf("the split %s runs for the target %s", split, target.Backend())
	}
	return t, nil
}

// vmTables is the split vm-tables: the one table of the VM target of the scope it runs in, built as
// the Node table of a generated parser is (key "program"), and joined into what its program is
// planned from (vmCores).
var vmTables = pipe.RegisterSplit(programIR, tableIR, corepass.IR, plandata.CoresIR, &pipe.Split[*Program, common.TableSource, *core.Module, *plandata.Cores]{
	Name: "vm-tables", Labels: []string{vmTable}, Features: []pipe.FeatureID{codegen.TargetFeature.ID()},
	Parts: func(x pipe.SplitCtx, _ *Program) ([]common.TableSource, []string, error) {
		t, err := vmTargetOf(x, "vm-tables")
		if err != nil {
			return nil, nil, err
		}
		return []common.TableSource{{Build: flowplan.TableBuild(t.prog, vmTableConfig), Key: "program", Origin: t.origin}}, []string{vmTable}, nil
	},
	Join: func(x pipe.SplitCtx, _ *Program, mods []*core.Module) (*plandata.Cores, error) {
		t, err := vmTargetOf(x, "vm-tables")
		if err != nil {
			return nil, err
		}
		if len(mods) != 1 {
			return nil, fmt.Errorf("the VM builds one table, not %d", len(mods))
		}
		return vmCores(t.prog, mods[0]), nil
	},
})

// vmCores returns what the program prog, whose Node table after the Core passes is m, is planned from
// for the VM: the Node table, whose calls all enter the slots of their rules, which memoize every rule
// for Documents; its start rules as the entry points; and the table as the one unit, with every rule,
// since one module serves every start rule, and the stream body of each rule that has a #stream
// repetition, since any such rule may start a stream parse.
func vmCores(prog *frontend.Result, m *core.Module) *plandata.Cores {
	rules := append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...)
	c := &plandata.Cores{}
	t := plandata.CoreTable{Name: vmTable, M: m, Slots: true}
	u := plandata.Unit{Label: vmTable, Table: vmTable, IR: flowplan.TableOf(m, vmTableConfig), Rules: planRules(rules)}
	for i, r := range rules {
		_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
		t.Rules = append(t.Rules, plandata.CallRule{Plain: r.Plain, Leader: r.Leader, Operators: operatorsRule})
		if i < len(prog.RuleList) && r.Stream {
			u.Streams = append(u.Streams, u.Rules[i])
		}
	}
	run := prog.Runtime()
	for _, name := range run.StartRules {
		c.Roots = append(c.Roots, plandata.Entry{Table: vmTable, ID: run.ByName[name].ID})
	}
	c.Tables, c.Units = []plandata.CoreTable{t}, []plandata.Unit{u}
	return c
}

// vmPlannedTables is the split vm-planned-tables: the one planned table of the plan of a VM target,
// joined, at the end of the Flow stage, with the program into vm-input. A rule with a #stream
// repetition whose stream body the plan does not hold is an internal error.
var vmPlannedTables = pipe.RegisterSplit(plandata.ProgramIR, plandata.PlannedIR, plandata.FlowTable, vmlower.InputIR, &pipe.Split[*plandata.Program, plandata.Planned, *plandata.Table[code.Flow], vmlower.Input]{
	Name: "vm-planned-tables", Labels: []string{vmTable}, Features: []pipe.FeatureID{codegen.TargetFeature.ID()},
	Parts: func(_ pipe.SplitCtx, p *plandata.Program) ([]plandata.Planned, []string, error) {
		if len(p.Units) != 1 {
			return nil, nil, fmt.Errorf("the plan of the VM has %d tables, not one", len(p.Units))
		}
		return []plandata.Planned{p.Units[0].Planned}, []string{p.Units[0].Label}, nil
	},
	Join: func(x pipe.SplitCtx, p *plandata.Program, tables []*plandata.Table[code.Flow]) (vmlower.Input, error) {
		t, err := vmTargetOf(x, "vm-planned-tables")
		if err != nil {
			return vmlower.Input{}, err
		}
		table := tables[0]
		for _, r := range p.Cores.Units[0].Streams {
			if _, ok := table.Index(plandata.Key{Use: plandata.StreamBody, Rule: r.ID}); !ok {
				return vmlower.Input{}, fmt.Errorf("rule %s has no stream body", r.Name)
			}
		}
		return vmlower.Input{Run: t.prog.Runtime(), Elab: t.prog, Table: table}, nil
	},
})

// planRules returns what the plan of a table reads of the compiled rules (plandata.Rule).
func planRules(rules []*check.Rule) []*plandata.Rule {
	out := make([]*plandata.Rule, len(rules))
	for i, r := range rules {
		out[i] = &plandata.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean,
			Novalue: r.Novalue, Leader: r.Leader}
	}
	return out
}

// vmPipeline returns the pipeline of a VM target.
func vmPipeline() assembled {
	p := pipe.Then3(
		pipe.Then3(pipe.Each(vmTables, coreConfig()), pipe.Of(flowplan.Dispatching), pipe.Of(flowplan.Planning)),
		pipe.Each(vmPlannedTables, flowLowering(flowpass.Bytecode)),
		pipe.Of(vmlower.Lowering))
	return assembled{pipeline: p, steps: []string{Elaborate, flowlower.Name, vmlower.Name}}
}

// flowLowering returns the pipeline of a planned table from its lowering to the end of the Flow stage,
// for a backend that runs the functions of the table at the Flow stage and prints the forms
// flowTarget: the steps of lowering before execlower.
func flowLowering(flowTarget flowpass.Target) pipe.Pipeline[plandata.Planned, *plandata.Table[code.Flow]] {
	return pipe.WithDerived(tableConfig, pipe.Then(pipe.Of(flowlower.Lowering), flowStage(flowTarget)))
}
