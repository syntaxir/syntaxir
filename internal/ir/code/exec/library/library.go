// Package library declares the unit library (design record 074, Units and Kinds): the compositions
// module of the runtime libraries (built by internal/ir/code/exec/compose), the specializations that a library
// requests of it, grouped by the files they are printed into, one specialized function per request,
// which the pass specialize adds, and the functions that later passes make of them. The module
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
// for the features On, as a function named Name, printed into the file File. A library of one file
// leaves File empty. Name is unique among the requests of its file, so that requests of one
// composition for one file, such as its forms in two parsers, do not collide. Cold, if not empty,
// asks for the cold path of the function to be split off its leading guards into a function named Cold,
// which the function calls, so that the guards stay small enough to inline into generated code (the Go
// target's go-cold-path); empty, the function is not split. Cold is named like the functions made of
// the requests (Made).
type Request struct {
	File string
	Fn   string
	On   []string
	Name string
	Cold string
}

// Made is a function that a pass made of the function of the request Request, such as a part split
// off it, which the library's printer prints with it. Its name is the one that the function of the
// request calls it by, unique among the functions made for the file of its request and declared by no
// composition of the module.
type Made struct {
	Request int
	Func    *code.Func
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
	// Made are the functions that the passes after specialize made, in the order they made them.
	Made []Made
}

// IR is the kind library. It has no printer: the specialized functions are printed by the library's
// printer, not dumped.
var IR = pipe.RegisterIR(&pipe.IR[*Library]{Name: "library", Verify: verify, Copy: clone})

// verify checks that the module is at no stage, that every request names a composition of the module
// and features that it declares under a name unique in its file, that the cold functions that requests
// ask for are named apart from the compositions, the requests and the other cold functions of their
// file, that the functions, if any, are one per request under its name, and that the functions made
// of them belong to a request and are named apart from the compositions and from the other functions
// made for the file.
func verify(l *Library) error {
	if l.Module == nil {
		return fmt.Errorf("the library has no compositions module")
	}
	if l.Module.Stage != "" {
		return fmt.Errorf("the compositions module %s of the library is at the %q stage", l.Module.Name, l.Module.Stage)
	}
	names := map[[2]string]bool{}
	for _, r := range l.Requests {
		if names[[2]string{r.File, r.Name}] {
			return fmt.Errorf("the library requests %s twice for the file %q", r.Name, r.File)
		}
		names[[2]string{r.File, r.Name}] = true
	}
	for _, r := range l.Requests {
		if r.Cold == "" {
			continue
		}
		if l.Module.Func(r.Cold) != nil {
			return fmt.Errorf("the library requests %s with the cold function %s, which has the name of a composition", r.Name, r.Cold)
		}
		if names[[2]string{r.File, r.Cold}] {
			return fmt.Errorf("the library requests %s with the cold function %s, which another function of the file %q has", r.Name, r.Cold, r.File)
		}
		names[[2]string{r.File, r.Cold}] = true
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
		if len(l.Made) > 0 {
			return fmt.Errorf("the library holds %d functions made of no specialized function", len(l.Made))
		}
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
	made := map[[2]string]bool{}
	for _, m := range l.Made {
		if m.Request < 0 || m.Request >= len(l.Requests) || m.Func == nil {
			return fmt.Errorf("a function made for the library belongs to no request (%d)", m.Request)
		}
		file := l.Requests[m.Request].File
		if l.Module.Func(m.Func.Name) != nil {
			return fmt.Errorf("the function %s made of the request %s has the name of a composition", m.Func.Name, l.Requests[m.Request].Name)
		}
		if made[[2]string{file, m.Func.Name}] {
			return fmt.Errorf("the library holds two functions %s made for the file %q", m.Func.Name, file)
		}
		made[[2]string{file, m.Func.Name}] = true
	}
	return nil
}

// clone returns a copy of l that a pass may change without changing l: new requests and new
// functions; the module, which no pass changes, is shared.
func clone(l *Library) *Library {
	c := &Library{Module: l.Module, Requests: make([]Request, len(l.Requests))}
	for i, r := range l.Requests {
		c.Requests[i] = r
		c.Requests[i].On = slices.Clone(r.On)
	}
	if l.Funcs != nil {
		c.Funcs = make([]*code.Func, len(l.Funcs))
		for i, f := range l.Funcs {
			c.Funcs[i] = code.CloneFunc(f)
		}
	}
	for _, m := range l.Made {
		c.Made = append(c.Made, Made{Request: m.Request, Func: code.CloneFunc(m.Func)})
	}
	return c
}
