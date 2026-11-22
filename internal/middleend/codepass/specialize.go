package codepass

import (
	"fmt"
	"strings"
	"sync"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PassSpecialize is the name of the pass specialize.
const PassSpecialize = "specialize"

// Specialization is the pass specialize of a library (design record 074): it sets the library's
// functions to one specialized copy of the composition of each request (specialize), in request order.
// The compositions module does not change.
var Specialization = pipe.New(pipe.Def[*library.Library, *library.Library]{
	Decl: pipe.Decl{Name: PassSpecialize, Required: true,
		Obligation: "Each function of the library is the composition of its request specialized for the request's features."},
	In: library.IR, Out: library.IR,
	Run: func(_ *pipe.Ctx[*library.Library], l *library.Library) (*library.Library, pass.PreservedSet, error) {
		fs, err := specialized(l.Module, l.Requests)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		out := *l
		out.Funcs = fs
		return &out, pass.PreserveNone(), nil
	},
})

// specializations memoizes the specialized functions of the most recent module and request lists,
// at most maxSpecializations of them, the oldest dropped first: a specialization is a function of the
// composition and the features alone, and no pass changes a compositions module (library.Library), so
// the module's identity and the requests key it. Each compilation of a TypeScript parser specializes
// the one compositions module of the TypeScript backend for the parser's features, of which there are
// few sets, so the memo spares a long-running host (the playground, a language server) the same work
// for every parser, and its bound keeps the memo from holding every module that a host has
// specialized.
var specializations struct {
	mu    sync.Mutex
	keys  []specKey
	funcs map[specKey][]*code.Func
}

// maxSpecializations bounds the memo of the specializations.
const maxSpecializations = 16

type specKey struct {
	m        *code.Module
	requests string
}

// specialized returns the functions of the requests rs of the module m, each a copy that the caller
// may change.
func specialized(m *code.Module, rs []library.Request) ([]*code.Func, error) {
	var b strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&b, "%q %q %q;", r.Fn, r.On, r.Name)
	}
	k := specKey{m, b.String()}
	specializations.mu.Lock()
	fs, ok := specializations.funcs[k]
	specializations.mu.Unlock()
	if !ok {
		fs = make([]*code.Func, len(rs))
		for i, r := range rs {
			f := m.Func(r.Fn)
			if f == nil {
				return nil, fmt.Errorf("specialize %s: the module %s has no composition %s", r.Name, m.Name, r.Fn)
			}
			var err error
			if fs[i], err = specialize(m, f, r.Name, r.On); err != nil {
				return nil, err
			}
		}
		specializations.mu.Lock()
		if specializations.funcs == nil {
			specializations.funcs = map[specKey][]*code.Func{}
		}
		if _, ok := specializations.funcs[k]; !ok {
			if len(specializations.keys) == maxSpecializations {
				delete(specializations.funcs, specializations.keys[0])
				specializations.keys = specializations.keys[1:]
			}
			specializations.keys = append(specializations.keys, k)
			specializations.funcs[k] = fs
		}
		specializations.mu.Unlock()
	}
	out := make([]*code.Func, len(fs))
	for i, f := range fs {
		out[i] = code.CloneFunc(f)
	}
	return out, nil
}
