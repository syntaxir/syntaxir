package codepass

import (
	"context"
	"slices"
	"strings"
	"testing"

	. "syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// specializeLibrary runs the pass specialize on a library of m with the requests rs, verifying each
// step.
func specializeLibrary(m *Module, rs []library.Request) (*library.Library, error) {
	return pipe.Execute(context.Background(), &pipe.Run{VerifyEach: true}, pipe.Of(Specialization),
		&library.Library{Module: m, Requests: rs})
}

// Two requests of one composition under different features keep two functions of one name, for two
// files, beside the module, which does not change, and a later compilation with the same requests
// gets its own copies.
func TestSpecializationKeepsOneFunctionPerRequest(t *testing.T) {
	m := module()
	m.AddFunc("f", nil, nil, []string{"budget"}, func(b *Builder) {
		b.If(F("budget"), func(b *Builder) { b.Do("log.charge") })
		b.Do("log.note", Int(1))
	})
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	before := FuncString(m.Funcs[0])
	rs := []library.Request{{File: "a.go", Fn: "f", On: []string{"budget"}, Name: "f"}, {File: "b.go", Fn: "f", Name: "f"}}
	l, err := specializeLibrary(m, rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Funcs) != 2 || len(m.Funcs) != 1 {
		t.Fatalf("the library holds %d functions and the module %d; want 2 and 1", len(l.Funcs), len(m.Funcs))
	}
	charged := strings.Contains(FuncString(l.Funcs[0]), "log.charge")
	plain := strings.Contains(FuncString(l.Funcs[1]), "log.charge")
	if !charged || plain || !slices.Equal(l.Funcs[0].Features, []string{"budget"}) || len(l.Funcs[1].Features) != 0 {
		t.Errorf("the specializations do not follow their requests:\n%s\n%s", FuncString(l.Funcs[0]), FuncString(l.Funcs[1]))
	}
	if after := FuncString(m.Funcs[0]); after != before {
		t.Errorf("the composition changed:\n%s\nwas:\n%s", after, before)
	}
	// A change of one compilation's copy does not reach the next compilation's.
	l.Funcs[0].Body.Stmts = nil
	again, err := specializeLibrary(m, rs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(FuncString(again.Funcs[0]), "log.charge") {
		t.Errorf("the second compilation's copy shares the first's:\n%s", FuncString(again.Funcs[0]))
	}
}

// A request of a composition or a feature that the module does not declare fails.
func TestSpecializationRejectsUndeclaredRequests(t *testing.T) {
	m := module()
	m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Do("log.note", Int(1)) })
	for _, r := range []library.Request{{Fn: "g", Name: "g"}, {Fn: "f", On: []string{"nope"}, Name: "f"}} {
		if _, err := specializeLibrary(m, []library.Request{r}); err == nil {
			t.Errorf("the request %+v was specialized", r)
		}
	}
}
