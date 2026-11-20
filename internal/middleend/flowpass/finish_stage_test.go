package flowpass

import (
	"context"
	"testing"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/pass/pipe"
)

// TestFinishingStep checks Finish as a step of the pipeline: one required step that keeps the Flow
// stage and makes what Finish makes.
func TestFinishingStep(t *testing.T) {
	if d := Finishing.Decl(); d.Name != FinishName || !d.Required {
		t.Errorf("declaration = %+v, want the required step %s", d, FinishName)
	}
	lowered := func() *code.Module {
		m, r := forms(t)
		f, err := flowlower.Lower(m, r, "r", flowlower.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	want := lowered()
	Finish(want)
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
