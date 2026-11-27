package codegentest

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Compile compiles grammars for the generators, as the compiler of package codegen does. The
// package of the tests sets it: this package cannot import the engine, whose tests import it.
var Compile driver.Compiler

// MutateCode, if set, changes the code that GenerateCode returns, for negative controls.
var MutateCode func([]byte) []byte

// expectationsOff holds the names of the tests that generate parsers without expectations.
var expectationsOff sync.Map

// OmitExpectations makes the Go parsers that t and its subtests generate record no expectations,
// as a parser generated without common.Go.Expectations. Generate and GenerateTS generate parsers that
// record them on every parse (see askExpectations), so that the tests that compare error messages
// keep their assertions.
func OmitExpectations(t testing.TB) {
	name := t.Name()
	expectationsOff.Store(name, true)
	t.Cleanup(func() { expectationsOff.Delete(name) })
}

// Generate generates a Go parser for g, as common.Go.Generate does, with the hooks of the generator
// that t set (SetGenHooks). The parser records expectations unless t called OmitExpectations.
func Generate(t testing.TB, g *ast.Grammar, opts common.GenOptions) ([]common.GeneratedFile, error) {
	if _, off := byTest(&expectationsOff, t); off {
		return GenerateRaw(t, g, opts)
	}
	opts.Expectations = true
	files, err := GenerateRaw(t, g, opts)
	if err != nil || opts.Lib == common.LibOnly {
		return files, err
	}
	return askExpectations(files), nil
}

// goEntryPoint matches the declaration of a generated Go function that takes parse options.
var goEntryPoint = regexp.MustCompile(`(?m)^func [^\n]*opts \.\.\.((?:rt\.|sxirruntime\.)?)ParseOption[^\n]*\{$`)

// goOneLineEntryPoint matches a generated Go function with parse options whose body is on one line.
var goOneLineEntryPoint = regexp.MustCompile(`(?m)^(func [^\n]*opts \.\.\.((?:rt\.|sxirruntime\.)?)ParseOption[^\n]*?\{) ([^\n]*\})$`)

// askExpectations makes every parse of the generated Go files pass WithExpectations first, as a
// caller that wants the expectations does, so that tests of a parser generated with expectations
// see the messages without each call passing the option. The tests of the default behavior
// generate with GenerateRaw.
func askExpectations(files []common.GeneratedFile) []common.GeneratedFile {
	out := make([]common.GeneratedFile, len(files))
	for i, f := range files {
		if strings.HasSuffix(f.Path, ".go") {
			f.Content = goEntryPoint.ReplaceAll(f.Content, []byte("$0\n\topts = append([]${1}ParseOption{${1}WithExpectations()}, opts...)"))
			f.Content = goOneLineEntryPoint.ReplaceAll(f.Content, []byte("$1 opts = append([]${2}ParseOption{${2}WithExpectations()}, opts...); $3"))
		}
		out[i] = f
	}
	return out
}

// GenerateRaw is Generate with the options as given. It does not verify after every pass (Debug.VerifyEach
// is off unless the options set it): the verification at the stage boundaries runs as in production,
// which verifies only where the optional passes before it did (record 074). The per-pass verification of
// the whole corpus is TestCorpusVerifiesEveryPass in the golang/print and typescript/print packages, in
// the default tier; a test that needs a pass named on failure sets Debug.VerifyEach itself.
func GenerateRaw(t testing.TB, g *ast.Grammar, opts common.GenOptions) ([]common.GeneratedFile, error) {
	if Compile == nil {
		t.Fatal("codegentest.Compile is not set: the test package must set it before generating")
	}
	opts.FeatureWitness = true
	opts = WithLib(opts)
	return generate.GoWith(Compile, g, opts, driver.Options{}, testGenHooks(t))
}

// GenerateTS generates a TypeScript parser for g that records expectations, as
// common.TypeScript.Generate does with Expectations.
func GenerateTS(g *ast.Grammar, opts common.GenOptions) ([]common.GeneratedFile, error) {
	opts.Expectations = true
	files, err := GenerateTSRaw(g, opts)
	if err != nil {
		return nil, err
	}
	return askTSExpectations(files), nil
}

// askTSExpectations makes every parse of the generated TypeScript files record expectations unless
// its options say otherwise, as askExpectations does for Go.
func askTSExpectations(files []common.GeneratedFile) []common.GeneratedFile {
	out := make([]common.GeneratedFile, len(files))
	for i, f := range files {
		text := strings.Replace(string(f.Content), "p.expectations = traceOptions?.expectations === true;", "p.expectations = g.expectations && traceOptions?.expectations !== false;", 1)
		f.Content = []byte(strings.Replace(text, "expectations: options?.expectations === true, signal,", "expectations: options?.expectations !== false, signal,", 1))
		out[i] = f
	}
	return out
}

// GenerateTSRaw is GenerateTS with the options as given. Like GenerateRaw, it verifies only at the stage
// boundaries unless the options set Debug.VerifyEach; TestCorpusVerifiesEveryPass in typescript/print
// verifies the corpus after every pass.
func GenerateTSRaw(g *ast.Grammar, opts common.GenOptions) ([]common.GeneratedFile, error) {
	if Compile == nil {
		panic("codegentest.Compile is not set: the test package must set it before generating")
	}
	opts = WithLib(opts)
	return generate.TypeScriptWith(Compile, g, opts, driver.Options{}, nil)
}

// GenerateTSStream is GenerateTSRaw for a parser printed as with -stream (print.Hooks.Stream), which
// the options of a TypeScript parser cannot select yet.
func GenerateTSStream(g *ast.Grammar, opts common.GenOptions) ([]common.GeneratedFile, error) {
	if Compile == nil {
		panic("codegentest.Compile is not set: the test package must set it before generating")
	}
	opts = WithLib(opts)
	return generate.TypeScriptWith(Compile, g, opts, driver.Options{}, &print.Hooks{Stream: true})
}

// NoOptIf returns the optimizations to turn off (common.GenOptions.NoOpt) if cond holds, and none
// otherwise, for tests that generate a parser with and without them.
func NoOptIf(cond bool, names ...string) []string {
	if !cond {
		return nil
	}
	return names
}
