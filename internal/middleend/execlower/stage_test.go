package execlower

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// TestLoweringStep checks the conversion as a step of the pipeline: one required step from the Flow
// stage to the Exec stage that returns the module with its functions, each a lowered function with its
// variables (View), and whose postcondition, the verifier of the Exec stage, names the step when it
// rejects the result.
func TestLoweringStep(t *testing.T) {
	if d := Lowering.Decl(); d.Name != Name || !d.Required {
		t.Errorf("declaration = %+v, want the required step %s", d, Name)
	}
	m := flowModule()
	fn := m.Funcs[0]
	fn.Attrs = &flow.Attrs{Vars: []string{"ok"}}
	u, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Lowering), flowTable(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if l := execdata.View(u.Module.Module(), 0); l.Func != fn || !slices.Equal(l.Vars, []string{"ok"}) || !slices.Contains(l.Module.Funcs, fn) || l.Module.Stage != Stage {
		t.Errorf("lowered = %+v", l)
	}
	// A call of an operation that the Flow module does not declare is not legal after the conversion.
	m = flowModule()
	m.Funcs[0].Attrs = &flow.Attrs{}
	m.Funcs[0].Body.Stmts = append(m.Funcs[0].Body.Stmts, &code.Do{Call: &code.Call{Callee: "flow.undeclared"}})
	_, err = pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Lowering), flowTable(t, m))
	var se *pipe.StepError
	if !errors.As(err, &se) || se.Step != Name || !strings.Contains(err.Error(), "call of undeclared runtime.undeclared") {
		t.Errorf("an invalid result: %v", err)
	}
}

// flowTable returns the table of no plan whose module at the Flow stage is m.
func flowTable(t *testing.T, m *code.Module) *plandata.Table[code.Flow] {
	t.Helper()
	ft, err := plandata.NewTable(nil, nil, code.At[code.Flow]{}.With(m))
	if err != nil {
		t.Fatal(err)
	}
	return ft
}

// lowered returns a lowered function with the variable ok.
func lowered(t *testing.T) *execdata.Lowered {
	t.Helper()
	m := flowModule()
	m.Funcs[0].Attrs = &flow.Attrs{Vars: []string{"ok"}}
	x, err := lower(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	return execdata.View(x, 0)
}

// TestLoweredCopies checks that a copy of a lowered function holds the copy of its function, that
// a module the passes made is taken back only if it keeps the function and its variables, and that
// neither changes the lowered function.
func TestLoweredCopies(t *testing.T) {
	l := lowered(t)
	c := l.Clone()
	if c.Module == l.Module || c.Func == l.Func || !slices.Contains(c.Module.Funcs, c.Func) || c.Func.Name != "f" {
		t.Fatalf("copy %+v", c)
	}
	if err := execdata.LoweredIR.Verify(c); err != nil {
		t.Fatal(err)
	}
	m := l.Module.Clone()
	w, err := l.WithModule(m)
	if err != nil || w.Func != m.Funcs[0] || w.Module != m || l.Module == m {
		t.Fatalf("WithModule = %+v, %v", w, err)
	}
	removed := l.Module.Clone()
	removed.Funcs = nil
	if _, err := l.WithModule(removed); err == nil || !strings.Contains(err.Error(), "replaced or removed the function f") {
		t.Errorf("a module without the function: %v", err)
	}
	dropped := l.Module.Clone()
	dropped.Funcs[0].Locals = nil
	if _, err := l.WithModule(dropped); err == nil || !strings.Contains(err.Error(), "removed the local ok") {
		t.Errorf("a function without the variable: %v", err)
	}
	c.Module.Funcs = append([]*code.Func{{Name: "g", Body: &code.Block{}}}, c.Module.Funcs[1:]...)
	if err := execdata.LoweredIR.Verify(c); err == nil || !strings.Contains(err.Error(), "replaced or removed the function f") {
		t.Errorf("the verifier of a module without the function: %v", err)
	}
	if err := execdata.LoweredIR.Verify(l); err != nil || len(l.Func.Locals) == 0 {
		t.Errorf("changing the copies changed the function: %v", err)
	}
}

// TestLowerCancelled checks that the lowering polls its control: under a context that has ended it
// unwinds with the context's error (cancel.Cancellation) within cancel.PollStride expressions,
// which the pass manager turns into the failure of the step, and without a control it lowers the
// same module.
func TestLowerCancelled(t *testing.T) {
	module := func() *code.Module { // lower takes the module, so each lowering gets its own
		m := flowModule()
		m.Funcs[0].Attrs = &flow.Attrs{Vars: []string{"ok"}}
		for range cancel.PollStride {
			m.Funcs[0].Body.Stmts = append(m.Funcs[0].Body.Stmts, &code.Do{Call: &code.Call{Callee: flow.OpAdvance, Args: []code.Expr{code.Int(1)}}})
		}
		return m
	}
	if _, err := lower(module(), nil); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	control := cancel.New(ctx)
	var err error
	func() {
		defer cancel.Recover(&err)
		_, err = lower(module(), control)
	}()
	if !errors.Is(err, context.Canceled) || control.Steps != cancel.PollStride {
		t.Errorf("a cancelled lowering = %v after %d steps, want %v after %d", err, control.Steps, context.Canceled, cancel.PollStride)
	}
}
