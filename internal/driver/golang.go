package driver

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	gobackend "syntaxir.dev/sxir/internal/backend/codegen/golang"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The pipeline of a Go target:
//
//	with target=go {
//	  each go-tables {
//	    core-config { ... }
//	  }
//	  each action-tables { ... }
//	  dispatch
//	  plan!
//	  each planned-tables {
//	    with table-config { flowlower! ... }
//	  }
//	  print-go!
//	}
//
// go-tables splits the program into the tables that the Go parser of the target has (gobackend.Decide): the
// Node table, the recognizer, the typed table and their traced copies, each labeled as its dumps
// are. Its join gives the Core modules of the tables with what the program and its tables are planned
// from (cores). action-tables lowers the actions of each table into its Action module (actions),
// the pass dispatch chooses the guards that the tables keep (flowplan.Dispatching), and
// plan! plans them. planned-tables splits the plan into the planned tables and joins
// them, at the Exec stage, into tables@exec, numbering the expectation table of the Node and
// recognizer tables. print-go prints the parser of the target from them.

// goTables is the split go-tables. It splits the program for the Go target of the scope it runs
// in (codegen.TargetFeature).
var goTables = pipe.RegisterSplit(programIR, tableIR, corepass.IR, plandata.CoresIR, &pipe.Split[*Program, common.TableSource, *core.Module, *plandata.Cores]{
	Name: "go-tables", Labels: gobackend.TableLabels, Features: []pipe.FeatureID{codegen.TargetFeature.ID()},
	Parts: func(x pipe.SplitCtx, _ *Program) ([]common.TableSource, []string, error) {
		d, err := goDecision(x)
		if err != nil {
			return nil, nil, err
		}
		return d.Sources(), d.Labels(), nil
	},
	Join: func(x pipe.SplitCtx, _ *Program, mods []*core.Module) (*plandata.Cores, error) {
		d, err := goDecision(x)
		if err != nil {
			return nil, err
		}
		return d.Cores(mods), nil
	},
})

// goDecision returns the decision of the Go parser of the target of the scope of x, which the first
// call makes.
func goDecision(x pipe.SplitCtx) (*gobackend.Decision, error) {
	target := pipe.SplitFeature(x, codegen.TargetFeature)
	t, ok := target.(*gobackend.Target)
	if !ok {
		return nil, fmt.Errorf("internal error: the split go-tables runs for the target %s", target.Backend())
	}
	return t.Decision()
}

// goPipeline returns the pipeline of a Go target.
func goPipeline() assembled {
	p := pipe.Then3(
		pipe.Then3(pipe.Each(goTables, coreConfig()), pipe.Then(actions(goActions), pipe.Of(flowplan.Dispatching)), pipe.Of(flowplan.Planning)),
		pipe.Each(plannedTables, lowering(gobackend.FlowTarget, gopass.Pipeline)),
		pipe.Of(gobackend.Printing))
	return assembled{pipeline: p, steps: []string{Elaborate, "each go-tables", gobackend.Printing.Name()}}
}
