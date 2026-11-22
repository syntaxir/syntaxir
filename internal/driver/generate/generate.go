// Package generate generates Go and TypeScript parser source from grammar syntax trees with the
// libraries of generated parsers. Package compiler generates through it from grammar sources;
// programs and tests of this module that hold a syntax tree call it directly.
package generate

import (
	"context"
	"io/fs"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend"
	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Go generates the files of a Go parser for g with o, running the passes that o selects (Settings),
// and the embedded copy of the Go library (pkg/runtime/libgo) with LibEmbed and LibOnly. g is not read with LibOnly and may be nil.
func Go(g *ast.Grammar, o common.GenOptions) ([]common.GeneratedFile, error) {
	o.LibGo = libGo
	return GoWith(checkForGenerator, g, o, driver.Options{}, nil)
}

// GoWith is Go with the compiler c, which checks the grammar, the library o.LibGo, the edits and
// observers of x and the hooks h of the printer (driver.Target.GoHooks), which only tests set; the
// settings and the debugging settings of x come from o.
func GoWith(c driver.Compiler, g *ast.Grammar, o common.GenOptions, x driver.Options, h *goprint.Hooks) ([]common.GeneratedFile, error) {
	if o.Lib == common.LibOnly {
		return goprint.LibFiles(o)
	}
	o, err := goprint.Validate(o)
	if err != nil {
		return nil, err
	}
	if x.Settings, err = Settings(o); err != nil {
		return nil, err
	}
	x.Debug = o.Debug
	p, err := driver.CheckWith(c, g, common.CompileOptions{SourceInfo: o.SourceInfo, DefaultValue: o.DefaultValue, LeftRecursionBindings: o.LeftRecursionBindings}, o.Start)
	if err != nil {
		return nil, err
	}
	out, err := driver.Compile(context.Background(), p, x, driver.Target{Backend: result.Go, Gen: o, GoHooks: h})
	if err != nil {
		return nil, err
	}
	return out[0].(*result.Files).Files, nil
}

// TypeScript generates the files of a TypeScript parser for g with o, running the passes that o
// selects (Settings), and lib.ts with LibEmbed and LibOnly. g is not read with LibOnly and may be nil.
func TypeScript(g *ast.Grammar, o common.GenOptions) ([]common.GeneratedFile, error) {
	o.TSLib = tsLib
	return TypeScriptWith(checkForGenerator, g, o, driver.Options{}, nil)
}

// TypeScriptWith is TypeScript with the compiler c, which checks the grammar, the library o.TSLib,
// the edits and observers of x and the hooks h of the printer (driver.Target.TSHooks), which only
// tests set; the settings and the debugging settings of x come from o.
func TypeScriptWith(c driver.Compiler, g *ast.Grammar, o common.GenOptions, x driver.Options, h *tsprint.Hooks) ([]common.GeneratedFile, error) {
	if o.Lib == common.LibOnly {
		x.Debug = o.Debug
		out, err := driver.Compile(context.Background(), nil, x, driver.Target{Backend: result.TypeScript, Gen: o, TSHooks: h})
		if err != nil {
			return nil, err
		}
		return out[0].(*result.Files).Files, nil
	}
	o, err := tsprint.Validate(o)
	if err != nil {
		return nil, err
	}
	if x.Settings, err = Settings(o); err != nil {
		return nil, err
	}
	x.Debug = o.Debug
	p, err := driver.CheckWith(c, g, common.CompileOptions{SourceInfo: o.SourceInfo, DefaultValue: o.DefaultValue, LeftRecursionBindings: o.LeftRecursionBindings}, o.Start)
	if err != nil {
		return nil, err
	}
	out, err := driver.Compile(context.Background(), p, x, driver.Target{Backend: result.TypeScript, Gen: o, TSHooks: h})
	if err != nil {
		return nil, err
	}
	return out[0].(*result.Files).Files, nil
}

// checkForGenerator checks g to its semantic model and sites, which is all a generator reads, and checks
// the recognizer of the program (as the engine's Program.Recognizer makes it) on first use. No
// backend of the engine is built.
func checkForGenerator(g *ast.Grammar, opts checkpkg.Options) (*frontend.Result, func() (*frontend.Result, error), error) {
	prog, err := check(g, opts, nil)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*frontend.Result, error) {
		source := prog.Grammar
		if prog.Source != nil {
			source = prog.Source
		}
		o := prog.Opts
		o.Recognition = checkpkg.Recognize
		return check(source, o, prog.Flags())
	}
	return prog, recognizer, nil
}

// check checks g with opts and, if not nil, the rule flags flags, naming the hidden rules of
// inline actions in errors as written.
func check(g *ast.Grammar, opts checkpkg.Options, flags []checkpkg.RuleFlags) (*frontend.Result, error) {
	names := desugar.InlineNames{}
	prog, err := frontend.Check(g, opts, flags, names, checkfacts.Source{}, nil)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return prog, nil
}

// libGo and tsLib are the libraries of generated parsers: Go generation copies the Go library
// into packages generated with LibEmbed and reads it to qualify the names generated code uses
// from it; TypeScript generation assembles lib.ts from the TypeScript library. The copies live in
// the back end rather than in the runtime so that only programs that generate code carry them.
var (
	libGo fs.FS = libsrc.FS
	tsLib fs.FS = tslib.FS
)
