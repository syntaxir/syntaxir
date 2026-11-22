package library

import (
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
)

// testLibrary returns a specialized library of the composition f, requested as f for the file a.go
// and as f for the file b.go.
func testLibrary() *Library {
	m := code.NewModule("test", "budget")
	f := m.AddFunc("f", nil, nil, []string{"budget"}, func(b *code.Builder) {})
	named := func(name string) *code.Func {
		g := code.CloneFunc(f)
		g.Name = name
		return g
	}
	return &Library{
		Module:   m,
		Requests: []Request{{File: "a.go", Fn: "f", On: []string{"budget"}, Name: "f", Cold: "fSlow"}, {File: "b.go", Fn: "f", Name: "f"}},
		Funcs:    []*code.Func{named("f"), named("f")},
		// A function of one name made for each file.
		Made: []Made{{Request: 0, Func: named("fSlow")}, {Request: 1, Func: named("fSlow")}},
	}
}

// One name requested and made for two files verifies, and every break of the rules of requests and
// made functions fails, naming what breaks.
func TestVerify(t *testing.T) {
	if err := verify(testLibrary()); err != nil {
		t.Fatalf("the library does not verify: %v", err)
	}
	for _, c := range []struct {
		name   string
		change func(l *Library)
		want   string
	}{
		{"one name twice in a file", func(l *Library) { l.Requests[1].File = "a.go"; l.Made = nil },
			`the library requests f twice for the file "a.go"`},
		{"cold function under a composition's name", func(l *Library) { l.Requests[0].Cold = "f" },
			"the library requests f with the cold function f, which has the name of a composition"},
		{"cold function under a request's name", func(l *Library) {
			l.Requests[1].File, l.Requests[1].Name, l.Requests[0].Cold = "a.go", "g", "g"
			l.Funcs[1].Name, l.Made = "g", nil
		}, `the library requests f with the cold function g, which another function of the file "a.go" has`},
		{"one cold function twice in a file", func(l *Library) {
			l.Requests[1].File, l.Requests[1].Name, l.Requests[1].Cold = "a.go", "g", "fSlow"
			l.Funcs[1].Name, l.Made = "g", nil
		}, `the library requests g with the cold function fSlow, which another function of the file "a.go" has`},
		{"made before specialize", func(l *Library) { l.Funcs = nil },
			"the library holds 2 functions made of no specialized function"},
		{"made of no request", func(l *Library) { l.Made[0].Request = 2 },
			"a function made for the library belongs to no request (2)"},
		{"made under a composition's name", func(l *Library) { l.Made[0].Func.Name = "f" },
			"the function f made of the request f has the name of a composition"},
		{"made twice for a file", func(l *Library) { l.Made[1].Request = 0 },
			`the library holds two functions fSlow made for the file "a.go"`},
	} {
		l := testLibrary()
		c.change(l)
		if err := verify(l); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

// A copy equals its original and shares no request or function with it.
func TestClone(t *testing.T) {
	l := testLibrary()
	c := clone(l)
	if !reflect.DeepEqual(c, l) {
		t.Errorf("the copy differs from the original: %+v", c)
	}
	c.Requests[0].File, c.Requests[0].On[0] = "c.go", "typed"
	c.Funcs[0].Name, c.Made[0].Func.Name = "g", "gSlow"
	c.Made[0].Request = 1
	if l.Requests[0].File != "a.go" || l.Requests[0].On[0] != "budget" || l.Funcs[0].Name != "f" ||
		l.Made[0].Func.Name != "fSlow" || l.Made[0].Request != 0 {
		t.Errorf("a change of the copy reached the original: %+v", l)
	}
}
