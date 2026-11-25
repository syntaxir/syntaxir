package flowpass

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// TestFinishingStep checks Finish as a step of the pipeline: one required step that keeps the Flow
// stage and makes what Finish makes.
func TestFinishingStep(t *testing.T) {
	if d := Finishing.Decl(); d.Name != FinishName || !d.Required {
		t.Errorf("declaration = %+v, want the required step %s", d, FinishName)
	}
	lowered := func() *code.Module {
		m, r := forms(t)
		f, err := flowlower.Lower(m, r, "r", lowerdata.Options{Guarded: flowplan.Guarded(m, lowerdata.Options{})})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	want := lowered()
	finishModule(want, nil)
	u, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Finishing), code.At[code.Flow]{}.With(lowered()))
	if err != nil {
		t.Fatal(err)
	}
	got := u.Module()
	if flow.AttrsOf(flow.FuncOf(got)).Forms != nil {
		t.Error("the step leaves the forms")
	}
	if g, w := code.FuncStringIn(got, flow.FuncOf(got)), code.FuncStringIn(want, flow.FuncOf(want)); g != w {
		t.Errorf("the step makes\n%s\nwhere Finish makes\n%s", g, w)
	}
}
