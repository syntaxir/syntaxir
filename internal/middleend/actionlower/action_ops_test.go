package actionlower

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// TestActionLeavesWriteOwnComponent pins that the leaves of action expressions (addActionOps) read and write only their own
// component: what action expressions gather and make is the state of the components gathered and made, whose operations
// have reference models, and a leaf that wrote it would leave that state outside the models. Another
// writer needs a reason recorded here, as TestCrossComponentWriters of package ops records those of the
// parser VM.
func TestActionLeavesWriteOwnComponent(t *testing.T) {
	m, err := actionModule()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range m.Ops {
		if o.Component != action.Runtime {
			continue
		}
		for _, s := range slices.Concat(o.Reads, o.Writes) {
			if s != action.Runtime {
				got = append(got, o.Name+" uses "+s)
			}
		}
	}
	if len(got) != 0 {
		t.Errorf("leaves of action expressions that use another component: %q", got)
	}
}

// TestActionLeavesArePrimitives checks that the leaves of action expressions are primitive operations of the Action dialect whose immediate operands are
// typed.
func TestActionLeavesArePrimitives(t *testing.T) {
	m, err := actionModule()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range m.Ops {
		if o.Component != action.Runtime {
			continue
		}
		if o.Kind != code.OpPrimitive || o.Dialect != action.Dialect {
			t.Errorf("%s: %s of dialect %q, want a primitive op of the Action dialect", o.Name, o.Kind, o.Dialect)
		}
		if (o.Imm == "") != (o.ImmType == nil) {
			t.Errorf("%s: immediate %q with type %v", o.Name, o.Imm, o.ImmType)
		}
	}
}

// TestActionLeafImmediateChecked is the negative control of the typed immediates: a leaf whose immediate
// operand has another type fails verification.
func TestActionLeafImmediateChecked(t *testing.T) {
	for _, tc := range []struct {
		name string
		imm  any
		ok   bool
	}{
		{"read of a capture", action.Read{Kind: action.ReadCapture, Name: "x"}, true},
		{"read with a constant", action.Const{Value: 1}, false},
		{"read with no immediate", nil, false},
	} {
		m, err := actionModule()
		if err != nil {
			t.Fatal(err)
		}
		nOps, nFuncs := len(m.Ops), len(m.Funcs)
		m.AddFunc("leaf", nil, []code.Type{code.THandle(exec.Datum)}, nil, func(b *code.Builder) {
			b.Return(&code.Call{Callee: action.OpActionRead, Imm: tc.imm})
		})
		if err := code.VerifyExtension(m, nOps, nFuncs); (err == nil) != tc.ok {
			t.Errorf("%s: verify = %v, want ok %v", tc.name, err, tc.ok)
		}
	}
}
