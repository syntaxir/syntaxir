package actionlower

import (
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/core"
)

// TestLoweredActionHasTheSignatureOfAction holds the definition of a lowered action expression to the callable type Action, which
// is what an application of an action expression runs: the lowered function takes the context and returns the value and the
// failure. The negative controls change the function by one parameter or result and must be rejected.
func TestLoweredActionHasTheSignatureOfAction(t *testing.T) {
	l, err := Lower(&core.Const{Value: 1}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkActionSignature(l.Module, l.Func); err != nil {
		t.Fatalf("the lowered action expression does not have the signature of Action: %v", err)
	}
	if len(l.Func.Params) != 1 || l.Func.Params[0].Name != CtxParam {
		t.Fatalf("params = %v, want the context %s", l.Func.Params, CtxParam)
	}
	act, datum, fail := code.THandle(action.Act), code.THandle(exec.Datum), code.THandle(action.Fail)
	for _, c := range []struct {
		name    string
		params  []code.Param
		results []code.Type
	}{
		{"no context", nil, []code.Type{datum, fail}},
		{"a second parameter", []code.Param{code.P("c", act), code.P("x", datum)}, []code.Type{datum, fail}},
		{"a parameter of another type", []code.Param{code.P("c", datum)}, []code.Type{datum, fail}},
		{"no failure", []code.Param{code.P("c", act)}, []code.Type{datum}},
		{"a third result", []code.Param{code.P("c", act)}, []code.Type{datum, fail, fail}},
		{"results in the other order", []code.Param{code.P("c", act)}, []code.Type{fail, datum}},
	} {
		f := &code.Func{Name: "action", Params: c.params, Results: c.results}
		err := checkActionSignature(l.Module, f)
		if err == nil || !strings.Contains(err.Error(), "signature of Action") {
			t.Errorf("%s: err = %v, want the signature of Action to be reported", c.name, err)
		}
	}
}

// TestActionChecksItsSignature goes through Lower itself: when the callable type Action declares another signature than
// the function that Lower lowers, lowering fails, so a lowering that dropped the check would pass for any change to
// the signature. The test changes the handle type of the shared base module of action expressions and restores it.
func TestActionChecksItsSignature(t *testing.T) {
	m, err := actionModule()
	if err != nil {
		t.Fatal(err)
	}
	h := m.HandleType(action.Action)
	saved := h.Callable
	defer func() { h.Callable = saved }()
	for _, c := range []struct {
		name string
		sig  code.Signature
	}{
		{"a second parameter", code.Signature{Params: append(slices.Clone(saved.Params), saved.Params[0]), Results: saved.Results}},
		{"one result", code.Signature{Params: saved.Params, Results: saved.Results[:1]}},
		{"no parameter", code.Signature{Results: saved.Results}},
	} {
		h.Callable = &c.sig
		_, err := Lower(&core.Const{Value: 1}, Options{})
		if err == nil || !strings.Contains(err.Error(), "signature of Action") {
			t.Errorf("%s: err = %v, want the signature of Action to be reported", c.name, err)
		}
	}
	h.Callable = saved
	if _, err := Lower(&core.Const{Value: 1}, Options{}); err != nil {
		t.Errorf("with the declared signature: %v", err)
	}
}
