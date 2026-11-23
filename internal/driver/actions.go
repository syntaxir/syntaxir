package driver

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/backend/closure"
	gobackend "syntaxir.dev/sxir/internal/backend/codegen/golang"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	tsbackend "syntaxir.dev/sxir/internal/backend/codegen/typescript"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The Action segment of the pipeline of a target, which every target runs on each table before it
// plans the program:
//
//	each action-tables {
//	  with action-config {
//	    actionlower!
//	    func funcs@action { slot action-passes { the Action passes } }
//	    func funcs@action { slot action-target-passes { the target's Action passes } }
//	    the target's Action steps
//	    func funcs@action { action-finish! }
//	  }
//	}
//
// action-tables splits the input of the planning into the distinct tables of its units, labeled as
// the first unit of each, and joins them with the Action module of each (plandata.TableIR.Actions).
// actionlower lowers the action expressions of a table once, into its Action module; action-config
// says whether the table builds the values of the typed runtime (gopass.TypedFeature), which the Go
// passes read. action-finish ends each function of a site. A closure target runs the segment on its one
// table (closureActions).

// The slots of the Action segment: the Action passes that a target runs on the function of each site,
// and those of the target.
var (
	ActionSlot       = pipe.NewSlot("action-passes", code.ActionStage)
	ActionTargetSlot = pipe.NewSlot("action-target-passes", code.ActionStage)
)

// actionConfig derives the features of a table that the Action passes read from its unit: whether its
// actions build the values of the typed runtime, which only the typed table of a Go parser does: every
// other table builds those of the Node runtime (lowerdata.Options.Node) or none.
var actionConfig = pipe.RegisterDerive(plandata.TableCoreIR, &pipe.Derive[*plandata.TableIR, bool]{
	Name: "action-config", Key: gopass.TypedFeature,
	Of: func(t *plandata.TableIR) bool { return !t.Opts.Node },
})

// actionTarget is what a target runs in the Action segment: its Action passes, those of the target
// on each function of a site, and its Action steps on the module.
type actionTarget struct {
	passes, target []*pipe.Pass[code.At[code.Action], code.At[code.Action]]
	steps          []*pipe.Pass[code.At[code.Action], code.At[code.Action]]
}

// The Action segments of the targets. Go and TypeScript print the text dispatch of predicates; Go
// compares fields and constructs structs in the typed table and prints the actions of flat rules in
// place. The bytecode VM and the closures run the functions as the lowering made them.
var (
	goActions = actionTarget{
		passes: []*pipe.Pass[code.At[code.Action], code.At[code.Action]]{actionpass.TextDispatch},
		target: []*pipe.Pass[code.At[code.Action], code.At[code.Action]]{gopass.FieldCompare, gopass.StructCall},
		steps:  []*pipe.Pass[code.At[code.Action], code.At[code.Action]]{gopass.InPlace},
	}
	tsActions = actionTarget{passes: []*pipe.Pass[code.At[code.Action], code.At[code.Action]]{actionpass.TextDispatch}}
	vmActions = actionTarget{}
)

// actionSegment returns the Action segment of a table for the target t.
func actionSegment(t actionTarget) pipe.Pipeline[*plandata.TableIR, code.At[code.Action]] {
	module := pipe.Then3(
		pipe.PerFunc(code.ActionFuncs, pipe.Slot(ActionSlot, pipe.Seq(code.ActionStage, t.passes...))),
		pipe.PerFunc(code.ActionFuncs, pipe.Slot(ActionTargetSlot, pipe.Seq(code.ActionStage, t.target...))),
		pipe.Then(pipe.Seq(code.ActionStage, t.steps...), pipe.PerFunc(code.ActionFuncs, pipe.Seq(code.ActionStage, actionpass.Finishing))))
	return pipe.WithDerived(actionConfig, pipe.Then(pipe.Of(actionlower.Lowering), module))
}

// actions returns the Action segment of the target t on the input of the planning.
func actions(t actionTarget) pipe.Pipeline[*plandata.Cores, *plandata.Cores] {
	return pipe.Each(actionTables, actionSegment(t))
}

// actionTables is the split action-tables: the distinct tables of the units of the input of the
// planning, in the order of their first units, joined with their Action modules into a copy of the
// input whose units share the tables as before.
var actionTables = pipe.RegisterSplit(plandata.CoresIR, plandata.TableCoreIR, code.ActionStage, plandata.CoresIR, &pipe.Split[*plandata.Cores, *plandata.TableIR, code.At[code.Action], *plandata.Cores]{
	Name: "action-tables", Labels: actionLabels(),
	Parts: func(_ pipe.SplitCtx, c *plandata.Cores) ([]*plandata.TableIR, []string, error) {
		tables, labels := distinctTables(c)
		return tables, labels, nil
	},
	Join: func(_ pipe.SplitCtx, c *plandata.Cores, mods []code.At[code.Action]) (*plandata.Cores, error) {
		tables, _ := distinctTables(c)
		if len(mods) != len(tables) {
			return nil, fmt.Errorf("internal error: %d Action modules for %d tables", len(mods), len(tables))
		}
		with := map[*plandata.TableIR]*plandata.TableIR{}
		for i, t := range tables {
			x := *t
			x.Actions = mods[i]
			with[t] = &x
		}
		out := *c
		out.Units = slices.Clone(c.Units)
		for i, u := range out.Units {
			out.Units[i].IR = with[u.IR]
		}
		return &out, nil
	},
})

// distinctTables returns the distinct tables of the units of c, in the order of their first units,
// with the labels of those units.
func distinctTables(c *plandata.Cores) ([]*plandata.TableIR, []string) {
	var tables []*plandata.TableIR
	var labels []string
	for _, u := range c.Units {
		if !slices.Contains(tables, u.IR) {
			tables, labels = append(tables, u.IR), append(labels, u.Label)
		}
	}
	return tables, labels
}

// actionLabels returns the labels of the tables of every backend that plans its program.
func actionLabels() []string {
	var out []string
	for _, l := range [][]string{gobackend.TableLabels, tsbackend.TableLabels, {vmTable}} {
		for _, n := range l {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// closureActions is the split closure-actions: the one table of a closure target, with the options of
// the VM's table and labeled as its Node table, whose Action module the join sets on the input of the
// closures.
var closureActions = pipe.RegisterSplit(closure.InputIR, plandata.TableCoreIR, code.ActionStage, closure.InputIR,
	&pipe.Split[closure.Input, *plandata.TableIR, code.At[code.Action], closure.Input]{
		Name: "closure-actions", Labels: []string{vmTable},
		Parts: func(_ pipe.SplitCtx, in closure.Input) ([]*plandata.TableIR, []string, error) {
			return []*plandata.TableIR{flowplan.TableOf(in.Module, vmTableConfig)}, []string{vmTable}, nil
		},
		Join: func(_ pipe.SplitCtx, in closure.Input, mods []code.At[code.Action]) (closure.Input, error) {
			if len(mods) != 1 {
				return closure.Input{}, fmt.Errorf("internal error: %d Action modules for the one table of the closures", len(mods))
			}
			in.Actions = mods[0]
			return in, nil
		},
	})
