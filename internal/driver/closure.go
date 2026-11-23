package driver

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The pipeline of a closure target:
//
//	with target=closure {
//	  each closure-tables {
//	    core-config { ... }
//	  }
//	  closure-core!
//	}
//
// closure-tables gives the one module that every family of the closures is built from, the Core IR
// of the program, built as the Node table of the VM and of a generated parser (key "program"), so a
// VM target in the same compilation shares it, and joins it into closure-input. closure-core! returns
// the module without the control of the compilation (closure.Core).

// closureTarget is a closure target as its split reads it (codegen.TargetFeature): the checked
// program whose Core module it builds, and whether that module records the origin map, as the
// compilation decides (origin).
type closureTarget struct {
	prog   *frontend.Result
	origin bool
}

// Backend returns the name of the closure backend.
func (*closureTarget) Backend() string { return result.Closure.String() }

// closureTables is the split closure-tables: the one Core module of the closure target of the scope
// it runs in, labeled as the Node table, joined into closure-input.
var closureTables = pipe.RegisterSplit(programIR, tableIR, corepass.IR, closure.InputIR, &pipe.Split[*Program, common.TableSource, *core.Module, closure.Input]{
	Name: "closure-tables", Labels: []string{vmTable}, Features: []pipe.FeatureID{codegen.TargetFeature.ID()},
	Parts: func(x pipe.SplitCtx, _ *Program) ([]common.TableSource, []string, error) {
		target := pipe.SplitFeature(x, codegen.TargetFeature)
		t, ok := target.(*closureTarget)
		if !ok {
			return nil, nil, fmt.Errorf("the split closure-tables runs for the target %s", target.Backend())
		}
		return []common.TableSource{{Build: flowplan.TableBuild(t.prog, vmTableConfig), Key: "program", Origin: t.origin}}, []string{vmTable}, nil
	},
	Join: func(_ pipe.SplitCtx, _ *Program, mods []*core.Module) (closure.Input, error) {
		if len(mods) != 1 {
			return closure.Input{}, fmt.Errorf("the closures build from one module, not %d", len(mods))
		}
		return closure.Input{Module: mods[0]}, nil
	},
})

// closurePipeline returns the pipeline of a closure target.
func closurePipeline() assembled {
	p := pipe.Then3(pipe.Each(closureTables, coreConfig()), pipe.Each(closureActions, actionSegment(vmActions)), pipe.Of(closure.Core))
	return assembled{pipeline: p, steps: []string{Elaborate, closure.CoreName}}
}
