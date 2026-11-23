// Package generate generates Go and TypeScript parser source from grammar syntax trees with the
// libraries of generated parsers. Package compiler generates through it from grammar sources;
// programs and tests of this module that hold a syntax tree call it directly.
package generate

import (
	"context"
	"fmt"
	"io/fs"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Go generates the files of a Go parser for g with o, running the passes that o selects (Settings),
// and the embedded copy of the Go library (pkg/runtime/libgo) with LibEmbed and LibOnly. g is not read with LibOnly and may be nil.
func Go(g *ast.Grammar, o common.GenOptions) ([]common.GeneratedFile, error) {
	o.LibGo = libGo
	return GoWith(driver.CheckForGenerator, g, o, driver.Options{}, nil)
}

// GoWith is Go with the compiler c, which checks the grammar, the library o.LibGo, the edits and
// observers of x and the hooks h of the printer (driver.Target.GoHooks), which only tests set; the
// settings and the debugging settings of x come from o.
func GoWith(c driver.Compiler, g *ast.Grammar, o common.GenOptions, x driver.Options, h *goprint.Hooks) ([]common.GeneratedFile, error) {
	return one(TargetsWith(c, g, x, driver.Target{Backend: result.Go, Gen: o, GoHooks: h}))
}

// TypeScript generates the files of a TypeScript parser for g with o, running the passes that o
// selects (Settings), and lib.ts with LibEmbed and LibOnly. g is not read with LibOnly and may be nil.
func TypeScript(g *ast.Grammar, o common.GenOptions) ([]common.GeneratedFile, error) {
	o.TSLib = tsLib
	return TypeScriptWith(driver.CheckForGenerator, g, o, driver.Options{}, nil)
}

// TypeScriptWith is TypeScript with the compiler c, which checks the grammar, the library o.TSLib,
// the edits and observers of x and the hooks h of the printer (driver.Target.TSHooks), which only
// tests set; the settings and the debugging settings of x come from o.
func TypeScriptWith(c driver.Compiler, g *ast.Grammar, o common.GenOptions, x driver.Options, h *tsprint.Hooks) ([]common.GeneratedFile, error) {
	return one(TargetsWith(c, g, x, driver.Target{Backend: result.TypeScript, Gen: o, TSHooks: h}))
}

// one returns the files of the one target of a generation.
func one(files [][]common.GeneratedFile, err error) ([]common.GeneratedFile, error) {
	if err != nil {
		return nil, err
	}
	return files[0], nil
}

// Targets generates the files of each target of g, in order, from one check of g and one
// compilation, with the libraries of this version: the result of each target is what Go or
// TypeScript returns for it alone. The options of the program (Start, SourceInfo, DefaultValue,
// LeftRecursionBindings), the settings (Optimize, Opt, NoOpt) and the debugging settings apply to the
// whole call, so the targets give them equal; every other option is the target's. With LibOnly, which
// every target or none gives, g is not read and may be nil, and each target generates its library
// alone.
func Targets(g *ast.Grammar, targets ...driver.Target) ([][]common.GeneratedFile, error) {
	for i := range targets {
		targets[i].Gen.LibGo, targets[i].Gen.TSLib = libGo, tsLib
	}
	return TargetsWith(driver.CheckForGenerator, g, driver.Options{}, targets...)
}

// TargetsWith is Targets with the compiler c, which checks the grammar, the libraries of the
// targets' options and the edits and observers of x; the settings and the debugging settings of x
// come from the options of the first target. On an error it returns no files.
func TargetsWith(c driver.Compiler, g *ast.Grammar, x driver.Options, targets ...driver.Target) ([][]common.GeneratedFile, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("a generation takes at least one target")
	}
	libOnly := 0
	for _, t := range targets {
		if t.Gen.Lib == common.LibOnly {
			libOnly++
		}
	}
	if libOnly > 0 {
		if libOnly < len(targets) {
			return nil, fmt.Errorf("the library alone (LibOnly) is generated for every target of a generation or for none")
		}
		return libraries(x, targets)
	}
	vs := make([]driver.Target, len(targets))
	for i, t := range targets {
		var err error
		switch t.Backend {
		case result.Go:
			t.Gen, err = goprint.Validate(t.Gen)
		case result.TypeScript:
			t.Gen, err = tsprint.Validate(t.Gen)
		default:
			err = fmt.Errorf("%s is no target of a generation (go, ts)", t.Backend)
		}
		if err != nil {
			if len(targets) > 1 {
				err = fmt.Errorf("target %s: %w", t.Backend, err)
			}
			return nil, err
		}
		vs[i] = t
	}
	o := vs[0].Gen
	var err error
	if x.Settings, err = Settings(o); err != nil {
		return nil, err
	}
	x.Debug = o.Debug
	p, err := driver.CheckWith(c, g, common.CompileOptions{SourceInfo: o.SourceInfo, DefaultValue: o.DefaultValue, LeftRecursionBindings: o.LeftRecursionBindings}, o.Start)
	if err != nil {
		return nil, err
	}
	if err := writePipeline(p, x, vs); err != nil {
		return nil, err
	}
	outs, err := driver.Compile(context.Background(), p, x, vs...)
	if err != nil {
		return nil, err
	}
	files := make([][]common.GeneratedFile, len(outs))
	for i, out := range outs {
		files[i] = out.(*result.Files).Files
	}
	return files, nil
}

// libraries generates the library alone (LibOnly) of each target, in order: the copy of the Go
// library for a Go target, and the pipeline of the TypeScript library alone, under the edits,
// observers and text of x and the debugging settings of the target, for a TypeScript target.
func libraries(x driver.Options, targets []driver.Target) ([][]common.GeneratedFile, error) {
	var files [][]common.GeneratedFile
	for _, t := range targets {
		var fs []common.GeneratedFile
		var err error
		switch t.Backend {
		case result.Go:
			fs, err = goprint.LibFiles(t.Gen)
		case result.TypeScript:
			x := x
			x.Debug = t.Gen.Debug
			if err = writePipeline(nil, x, []driver.Target{t}); err != nil {
				break
			}
			var outs []driver.Output
			if outs, err = driver.Compile(context.Background(), nil, x, t); err == nil {
				fs = outs[0].(*result.Files).Files
			}
		default:
			err = fmt.Errorf("%s is no target of a generation (go, ts)", t.Backend)
		}
		if err != nil {
			if len(targets) > 1 {
				err = fmt.Errorf("target %s: %w", t.Backend, err)
			}
			return nil, err
		}
		files = append(files, fs)
	}
	return files, nil
}

// libGo and tsLib are the libraries of generated parsers: Go generation copies the Go library
// into packages generated with LibEmbed and reads it to qualify the names generated code uses
// from it; TypeScript generation assembles lib.ts from the TypeScript library. The copies live in
// the back end rather than in the runtime so that only programs that generate code carry them.
var (
	libGo fs.FS = libsrc.FS
	tsLib fs.FS = tslib.FS
)
