package driver

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	tsbackend "syntaxir.dev/sxir/internal/backend/codegen/typescript"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
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
//	  ts-library!
//	  within ts-library {
//	    specialize!
//	  }
//	  print-ts!
//	}
//
// ts-tables splits the compilation into the tables that the TypeScript parser has: the Node table
// and, with Recognize, the recognizer (tsbackend.Target.Sources). Its join gives what the program and
// its tables are planned from (cores), which plan! plans. planned-tables splits the plan into the
// planned tables and joins them at the Exec stage. ts-library! sets the library of the tables to the
// compositions that the TypeScript library replaces, requested for the features of the parser, which
// specialize! specializes, and print-ts prints the parser of the target and its library from them,
// numbering the expectation table as it prints the rules.
//
// The TypeScript library alone (common.LibOnly) compiles no program; its pipeline starts at the
// library, specialized for every feature (tsbackend.LibrarySource):
//
//	with target=ts {
//	  pipeline library -> output {
//	    specialize!
//	    print-ts-lib!
//	  }
//	}

// tsTarget returns the TypeScript target of the compilation c.
func (c *compilation) tsTarget() (*tsbackend.Target, error) {
	t, ok := c.target.(*tsbackend.Target)
	if !ok {
		return nil, fmt.Errorf("internal error: the compilation of the target %s has no TypeScript target", c.t.Backend)
	}
	return t, nil
}

// tsTables is the split ts-tables.
var tsTables = pipe.RegisterSplit(compilationIR, tableIR, corepass.IR, plandata.CoresIR, &pipe.Split[*compilation, common.TableSource, *core.Module, *plandata.Cores]{
	Name: "ts-tables", Labels: tsbackend.TableLabels,
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
		pipe.Each(plannedTables, lowering(tsbackend.FlowTarget, tspass.Pipeline)),
		pipe.Then3(pipe.Of(tsbackend.Library), pipe.Within(tsbackend.LibraryLens, pipe.Of(codepass.Specialization)),
			pipe.Of(tsbackend.Printing)))
	return assembled{pipeline: p, steps: []string{Elaborate, "each ts-tables", tsbackend.Printing.Name()}}, nil
}

// libraryPipeline is the pipeline of a target from a library to its output, and the steps whose
// failure is an error of the compilation itself.
type libraryPipeline struct {
	pipeline pipe.Pipeline[*library.Library, Output]
	steps    []string
}

// error returns the failure err of a run of the pipeline, as assembled.error does.
func (a libraryPipeline) error(err error) error { return assembled{steps: a.steps}.error(err) }

// tsLibraryPipeline returns the pipeline of the TypeScript library alone for the target t.
func tsLibraryPipeline(t Target) libraryPipeline {
	target := tsbackend.NewTarget(codegen.Checked{}, nil, nil, t.Gen, nil)
	p := pipe.WithConst(codegen.TargetFeature, common.Target(target),
		pipe.Then(pipe.Of(codepass.Specialization), pipe.Of(tsbackend.LibraryPrinting)))
	return libraryPipeline{pipeline: p, steps: []string{codepass.PassSpecialize, tsbackend.LibraryPrinting.Name()}}
}
