// Package enginetest compiles and runs grammars with the engine for the tests of the packages that
// compare with it or build on a compiled engine.Program: the generators, the output schema, the
// value model and the bytecode. The tests of package engine itself cannot import it (it imports
// the engine); they keep their own helpers of the same names.
package enginetest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/frontend/schemaexport"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/schema"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// UseEngine makes codegentest compile the grammars that tests generate parsers for with the engine,
// and compare the generated parsers with it, as package engine does. Call it from an init function of
// the test package.
func UseEngine() {
	codegentest.Compile = CompileForGenerator
	codegentest.Reference = Reference
	codegentest.ReferenceStream = ReferenceStream
	codegentest.ReferenceTraced = ReferenceTraced
}

// Main runs the tests of a package that builds generated programs: with the Go flags and the
// private build cache of codegentest, and failing when a test builds generated fixtures without
// choosing their form. Call it from TestMain.
func Main(m *testing.M) {
	// Tests build generated programs in fresh temporary directories, with a build cache private
	// to the test process (SetGoCache) so that their builds, which no later run reuses, do not
	// accumulate in the shared cache. -trimpath makes the entries independent of the directory,
	// so the tests of the package share the builds of the standard library and of this module;
	// commands that set GOFLAGS add it themselves. -mod=mod lets the temporary modules that
	// replace this module resolve it without a go.sum.
	codegentest.SetGoFlags()
	removeGoCache := codegentest.SetGoCache()
	code := m.Run()
	removeGoCache()
	if names := codegentest.UnwrappedFixtureTests(); len(names) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests build generated fixtures without choosing their form: %s\nWrap the test body in codegentest.EachMode, or call codegentest.NeedsBudgetControls, or pin the form with codegentest.PinOrdinary\n", strings.Join(names, ", "))
		code = 1
	}
	os.Exit(code)
}

// Parse parses SyntaxIR source for a test whose subject is not start rules (compiletest.Parse).
func Parse(src string) (*ast.Grammar, error) { return compiletest.Parse(src) }

// MustParse is Parse that panics on a syntax error.
func MustParse(src string) *ast.Grammar {
	g, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return g
}

// MustCompile compiles SyntaxIR source with source information, so that results show rule names
// and capture labels.
func MustCompile(t testing.TB, src string, opts ...check.Options) *engine.Program {
	t.Helper()
	var o check.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	o.SourceInfo = true
	return CompileBare(t, src, o)
}

// CompileBare compiles SyntaxIR source with the options as given: without source information
// unless they request it.
func CompileBare(t testing.TB, src string, opts ...check.Options) *engine.Program {
	t.Helper()
	g, err := Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	var o check.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	prog, err := entry.Compile(g, entry.Options{Compile: o})
	if err != nil {
		t.Fatalf("compile error: %v\n%s", err, src)
	}
	return prog
}

// CompileStarts compiles source as written, without marking start rules for the test.
func CompileStarts(t testing.TB, src string) (*engine.Program, error) {
	t.Helper()
	g, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	return entry.Compile(g, entry.Options{})
}

// CompileMessage compiles src and returns the error message, or "" if it compiles.
func CompileMessage(t testing.TB, src string, opts ...check.Options) string {
	t.Helper()
	g, err := Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	var o check.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	_, err = entry.Compile(g, entry.Options{Compile: o})
	if err == nil {
		return ""
	}
	return err.Error()
}

// Reference compiles src with the engine for the comparisons of generated parsers with it. Its
// parses record expectations, as the generated parsers' do.
func Reference(t *testing.T, src string, opts check.Options) codegentest.ReferenceParse {
	t.Helper()
	prog := CompileBare(t, src, opts)
	return func(input string, bytes, recognize bool, missing int, units *uint64) (*value.Node, error) {
		unit := sxirinput.CodePoints
		if bytes {
			unit = sxirinput.Bytes
		}
		return prog.ParseWith("main", input, engine.ParseOptions{Unit: unit, Recognize: recognize, Expectations: true, MissingTokens: missing, ComputeReport: units})
	}
}

// ReferenceTraced compiles src with opts and returns the traced parse of its rule main and the parse
// of any of its rules, with positions in bytes (codegentest.ReferenceTraced). The traced parse
// reports the value of every successful evaluation of a rule that produces a value of the rule at
// its exit (state.TraceValue).
func ReferenceTraced(t *testing.T, src string, opts check.Options) (codegentest.TracedParse, codegentest.RuleParse) {
	t.Helper()
	prog := CompileBare(t, src, opts)
	traced := func(input string, missing int, call func(codegentest.RuleCall)) (*value.Node, error) {
		return prog.ParseWith("main", input, engine.ParseOptions{Unit: sxirinput.Bytes, MissingTokens: missing, Trace: func(e engine.TraceEvent) {
			if v, ok := state.TraceValue(e); ok {
				call(codegentest.RuleCall{Rule: e.Rule, Start: e.Pos, End: e.End, Value: v})
			}
		}})
	}
	rule := func(r, input string, missing int) (*value.Node, error) {
		return prog.ParseWith(r, input, engine.ParseOptions{Unit: sxirinput.Bytes, Expectations: true, MissingTokens: missing})
	}
	return traced, rule
}

// ReferenceStream compiles src with opts and returns the stream parse of its rule main
// (codegentest.ReferenceStream).
func ReferenceStream(t *testing.T, src string, opts check.Options) codegentest.ReferenceStreamParse {
	t.Helper()
	prog := CompileBare(t, src, opts)
	return func(input string, bytes bool, units *uint64) error {
		unit := sxirinput.CodePoints
		if bytes {
			unit = sxirinput.Bytes
		}
		return prog.ParseStreamWith("main", strings.NewReader(input), nil, engine.ParseOptions{Unit: unit, Expectations: true, ComputeReport: units})
	}
}

// CompileForGenerator compiles grammars for the generators with the engine (driver.Compiler).
var CompileForGenerator driver.Compiler = compileForGenerator

// compileForGenerator compiles g with the engine and returns its checked program and a function that
// checks the programs derived from it as the engine derives them.
func compileForGenerator(_ context.Context, g *ast.Grammar, opts check.Options) (*frontend.Result, func(driver.Role) (*frontend.Result, error), error) {
	prog, err := entry.Compile(g, entry.Options{Compile: opts})
	if err != nil {
		return nil, nil, err
	}
	return entry.Checked(prog), func(role driver.Role) (*frontend.Result, error) { return entry.Derive(prog, role) }, nil
}

// OutputSchema exports the output schema of p for start, as Parser.OutputSchema does.
func OutputSchema(p *engine.Program, start string, allRules bool) (*schema.Output, error) {
	sp, err := entry.SchemaSource(p, start)
	if err != nil {
		return nil, err
	}
	return schemaexport.Export(sp, start, allRules)
}

// Result parses input from main with o and returns the result, or the error prefixed "error: ".
func Result(prog *engine.Program, input string, o engine.ParseOptions) string {
	n, err := prog.ParseWith("main", input, o)
	if err != nil {
		return "error: " + err.Error()
	}
	return n.String()
}

// ParseGrammar parses SyntaxIR source to its syntax tree, as the compiler does.
func ParseGrammar(src string) (*ast.Grammar, error) { return parser.Parse(src) }

// CompileOption configures Compile and CompileSource.
type CompileOption func(*entry.Options)

// WithSourceInfo records the rule of each node and the names of captures in parse results.
func WithSourceInfo() CompileOption { return func(o *entry.Options) { o.Compile.SourceInfo = true } }

// WithDefaultValue drops value actions and declared rule types, so that the parser returns the
// default node tree.
func WithDefaultValue() CompileOption {
	return func(o *entry.Options) { o.Compile.DefaultValue = true }
}

// Compile compiles g to an engine parser that starts at the rule start (empty for the default
// start rule) and runs on the program's default backend, the closure backend.
func Compile(g *ast.Grammar, start string, opts ...CompileOption) (*engine.Parser, error) {
	var o entry.Options
	for _, opt := range opts {
		opt(&o)
	}
	return entry.CompileParser(g, start, o)
}

// CompileSource parses SyntaxIR source and compiles it with Compile.
func CompileSource(src, start string, opts ...CompileOption) (*engine.Parser, error) {
	g, err := ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return Compile(g, start, opts...)
}

// CorpusProgram compiles a corpus grammar, or reports false for a grammar that does not compile.
func CorpusProgram(t testing.TB, c codegentest.Case) (*engine.Program, bool) {
	t.Helper()
	g, err := Parse(c.Src)
	if err != nil {
		return nil, false
	}
	prog, err := entry.Compile(g, entry.Options{})
	if err != nil {
		return nil, false
	}
	return prog, true
}
