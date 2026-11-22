package flowlower

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// finish runs on the lowered module f the Flow passes of the target, with inline as the number of
// code points of the inline-literals pass, and ends them, as the driver does after the lowering.
func finish(t *testing.T, f *code.Module, target flowpass.Target, inline int) *code.Module {
	t.Helper()
	settings := with(t, with(t, pass.Settings{}, flowpass.PassClassTables, 0), flowpass.PassInlineLiterals, inline)
	f, _, err := flowpass.Run(f, flowpass.RunOptions{Settings: settings, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	return finishFlow(t, f)
}

// finishFlow ends the Flow passes on the module f with the step that ends them in the pipeline
// (flowpass.Finishing), and returns the module.
func finishFlow(t *testing.T, f *code.Module) *code.Module {
	t.Helper()
	u, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Of(flowpass.Finishing), code.At[code.Flow]{}.With(f))
	if err != nil {
		t.Fatal(err)
	}
	return u.Module()
}

// lowerFinished lowers the rule r of m and ends the passes, with the in-place literals of at most
// inline code points when inline is positive.
func lowerFinished(t *testing.T, m *core.Module, r *core.Rule, opts lowerdata.Options, inline int) (*code.Module, error) {
	t.Helper()
	f, err := Lower(m, r, "r", opts)
	if err != nil {
		return nil, err
	}
	return finish(t, f, flowpass.Target{InlineLiterals: inline > 0}, inline), nil
}

// with returns s with the optimization name set to v.
func with(t *testing.T, s pass.Settings, name string, v int) pass.Settings {
	t.Helper()
	s, err := s.With(name, v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
