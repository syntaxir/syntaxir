package driver

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The parts of the pipeline of a table that every generating backend runs.
//
//	core-config {
//	  elaborate!
//	  slot core-passes { Core passes }
//	}
//
// and, once the table is planned,
//
//	with table-config {
//	  flowlower!
//	  within module@flow {
//	    func funcs@flow { slot flow-passes { Flow passes } }
//	    flow-finish!
//	  }
//	  execlower!
//	  within module@exec {
//	    func funcs@exec { slot exec-passes { Exec passes } }
//	    func lowered-funcs@exec { slot target-passes { the target's Exec passes } }
//	  }
//	}

// coreConfig returns the Core part of the pipeline of a table: elaborate, which runs the build of the
// table's Core IR and computes its attributes, and the Core passes in the slot CoreSlot. The module
// is dumped as "elaborate" as the build made it, with the attributes of its nodes. The tables whose
// builds have the same key share one run of it (coreKey), whose dumps are written again with the
// label of each.
func coreConfig() pipe.Pipeline[common.TableSource, *core.Module] {
	return pipe.Shared(coreKey, pipe.Then(pipe.Of(elaborating), pipe.Slot(CoreSlot, corepass.Pipeline())))
}

// tableConfig derives the features of a planned table from its unit: whether the parser reports the
// rules and the skipped alternatives of the table (flowpass.ObserveFeature), which the options of
// its lowering say.
var tableConfig = pipe.RegisterDerive(plandata.PlannedIR, &pipe.Derive[plandata.Planned, bool]{
	Name: "table-config", Key: flowpass.ObserveFeature,
	Of: func(p plandata.Planned) bool { return p.IR.Opts.Observe },
})

// lowering returns the pipeline of a planned table from its lowering to the Exec stage, for a backend
// that prints the forms flowTarget of the Flow passes and runs the Exec passes target on each lowered
// function.
//
// The lowering flowlower lowers the functions of the plan, with its decisions (plandata.Planned.Refuse),
// to the table at the Flow stage, which its postcondition verifies. The Flow passes run on each
// function of the module, pass by pass (pipe.PerFunc), and flow-finish ends each function, also after a
// failed pass. The conversion execlower converts the module to the Exec stage, which its
// postcondition verifies; the Exec passes that every target runs and then those of target run on each
// function, the latter as a lowered function (execlower.LoweredFuncs), which keeps what the printers
// print: the function in its module with its variables (execlower.Lowered.Keeps). Every pass keeps the
// functions of the plan in the module, which the lenses of the module check (plandata.Table.With).
//
// The plan and the Core module of the planned table stay the caller's: flowlower only reads them, and
// the module it makes shares the Core nodes and rules with the Core module, which the passes and
// flow-finish read and never change (flow.Forms).
func lowering(flowTarget flowpass.Target, target TargetPipeline) pipe.Pipeline[plandata.Planned, *plandata.Table[code.Exec]] {
	flow := pipe.Within(plandata.FlowModule, pipe.Then(
		pipe.WithConst(flowpass.TargetFeature, flowTarget,
			pipe.PerFunc(code.FlowFuncs, pipe.Slot(FlowSlot, pipe.Seq(code.FlowStage, flowpass.Passes...)))),
		pipe.Of(flowpass.Finishing)))
	exec := pipe.Then(pipe.Of(execlower.Lowering), pipe.Within(plandata.ExecModule, pipe.Then(
		pipe.PerFunc(code.ExecFuncs, pipe.Slot(ExecSlot, pipe.Seq(code.ExecStage, codepass.Passes...))),
		pipe.PerFunc(execlower.LoweredFuncs, pipe.Slot(TargetSlot, target)))))
	return pipe.WithDerived(tableConfig, pipe.Then3(pipe.Of(flowlower.Lowering), flow, exec))
}

// plannedTables is the split planned-tables of every backend that plans the program: it splits the
// plan into the planned tables of the program (plandata.Program.Units), labeled as their units, and
// joins them, at the Exec stage, into tables@exec, numbering the expectation table from the Node and
// recognizer tables with every rule (common.JoinTables). A table that the plan prunes has a part of
// the functions it leaves out, labeled with flowplan.UnreachableSuffix, which only the numbering
// reads.
var plannedTables = pipe.RegisterSplit(plandata.ProgramIR, plandata.PlannedIR, plandata.ExecTable, common.TablesIR, &pipe.Split[*plandata.Program, plandata.Planned, *plandata.Table[code.Exec], *common.Tables]{
	Name: "planned-tables", Labels: plannedLabels(goprint.TableLabels),
	Parts: func(p *plandata.Program) ([]plandata.Planned, []string, error) {
		units, labels := make([]plandata.Planned, len(p.Units)), make([]string, len(p.Units))
		for i, u := range p.Units {
			units[i], labels[i] = u.Planned, u.Label
		}
		return units, labels, nil
	},
	Join: common.JoinTables,
})

// plannedLabels returns the labels of the planned tables of a backend whose tables are labeled labels:
// each label, and each with flowplan.UnreachableSuffix.
func plannedLabels(labels ...[]string) []string {
	var out []string
	for _, l := range labels {
		out = append(out, l...)
		for _, n := range l {
			out = append(out, n+flowplan.UnreachableSuffix)
		}
	}
	return out
}
