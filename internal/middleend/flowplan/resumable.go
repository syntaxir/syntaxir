package flowplan

import (
	"fmt"
	"slices"
	"strconv"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// SuspendFeature reports that the table is lowered for a suspended stream (lowerdata.Options.Suspend): its
// functions demand their input, and the table has resumable functions (Resumable).
var SuspendFeature = pipe.NewFeature("suspend", strconv.FormatBool, strconv.ParseBool)

// PassResumable is the name of the pass Resumable.
const PassResumable = "resumable"

// Resumable marks the resumable functions of a table lowered for a suspended stream (SuspendFeature):
// a function is resumable when it demands input (flow.inputDemand) or calls a resumable function of the table,
// a rule through its body and an outlined node through its outlined function (Plan.Outlined). The functions
// get the flag as flow.Attrs.Resumable, which the TypeScript printer reads to print them as generators. The pass
// runs right after the lowering, before the Flow passes, so that it sees the demands the lowering emitted.
var Resumable = pipe.New(pipe.Def[*plandata.Table[code.Flow], *plandata.Table[code.Flow]]{
	Decl: pipe.Decl{Name: PassResumable, Required: true, Obligation: "A function is resumable exactly when it demands input or calls a resumable function of its table.",
		Features: []pipe.FeatureID{SuspendFeature.ID()}},
	In: plandata.FlowTable, Out: plandata.FlowTable,
	// The pass is required, so it runs whenever the table is lowered, and marks nothing for a table that is not
	// suspended: a required pass has no flag and no Applies.
	Run: func(c *pipe.Ctx[*plandata.Table[code.Flow]], t *plandata.Table[code.Flow]) (*plandata.Table[code.Flow], pass.PreservedSet, error) {
		if !pipe.Feature(c, SuspendFeature) {
			return t, pass.PreserveAll(), nil
		}
		if err := markResumable(t); err != nil {
			return nil, pass.PreserveNone(), err
		}
		return t, pass.PreserveNone(), nil
	},
})

// markResumable sets flow.Attrs.Resumable on each function of t by the fixpoint of its calls: the functions
// are marked by their demands, and a function that calls a marked one is marked in turn.
func markResumable(t *plandata.Table[code.Flow]) error {
	fs := t.Module.Module().Funcs
	seed := make([]bool, len(fs))
	callees := make([][]int, len(fs))
	for i, fn := range fs {
		a := flow.AttrsOf(fn)
		if a == nil {
			return fmt.Errorf("internal error: function %s of a suspended table has no Flow attributes", fn.Name)
		}
		var err error
		code.Walk(fn.Body, nil, func(e code.Expr) {
			c, ok := e.(*code.Call)
			if !ok || err != nil {
				return
			}
			var k plandata.Key
			switch c.Callee {
			case flow.OpInputDemand:
				seed[i] = true
				return
			case flow.OpCallRule:
				k = plandata.Key{Use: plandata.RuleBody, Rule: c.Imm.(prim.RuleCall).Rule}
			case flow.OpCallOutline:
				outlined := c.Imm.(prim.Outline).Call
				var ok bool
				if k, ok = t.Plan.Outlined(outlined); !ok {
					err = fmt.Errorf("internal error: outlined call %d of %s has no function of the plan", outlined, fn.Name)
					return
				}
			default:
				return
			}
			j, ok := t.Index(k)
			if !ok {
				err = fmt.Errorf("internal error: a call in %s has no function of the plan (%v)", fn.Name, k)
				return
			}
			callees[i] = append(callees[i], j)
		})
		if err != nil {
			return err
		}
	}
	resumable := slices.Clone(seed)
	for changed := true; changed; {
		changed = false
		for i := range fs {
			if resumable[i] {
				continue
			}
			for _, j := range callees[i] {
				if resumable[j] {
					resumable[i], changed = true, true
					break
				}
			}
		}
	}
	for i, fn := range fs {
		flow.AttrsOf(fn).Resumable = resumable[i]
	}
	return nil
}
