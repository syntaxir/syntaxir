// Package libprint prints the compositions of internal/ir/code/exec/compose into the Go code that uses them:
// the library that generated Go parsers use (pkg/runtime/libgo), the parse state of the engine and its
// backends (internal/runtime/state, closure and vm), and the engine's and the library's conformance
// bindings. It owns the paths of those files, the specializations that each file prints (Requests)
// and the receiver and parameter text of the engine's types, so that the compositions stay free of
// their consumers' layout. It is the printer of the Go library's route of the pipeline (record 074):
// it builds no module, runs no pass and writes no file, but prints the files from the library unit
// that the pipeline specialized (Files). The TypeScript library's compositions are printed by the
// TypeScript generator.
package libprint

import (
	"fmt"

	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
)

//go:generate go run print_lib.go

// output is a file that the library printer prints: its path relative to the root of the module,
// the receiver types of the methods of the node and the typed parser in it ("" for none), the
// specializations that it prints, in order, and the function that prints it.
type output struct {
	path        string
	recv, trecv string
	specs       []goSpec
	print       func(u file) (string, error)
}

// outputs are the files of the Go library, the engine and their bindings, in the order they are
// printed.
var outputs = []output{
	{libGoFile, "Parser", "TParser", goSpecs, printLibGo},
	{goActionsFile, "Parser", "TParser", goActionSpecs, printGoActions},
	{goOperatorsFile, "Parser", "TParser", goOperatorSpecs, printGoOperators},
	{goStreamFile, "Parser", "", goStreamSpecs, printGoStream},
	{engineFile, "Parser", "", engineSpecs, printEngine},
	{engineActionsFile, "Parser", "", engineActionSpecs, printEngineActions},
	{engineOperatorsFile, "Parser", "", engineOperatorSpecs, printEngineOperators},
	{engineStreamFile, "Parser", "", engineStreamSpecs, printEngineStream},
	{engineDocumentFile, "Parser", "", engineDocumentSpecs, printEngineDocument},
	{engineClosureActionFile, "", "", engineBackendSpecs, printEngineClosureActions},
	{engineVMActionFile, "Program", "", engineBackendSpecs, printEngineVMActions},
	{engineOprBindingsFile, "", "", nil, printEngineOprBindings},
	{engineCtxBindingsFile, "", "", nil, printEngineCtxBindings},
	{engineFrameBindingsFile, "", "", nil, printEngineFrameBindings},
	{engineDocBindingsFile, "", "", nil, printEngineDocBindings},
	{engineExpBindingsFile, "", "", nil, printEngineExpBindings},
	{engineRuleBindingsFile, "", "", nil, printEngineRuleBindings},
	{engineMissingBindingsFile, "", "", nil, printEngineMissingBindings},
	{engineCallBindingsFile, "", "", nil, printEngineCallBindings},
	{engineActionStateBindingsFile, "", "", nil, printEngineActionStateBindings},
}

// requestName returns the name of the request of the specialization s in the output o: the Go
// declaration that it prints, the method name with its receiver type, or the name of a function, so
// that the forms of one composition in the node and the typed parser, and as a method and a function,
// are told apart.
func (o *output) requestName(s goSpec) string {
	name := s.name
	if name == "" {
		name = s.fn
	}
	recv := o.recv
	if s.typed {
		recv = o.trecv
	}
	if s.plain || recv == "" {
		return name
	}
	return recv + "." + name
}

// Requests returns the specializations that the Go library and the engine print, grouped by the
// files of outputs in their order: the requests of the source unit of the Go library's route, which
// the driver makes with the compositions module.
func Requests() []library.Request {
	var rs []library.Request
	for i := range outputs {
		o := &outputs[i]
		for _, s := range o.specs {
			rs = append(rs, library.Request{File: o.path, Fn: s.fn, On: s.on, Name: o.requestName(s), Cold: s.cold})
		}
	}
	return rs
}

// Files prints the files of outputs from the library l, which holds the specializations of Requests
// and the functions that the passes made of them, with paths relative to the root of the module.
func Files(l *library.Library) ([]result.File, error) {
	if l.Funcs == nil {
		return nil, fmt.Errorf("libprint: the library is not specialized")
	}
	index := map[[2]string]int{}
	for i, r := range l.Requests {
		index[[2]string{r.File, r.Name}] = i
	}
	var out []result.File
	for i := range outputs {
		o := &outputs[i]
		src, err := o.print(file{l: l, o: o, index: index})
		if err != nil {
			return nil, fmt.Errorf("libprint: %s: %w", o.path, err)
		}
		out = append(out, result.File{Path: o.path, Content: []byte(src)})
	}
	return out, nil
}

// file is the library as the printer of one output reads it.
type file struct {
	l     *library.Library
	o     *output
	index map[[2]string]int // the request of each file and name
}

// module returns the compositions module, which declares what the specialized functions call.
func (u file) module() *code.Module { return u.l.Module }

// request returns the index of the request of the specialization s in the file.
func (u file) request(s goSpec) (int, error) {
	i, ok := u.index[[2]string{u.o.path, u.o.requestName(s)}]
	if !ok {
		return 0, fmt.Errorf("the library holds no specialization %s", u.o.requestName(s))
	}
	return i, nil
}

// fn returns a copy of the function of the specialization s, named after its composition, whose name
// the printer maps to the printed one (print.Target.FuncName); the printer may change the copy.
func (u file) fn(s goSpec) (*code.Func, error) {
	i, err := u.request(s)
	if err != nil {
		return nil, err
	}
	f := *u.l.Funcs[i]
	f.Name = u.l.Requests[i].Fn
	return &f, nil
}

// made returns copies of the functions that the passes made of the function of the specialization s,
// in the order they made them.
func (u file) made(s goSpec) ([]*code.Func, error) {
	i, err := u.request(s)
	if err != nil {
		return nil, err
	}
	var fs []*code.Func
	for _, m := range u.l.Made {
		if m.Request == i {
			f := *m.Func
			fs = append(fs, &f)
		}
	}
	return fs, nil
}
