package golang

import (
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/golang/libprint"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The Go library is the route of the pipeline that prints the compositions into the Go runtime
// library, the engine and their conformance bindings, which go generate writes. No grammar determines
// it, so it starts at the library (LibrarySource), the compositions with the specializations that
// the files of the library printer request (libprint.Requests); specialize specializes them,
// go-cold-path splits the cold paths of the guards that inline into generated code
// (gopass.ColdPath), and print-libgo! prints the files:
//
//	pipeline library -> output {
//	  specialize!
//	  go-cold-path!
//	  print-libgo!
//	}

// compositions returns the compositions module (compose.Build), built once: no pass changes it, and
// the specializations of a module are memoized by its identity.
var compositions = sync.OnceValue(compose.Build)

// LibrarySource returns the source unit of the Go library: the compositions with the requests of
// every file that the library printer prints.
func LibrarySource() *library.Library {
	return &library.Library{Module: compositions(), Requests: libprint.Requests()}
}

// LibraryPrinting is the step print-libgo: it prints the files of the Go library, the engine and
// their conformance bindings from the specialized compositions of the library, with paths relative
// to the root of the module.
var LibraryPrinting = pipe.New(pipe.Def[*library.Library, result.Output]{
	Decl: pipe.Decl{Name: "print-libgo", Required: true,
		Obligation: "The files are the Go library, the engine and their bindings of the specialized compositions."},
	In: library.IR, Out: result.IR,
	Run: func(_ *pipe.Ctx[*library.Library], l *library.Library) (result.Output, pass.PreservedSet, error) {
		files, err := libprint.Files(l)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return &result.Files{Lang: result.Go, Files: files}, pass.PreserveNone(), nil
	},
})
