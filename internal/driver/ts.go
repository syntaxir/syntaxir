package driver

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The pipeline of a TypeScript target:
//
//	with target=ts {
//	  each ts-tables {
//	    core-config { ... }
//	  }
//	  plan!
//	  each planned-tables {
//	    with table-config { flowlower! ... }
//	  }
//	  print-ts!
//	}
//
// ts-tables splits the compilation into the tables that the TypeScript parser has: the Node table
// and, with Recognize, the recognizer (tsprint.TSTarget.Sources). Its join gives what the program and
// its tables are planned from (cores), which plan! plans. planned-tables splits the plan into the
// planned tables and joins them at the Exec stage, and print-ts prints the parser of the target from
// them, which numbers the expectation table as it prints the rules.

// tsTarget returns the TypeScript target of the compilation c.
func (c *compilation) tsTarget() (*tsprint.TSTarget, error) {
	t, ok := c.target.(*tsprint.TSTarget)
	if !ok {
		return nil, fmt.Errorf("internal error: the compilation of the target %s has no TypeScript target", c.t.Backend)
	}
	return t, nil
}

// tsTables is the split ts-tables.
var tsTables = pipe.RegisterSplit(compilationIR, tableIR, corepass.IR, plandata.CoresIR, &pipe.Split[*compilation, common.TableSource, *core.Module, *plandata.Cores]{
	Name: "ts-tables", Labels: tsprint.TableLabels,
	Parts: func(c *compilation) ([]common.TableSource, []string, error) {
		t, err := c.tsTarget()
		if err != nil {
			return nil, nil, err
		}
		return t.Sources()
	},
	Join: func(c *compilation, mods []*core.Module) (*plandata.Cores, error) {
		t, err := c.tsTarget()
		if err != nil {
			return nil, err
		}
		return t.Cores(mods)
	},
})

// tsPipeline returns the pipeline of the TypeScript target of the compilation c.
func tsPipeline(c *compilation) (assembled, error) {
	if _, err := c.tsTarget(); err != nil {
		return assembled{}, err
	}
	p := pipe.Then3(
		pipe.Then(pipe.Each(tsTables, coreConfig()), pipe.Of(flowplan.Planning)),
		pipe.Each(plannedTables, lowering(tsprint.FlowTarget, tspass.Pipeline)),
		pipe.Of(tsprint.Printing))
	return assembled{pipeline: p, steps: []string{Elaborate, "each ts-tables", tsprint.Printing.Name()}}, nil
}
