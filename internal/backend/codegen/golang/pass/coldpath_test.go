package pass

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// coldModule returns a module with the function f named name, which returns early when pos is
// negative and otherwise sets the local n and calls the operation o.
func coldModule(name string, features []string, body func(b *code.Builder)) (*code.Module, *code.Func) {
	m := code.NewModule("t", exec.Units)
	m.AddComponent("c", "")
	m.AddOp(&code.Op{Name: "o", Component: "c", Params: []code.Param{code.P("n", code.TInt)}, Contract: "Does nothing.", Kind: code.OpPrimitive})
	f := m.AddFunc(name, []code.Param{code.P("pos", code.TInt)}, nil, features, body)
	f.Locals = []code.Param{code.P("n", code.TInt)}
	return m, f
}

// runCold runs the step go-cold-path on a library of m whose one request is the function f of the
// module, as specialized, printed into the file a.go, which asks for the cold function expectSlow.
func runCold(t *testing.T, m *code.Module, f *code.Func) (*library.Library, error) {
	t.Helper()
	return runColdRequest(t, m, f, "expectSlow")
}

// runColdRequest is runCold with the request asking for the cold function cold, or for none if cold
// is empty.
func runColdRequest(t *testing.T, m *code.Module, f *code.Func, cold string) (*library.Library, error) {
	t.Helper()
	l := &library.Library{Module: m, Requests: []library.Request{{File: "a.go", Fn: f.Name, On: f.Features, Name: f.Name, Cold: cold}}, Funcs: []*code.Func{f}}
	return pipe.Execute(context.Background(), &pipe.Run{VerifyEach: true}, pipe.Of(ColdPath), l)
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
	guard := l.Funcs[0]
	if guard.Name != "expect" || len(guard.Locals) != 0 || guard.NoInline {
		t.Errorf("guard %s locals %v noinline %v", guard.Name, guard.Locals, guard.NoInline)
	}
	stmts := guard.Body.Stmts
	if len(stmts) != 2 {
		t.Fatalf("the guard has %d statements, want the guard and the call", len(stmts))
	}
	call, ok := stmts[1].(*code.Do)
	if !ok || call.Call.Callee != "expectSlow" || len(call.Call.Args) != 1 {
		t.Fatalf("the guard ends with %#v, want a call of expectSlow with pos", stmts[1])
	}
	if len(l.Made) != 1 || l.Made[0].Request != 0 {
		t.Fatalf("made %+v, want the cold function of the request", l.Made)
	}
	slow := l.Made[0].Func
	if slow.Name != "expectSlow" || !slow.NoInline || len(slow.Locals) != 1 || len(slow.Params) != 1 || len(slow.Body.Stmts) != 2 {
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
	// A request that asks for no cold function stays as it is, such as the engine's expect, which
	// generated code does not inline; every request that asks for one is split under that name,
	// whatever its composition, and the form that counts compute units among them, since no form
	// charges inside the record.
	for _, c := range []struct {
		name     string
		features []string
		cold     string
		split    bool
	}{{"other", nil, "", false}, {"other", nil, "otherCold", true}, {"expect", []string{exec.Units}, "expectSlow", true}, {"expect", []string{exec.Units}, "", false}} {
		m, f := coldModule(c.name, c.features, func(b *code.Builder) {
			b.If(code.Lt(code.L("pos"), code.Int(0)), func(b *code.Builder) { b.Return() })
			b.Do("o", code.L("pos"))
		})
		l, err := runColdRequest(t, m, f, c.cold)
		if err != nil {
			t.Fatal(err)
		}
		if split := len(l.Made) != 0 || len(l.Funcs[0].Body.Stmts) != 2; split != c.split {
			t.Errorf("%s %v cold %q: split %v, want %v", c.name, c.features, c.cold, split, c.split)
		}
		if c.split && (len(l.Made) != 1 || l.Made[0].Func.Name != c.cold) {
			t.Errorf("%s %v: made %+v, want the cold function %s", c.name, c.features, l.Made, c.cold)
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

// A cold function whose name a composition of the module or a function made for the same file has
// is refused, and the requests of other files may make one of the same name.
func TestColdPathKeepsTheNamesApart(t *testing.T) {
	split := func(b *code.Builder) {
		b.If(code.Lt(code.L("pos"), code.Int(0)), func(b *code.Builder) { b.Return() })
		b.Do("o", code.L("pos"))
	}
	m, f := coldModule("expect", nil, split)
	m.AddFunc("expectSlow", nil, nil, nil, func(b *code.Builder) {})
	if _, err := runCold(t, m, f); err == nil || !strings.Contains(err.Error(), "declares expectSlow already") {
		t.Errorf("a composition named expectSlow: %v", err)
	}
	m, f = coldModule("expect", nil, split)
	two := func(files ...string) (*library.Library, error) {
		g := *f
		g.Name = "expect2"
		l := &library.Library{Module: m, Requests: []library.Request{{File: files[0], Fn: "expect", Name: "expect", Cold: "expectSlow"}, {File: files[1], Fn: "expect", Name: "expect2", Cold: "expectSlow"}}, Funcs: []*code.Func{f, &g}}
		return pipe.Execute(context.Background(), &pipe.Run{VerifyEach: true}, pipe.Of(ColdPath), l)
	}
	if _, err := two("a.go", "a.go"); err == nil || !strings.Contains(err.Error(), "declares expectSlow already") {
		t.Errorf("two cold functions for one file: %v", err)
	}
	l, err := two("a.go", "b.go")
	if err != nil || len(l.Made) != 2 || l.Made[1].Request != 1 {
		t.Errorf("two files: %+v, %v", l, err)
	}
}
