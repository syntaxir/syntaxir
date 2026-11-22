// Package library declares the unit library (design record 074, Units and Kinds): the compositions
// module of the runtime libraries (built by internal/ir/code/exec/compose), the specializations that a library
// requests of it, and one specialized function per request, which the pass specialize adds. The module
// is at no stage: its operations are the runtime's own and the Action dialect's, never a Flow
// operation, so a printer may read it beside the units of the Exec stage.
package library

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Request is a specialization that a library requests: the composition Fn of the module, specialized
// for the features On, as a function named Name.
type Request struct {
	Fn   string
	On   []string
	Name string
}

// Library is the unit library: the compositions module, the requests of a library and, after
// specialize, one specialized function per request, in request order. The functions are kept outside
// the module, which no pass changes, so that requests of one composition under different features do
// not collide.
type Library struct {
	Module   *code.Module
	Requests []Request
	// Funcs are the specialized functions, Funcs[i] for Requests[i]; nil before specialize.
	Funcs []*code.Func
}

// IR is the kind library. It has no printer: the specialized functions are printed by the library's
// printer, not dumped.
var IR = pipe.RegisterIR(&pipe.IR[*Library]{Name: "library", Verify: verify, Copy: clone})

// verify checks that the module is at no stage, that every request names a composition of the module
// and features that it declares, and that the functions, if any, are one per request under its name.
func verify(l *Library) error {
	if l.Module == nil {
		return fmt.Errorf("the library has no compositions module")
	}
	if l.Module.Stage != "" {
		return fmt.Errorf("the compositions module %s of the library is at the %q stage", l.Module.Name, l.Module.Stage)
	}
	for _, r := range l.Requests {
		if l.Module.Func(r.Fn) == nil {
			return fmt.Errorf("the library requests %s of the composition %s, which the module does not declare", r.Name, r.Fn)
		}
		for _, f := range r.On {
			if !slices.Contains(l.Module.Features, f) {
				return fmt.Errorf("the library requests %s for the feature %s, which the module does not declare", r.Name, f)
			}
		}
	}
	if l.Funcs == nil {
		return nil
	}
	if len(l.Funcs) != len(l.Requests) {
		return fmt.Errorf("the library holds %d specialized functions for %d requests", len(l.Funcs), len(l.Requests))
	}
	for i, f := range l.Funcs {
		if f == nil || f.Name != l.Requests[i].Name {
			return fmt.Errorf("the specialized function %d of the library is not the request %s", i, l.Requests[i].Name)
		}
	}
	return nil
}

// clone returns a copy of l that a pass may change without changing l: new requests and new
// functions; the module, which no pass changes, is shared.
func clone(l *Library) *Library {
	c := &Library{Module: l.Module, Requests: make([]Request, len(l.Requests))}
	for i, r := range l.Requests {
		c.Requests[i] = Request{Fn: r.Fn, On: slices.Clone(r.On), Name: r.Name}
	}
	if l.Funcs != nil {
		c.Funcs = make([]*code.Func, len(l.Funcs))
		for i, f := range l.Funcs {
			c.Funcs[i] = code.CloneFunc(f)
		}
	}
	return c
}
