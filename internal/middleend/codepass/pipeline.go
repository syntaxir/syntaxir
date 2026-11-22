package codepass

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The passes of Exec IR as a pipeline (design 063): transformations of a lowered module that keep the
// behavior of its functions and that the backend printing it then prints. A pass has the name of the
// flag that turns it off, distinct from the names of the Core and Flow passes. The unit of a pass is
// a module at the Exec stage that holds one function (code.ExecFuncs), with the declarations of the
// module of its table.

// Unit is the unit of the Exec passes: a module at the Exec stage.
type Unit = code.At[code.Exec]

// Passes are the Exec passes that every target runs, in the order they run: canonicalize, the pass of
// the same name as at the Flow stage, with the patterns of the Exec stage.
var Passes = []*pipe.Pass[Unit, Unit]{ExecCanonicalize}

// Pipeline is the pipeline of the Exec passes on a module, each on every function of the module in
// turn (OnFuncs).
func Pipeline() pipe.Pipeline[Unit, Unit] { return OnFuncs(Passes...) }

// OnFuncs returns the pipeline that runs the passes ps on each function of a module, pass by pass
// (pipe.PerFunc on code.ExecFuncs).
func OnFuncs(ps ...*pipe.Pass[Unit, Unit]) pipe.Pipeline[Unit, Unit] {
	return pipe.PerFunc(code.ExecFuncs, pipe.Seq(code.ExecStage, ps...))
}

// Names returns the names of the Exec passes in the order they run.
func Names() []string {
	names := make([]string, len(Passes))
	for i, p := range Passes {
		names[i] = p.Name()
	}
	return names
}
