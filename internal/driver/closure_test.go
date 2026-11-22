package driver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// closure-core returns the module of its input without the control of the compilation, and leaves
// the input, which may be the kept result of a shared Core part, as it was.
func TestClosureCoreKeepsItsInput(t *testing.T) {
	t.Parallel()
	control := &cancel.Control{Ctx: context.Background()}
	rules := []*core.Rule{{Name: "a"}}
	in := &core.Module{Rules: rules, Control: control}
	out, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(closure.Core), closure.Input{Module: in})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(*closure.Module).Core
	if m == in || m.Control != nil || len(m.Rules) != 1 || m.Rules[0] != rules[0] {
		t.Errorf("closure-core returned %p with control %v and rules %v, from %p", m, m.Control, m.Rules, in)
	}
	if in.Control != control {
		t.Error("closure-core changed its input")
	}
}

// A VM target and a closure target in one compilation build their Core module once (key "program"),
// and each output equals the one that the target gives alone.
func TestCompileClosureSharesTheCoreOfTheVM(t *testing.T) {
	t.Parallel()
	p, err := checkSource(t, "@start\nrule A = (\"a\" B / \"c\")+\nrule B = \"b\"* &\"a\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outputs := func(outs []Output) (bytecode, coreIR string) {
		for _, out := range outs {
			switch out := out.(type) {
			case *vmlower.Bytecode:
				bytecode = out.Module.Disassemble()
			case *closure.Module:
				coreIR = core.Print(out.Core, true)
			}
		}
		return bytecode, coreIR
	}
	vmOuts, err := Compile(context.Background(), p, Options{Settings: speed}, Target{Backend: result.VM})
	if err != nil {
		t.Fatal(err)
	}
	closureOuts, err := Compile(context.Background(), p, Options{Settings: speed}, Target{Backend: result.Closure})
	if err != nil {
		t.Fatal(err)
	}
	wantBytecode, _ := outputs(vmOuts)
	_, wantCore := outputs(closureOuts)
	var steps []string
	both, err := Compile(context.Background(), p, Options{Settings: speed, Observers: []pipe.Observer{&stepNames{seen: &steps}}},
		Target{Backend: result.VM}, Target{Backend: result.Closure})
	if err != nil {
		t.Fatal(err)
	}
	gotBytecode, gotCore := outputs(both)
	if gotBytecode == "" || gotBytecode != wantBytecode {
		t.Errorf("the bytecode beside the closures is\n%s\nwant\n%s", gotBytecode, wantBytecode)
	}
	if gotCore == "" || gotCore != wantCore {
		t.Errorf("the Core module beside the VM is\n%s\nwant\n%s", gotCore, wantCore)
	}
	elaborated := 0
	for _, s := range steps {
		if strings.HasPrefix(s, Elaborate+" ") {
			elaborated++
		}
	}
	if elaborated != 1 {
		t.Errorf("the compilation elaborated %d times, want once: %q", elaborated, steps)
	}
}

// A compilation whose context has ended fails at the step that finds it, with the cancellation of the
// context (cancel.Cancellation), not as an internal error.
func TestCompileCancelledNamesTheStep(t *testing.T) {
	t.Parallel()
	p, err := checkSource(t, "@start\nrule A = \"a\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	for _, backend := range []result.Backend{result.Closure, result.VM} {
		_, err := Compile(ctx, p, Options{Settings: speed}, Target{Backend: backend})
		var se *pass.StepError
		var c cancel.Cancellation
		if !errors.As(err, &se) || !errors.As(err, &c) || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "internal error") {
			t.Errorf("%s: err = %v, want the cancellation of a step", backend, err)
		}
	}
}
