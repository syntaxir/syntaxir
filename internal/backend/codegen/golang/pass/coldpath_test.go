package pass

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// coldModule returns a module with the function f named name, which returns early when pos is
// negative and otherwise sets the local n and calls the operation o.
func coldModule(name string, features []string, body func(b *code.Builder)) (*code.Module, *code.Func) {
	m := code.NewModule("t", exec.Budget)
	m.AddComponent("c", "")
	m.AddOp(&code.Op{Name: "o", Component: "c", Params: []code.Param{code.P("n", code.TInt)}, Contract: "Does nothing.", Kind: code.OpPrimitive})
	f := m.AddFunc(name, []code.Param{code.P("pos", code.TInt)}, nil, features, body)
	f.Locals = []code.Param{code.P("n", code.TInt)}
	return m, f
}

func runCold(t *testing.T, m *code.Module, f *code.Func) (*execlower.Lowered, error) {
	t.Helper()
	return pipe.Execute(context.Background(), &pipe.Run{VerifyEach: true}, Library, &execlower.Lowered{Module: m, Func: f})
}

func TestColdPathSplitsTheGuard(t *testing.T) {
	m, f := coldModule("expect", nil, func(b *code.Builder) {
		b.If(code.Lt(code.L("pos"), code.Int(0)), func(b *code.Builder) { b.Return() })
		b.Set("n", code.L("pos"))
		b.Do("o", code.L("n"))
	})
	l, err := runCold(t, m, f)
	if err != nil {
		t.Fatal(err)
	}
	if l.Func.Name != "expect" || len(l.Func.Locals) != 0 || l.Func.NoInline {
		t.Errorf("guard %s locals %v noinline %v", l.Func.Name, l.Func.Locals, l.Func.NoInline)
	}
	stmts := l.Func.Body.Stmts
	if len(stmts) != 2 {
		t.Fatalf("the guard has %d statements, want the guard and the call", len(stmts))
	}
	call, ok := stmts[1].(*code.Do)
	if !ok || call.Call.Callee != "expectSlow" || len(call.Call.Args) != 1 {
		t.Fatalf("the guard ends with %#v, want a call of expectSlow with pos", stmts[1])
	}
	slow := l.Module.Func("expectSlow")
	if slow == nil || !slow.NoInline || len(slow.Locals) != 1 || len(slow.Params) != 1 || len(slow.Body.Stmts) != 2 {
		t.Fatalf("cold function %#v", slow)
	}
	if slow.Derived == nil || slow.Derived.Pass != PassColdPath || slow.Derived.From != "expect" {
		t.Errorf("derived %+v", slow.Derived)
	}
	if !strings.HasPrefix(slow.Doc, "expectSlow ") {
		t.Errorf("doc %q does not start with the name", slow.Doc)
	}
	if m.Func("expectSlow") != nil || len(f.Body.Stmts) != 3 {
		t.Errorf("the input module changed")
	}
}

func TestColdPathLeavesOtherForms(t *testing.T) {
	// A function that is not listed, and the form of a listed one with the budget, stay as they are.
	for _, c := range []struct {
		name     string
		features []string
	}{{"other", nil}, {"expect", []string{exec.Budget}}} {
		m, f := coldModule(c.name, c.features, func(b *code.Builder) {
			b.If(code.Lt(code.L("pos"), code.Int(0)), func(b *code.Builder) { b.Return() })
			b.Do("o", code.L("pos"))
		})
		l, err := runCold(t, m, f)
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Module.Funcs) != 1 || len(l.Func.Body.Stmts) != 2 {
			t.Errorf("%s %v was split", c.name, c.features)
		}
	}
}

func TestColdPathRejectsWhatItCannotSplit(t *testing.T) {
	for name, body := range map[string]func(b *code.Builder){
		"no guard": func(b *code.Builder) { b.Do("o", code.L("pos")) },
		"only guards": func(b *code.Builder) {
			b.If(code.Lt(code.L("pos"), code.Int(0)), func(b *code.Builder) { b.Return() })
		},
		"a guard that reads a local": func(b *code.Builder) {
			b.If(code.Lt(code.L("n"), code.Int(0)), func(b *code.Builder) { b.Return() })
			b.Do("o", code.L("pos"))
		},
	} {
		m, f := coldModule("expect", nil, body)
		if _, err := runCold(t, m, f); err == nil || !strings.Contains(err.Error(), "go cold path") {
			t.Errorf("%s: error %v", name, err)
		}
	}
}
