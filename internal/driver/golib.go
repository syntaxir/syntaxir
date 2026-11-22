package driver

import (
	"context"

	gobackend "syntaxir.dev/sxir/internal/backend/codegen/golang"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The Go library is the route of the pipeline that prints the compositions into the Go runtime
// library, the parse state of the engine and its backends and their conformance bindings, which go
// generate writes (internal/backend/codegen/golang/libprint). No grammar and no target determine
// it: its pipeline starts at the library of the compositions with the requests of every file that it
// prints (gobackend.LibrarySource), specialize! specializes them, go-cold-path! splits the cold paths
// of the guards that generated code inlines (gopass.ColdPath), and print-libgo! prints the files
// (gobackend.LibraryPrinting):
//
//	pipeline library -> output {
//	  specialize!
//	  go-cold-path!
//	  print-libgo!
//	}

// goLibraryPipeline returns the pipeline of the Go library.
func goLibraryPipeline() libraryPipeline {
	p := pipe.Then3(pipe.Of(codepass.Specialization), pipe.Of(gopass.ColdPath), pipe.Of(gobackend.LibraryPrinting))
	return libraryPipeline{pipeline: p, steps: []string{codepass.PassSpecialize, gopass.PassColdPath, gobackend.LibraryPrinting.Name()}}
}

// goLibraryCompilation checks the options o of the Go library and returns its pipeline as assembled
// and as the call runs it: with the edits of o, which name no slot since the pipeline has none, or
// the pipeline of the text of o.
func goLibraryCompilation(o Options) (libraryPipeline, pipe.Pipeline[*library.Library, Output], error) {
	if err := CheckDebug(o.Debug); err != nil {
		return libraryPipeline{}, pipe.Pipeline[*library.Library, Output]{}, err
	}
	a := goLibraryPipeline()
	pl, err := callPipeline(a.pipeline, o)
	return a, pl, err
}

// GoLibrary runs the pipeline of the Go library under o and returns its files (result.Files), with
// paths relative to the root of the module.
func GoLibrary(ctx context.Context, o Options) (Output, error) {
	a, pl, err := goLibraryCompilation(o)
	if err != nil {
		return nil, err
	}
	d := newDebugger(o)
	out, err := execute(ctx, d, &pipe.Run{Settings: o.Settings, VerifyEach: o.Debug.VerifyEach, Observers: d.observers()}, pl, gobackend.LibrarySource())
	if err != nil {
		err = a.error(err)
	}
	if ferr := d.flush(); ferr != nil && err == nil {
		err = ferr
	}
	if err != nil {
		return nil, err
	}
	d.report()
	return out, nil
}

// DescribeGoLibrary returns the pipeline that GoLibrary runs under o, as text without headers, which
// the reader of the build writes (generate.DescribeGoLibrary).
func DescribeGoLibrary(o Options) (*pipe.Description, error) {
	_, pl, err := goLibraryCompilation(o)
	if err != nil {
		return nil, err
	}
	return pipe.Describe(pl), nil
}
