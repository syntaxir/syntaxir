package flowlower

import (
	"context"
	"errors"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/flowlower/lowerdata"
	"zeroparse.dev/zp/internal/middleend/flowplan/plandata"
	"zeroparse.dev/zp/internal/pass/pipe"
)

// TestLoweringStep checks the lowering as a step of the pipeline: one required step from a planned
// table to the table at the Flow stage, whose module holds the function that Lower makes, and whose
// failure names it.
func TestLoweringStep(t *testing.T) {
	if d := Lowering.Decl(); d.Name != Name || !d.Required {
		t.Errorf("declaration = %+v, want the required step %s", d, Name)
	}
	m, r := valueFree(alts(&core.Lit{Text: "ab"}, &core.Lit{Text: "c"}))
	old, err := Lower(m, r, "r", lowerdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Lowering), planned(m, lowerdata.Func{Form: lowerdata.Flat, Rule: r, Name: "r"}))
	if err != nil {
		t.Fatal(err)
	}
	got := u.Module.Module()
	if got.Stage != flow.Stage || code.FuncStringIn(got, got.Funcs[0]) != code.FuncStringIn(old, old.Funcs[0]) {
		t.Errorf("the step lowers, at the stage %q,\n%s\nwhere Lower lowers\n%s", got.Stage, code.FuncStringIn(got, got.Funcs[0]), code.FuncStringIn(old, old.Funcs[0]))
	}
	// A unit that the lowering refuses: a local function of a left-recursion leader.
	r.Policy.Leader = true
	_, err = pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Lowering), planned(m, lowerdata.Func{Form: lowerdata.Flat, Rule: r, Name: "r"}))
	var se *pipe.StepError
	if !errors.As(err, &se) || se.Step != Name || !strings.Contains(err.Error(), "is not lowered to a local function") {
		t.Errorf("a refused unit: %v", err)
	}
}

// planned returns the planned table of the functions fs of the Core module m, keyed by their index.
func planned(m *core.Module, fs ...lowerdata.Func) plandata.Planned {
	p := &plandata.Plan{}
	for i, f := range fs {
		p.Add(&plandata.FuncSpec{Key: plandata.Key{Line: i}, Func: f})
	}
	return plandata.Planned{Plan: p, IR: &plandata.TableIR{M: m}}
}
