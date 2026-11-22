// Package compiler makes executable artifacts from SyntaxIR grammars: parsers that run on the
// bytecode VM (Compile, CompileGrammar), the reference parser that differential tests compare with
// (NewReferenceParser), compiled grammar files (Marshal), Go and TypeScript parser source
// (GenerateGo, GenerateTypeScript, and Generate for several languages from one check) and output
// schemas (OutputSchema).
//
// Every function takes the same Options: the start rule (WithStart), source information
// (WithSourceInfo), default values (WithDefaultValue), the optimization setting (WithOptimize) and
// the debugging setting of the compiler (WithDebug), so a grammar means the same parser whatever the
// artifact. The parsers implement runtime.Parser;
// the parser of Compile is a *vm.Parser, which also parses prefixes, iterates over streams, saves
// itself (Marshal) and runs on the iterative VM.
//
// A grammar source that does not load gives the errors of grammar.Load, such as
// grammar.SyntaxErrors; a grammar that loads but does not check gives grammar.CheckErrors.
package compiler

import (
	"io"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/schema"
	"syntaxir.dev/sxir/pkg/runtime/vm"
)

// Option configures a compilation.
type Option func(*options)

// options are the settings that Options set.
type options struct {
	start        string
	sourceInfo   bool
	defaultValue bool
	optimize     Optimize
	debug        Debug
}

// gen returns g with the options of the call o that the generators take: the start rule, the
// options of the check, the optimizations and the debugging settings, which apply to every target.
func (o options) gen(g common.GenOptions) common.GenOptions {
	g.Start, g.DefaultValue, g.SourceInfo = o.start, o.defaultValue, o.sourceInfo
	g.Optimize, g.Opt, g.NoOpt = o.optimize.Profile, o.optimize.Values, o.optimize.Off
	g.Debug = o.debug.internal()
	return g
}

// newOptions returns the settings that opts give, in order.
func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// entry returns the compile options of the settings.
func (o options) entry() entry.Options {
	return entry.Options{SourceInfo: o.sourceInfo, DefaultValue: o.defaultValue}
}

// WithStart selects the start rule of the parser, which must be a start rule (a rule marked
// @start). An empty name, the default, selects the default start rule.
func WithStart(name string) Option { return func(o *options) { o.start = name } }

// WithSourceInfo makes the parser record the rule of each node and the names of captures in parse
// results.
func WithSourceInfo() Option { return func(o *options) { o.sourceInfo = true } }

// WithDefaultValue makes the parser drop value actions and declared rule types and return the
// default node tree: the value of each rule is what it would be without its action. Actions that
// predicates read are kept. It cannot be combined with typed generated output.
func WithDefaultValue() Option { return func(o *options) { o.defaultValue = true } }

// Optimize is an optimization setting: a profile, then values for single optimizations, then
// optimizations turned off. None of them changes what a parser returns, only how it runs. The
// names are those of the guide to generated code and of sxir gen -opt and -no-opt.
type Optimize struct {
	// Profile is "speed" (the default, also for an empty name), "memory" or "size".
	Profile string
	// Values gives optimizations a value other than the profile's: 1 turns on one that the
	// profile leaves off, and a count sets an integer one.
	Values map[string]int
	// Off turns the named optimizations off, after Values.
	Off []string
}

// The optimization profiles.
var (
	// Speed is the fastest parse that does not cost code or memory out of proportion; the default.
	Speed = Optimize{Profile: "speed"}
	// Memory allocates typed values one by one: fewer bytes allocated and less live heap, with
	// many more allocations.
	Memory = Optimize{Profile: "memory"}
	// Size makes smaller generated Go source and binaries, for a little parse time.
	Size = Optimize{Profile: "size"}
)

// WithOptimize selects the optimization setting. An unknown profile or optimization, or a value
// that an optimization does not take, is an error of every function. The optimizations are
// those of the generators today: the bytecode and the reference backends have none to select, so
// for them the setting is checked and has no further effect.
func WithOptimize(o Optimize) Option { return func(opts *options) { opts.optimize = o } }

// Debug is the debugging setting of the compiler: it observes and checks the pass pipeline that
// makes an artifact, and changes the artifact only under a bisection limit. Each field is a flag of
// sxir gen: PrintBefore is -print-before, PrintAfter -print-after, PrintChanged -print-changed,
// VerifyEach -verify-each, Stats -stats, TimePasses -time-passes, OptBisectLimit -opt-bisect-limit
// and PrintPipeline -print-pipeline; Output has no flag, as the command writes to standard error. The pass and counter
// names and the text written to Output are a debugging aid: their form is not stable and may change
// in any version.
type Debug struct {
	// PrintAfter lists the steps and passes after which the intermediate representation is written
	// to Output as text: elaborate and the names of the Core passes for the Core IR; flowlower, the
	// names of the Flow passes and flow-finish for Code IR at the Flow stage; execlower and the names
	// of the Exec passes for Code IR at the Exec stage; or all. "sxir gen -h" lists the names.
	PrintAfter []string
	// PrintBefore lists the passes before whose runs the intermediate representation they receive is
	// written to Output, as PrintAfter writes it, or all for every pass. The required steps
	// (elaborate, flowlower, flow-finish and execlower) are not passes to print before.
	PrintBefore []string
	// PrintChanged writes the intermediate representation after each pass that changed it, and the
	// unit that the first such pass received before its first dump. A pass that PrintAfter also
	// names is written once.
	PrintChanged bool
	// VerifyEach verifies the intermediate representation after every pass instead of only where a
	// stage ends, so that an internal error names the first pass that broke it, and reports a pass
	// that claims to preserve every analysis but changed its unit. It costs compile time.
	VerifyEach bool
	// Stats writes, after the build, the statistics of each pass that ran: the units it ran on,
	// those it changed, its own counters and the applications of its patterns.
	Stats bool
	// TimePasses writes, after the build, the time each pass took, summed over the units it ran on,
	// and its share of the total.
	TimePasses bool
	// OptBisectLimit, if set, runs only the runs of optional passes numbered up to the limit, in the
	// order the pipeline makes them, and skips the later ones; the required steps always run, so the
	// output stays a valid parser. A run is one pass on one unit, such as one function, and each
	// application of a pattern of a pass takes a number of its own. Each decision is written to
	// Output with its number, the pass and the unit. Under a limit the generators generate once, so
	// the output may keep rules that it would otherwise leave out. A negative limit is an error.
	OptBisectLimit *int
	// PrintPipeline writes, before the build, the pass pipeline of the build as text: its steps in
	// order, the required ones marked with !, the patterns of each pass, and headers with the options
	// of the build.
	PrintPipeline bool
	// Output receives everything above. It is required when any of it is set.
	Output io.Writer
}

// WithDebug selects the debugging setting. A name that PrintAfter or PrintBefore does not know, a
// negative OptBisectLimit, or a setting that writes without an Output, is an error of every function. The setting takes effect in the
// functions that run the pass pipeline, the generators today; for the others it is checked and has
// no further effect.
func WithDebug(d Debug) Option { return func(o *options) { o.debug = d } }

// internal returns the debugging setting of the generators.
func (d Debug) internal() common.Debug {
	return common.Debug{PrintAfter: d.PrintAfter, PrintBefore: d.PrintBefore, PrintChanged: d.PrintChanged, VerifyEach: d.VerifyEach, Stats: d.Stats, TimePasses: d.TimePasses, OptBisectLimit: d.OptBisectLimit, PrintPipeline: d.PrintPipeline, Output: d.Output}
}

// check reports an invalid optimization or debugging setting.
func (o options) check() error {
	_, err := generate.Settings(common.GenOptions{Optimize: o.optimize.Profile, Opt: o.optimize.Values, NoOpt: o.optimize.Off, Debug: o.debug.internal()})
	return err
}

// build checks g and returns the engine parser of the settings, on the program's default
// backend.
func build(g *ast.Grammar, o options) (*engine.Parser, error) {
	if err := o.check(); err != nil {
		return nil, err
	}
	p, err := entry.CompileParser(g, o.start, o.entry())
	return p, grammarErrors(err)
}

// load loads the grammar of src and compiles it.
func load(src grammar.Source, o options) (*engine.Parser, error) {
	g, err := grammar.Load(src)
	if err != nil {
		return nil, err
	}
	return build(g, o)
}

// Compile compiles the grammar src to a parser that runs on the bytecode VM. The parser is a
// *vm.Parser.
func Compile(src grammar.Source, opts ...Option) (runtime.Parser, error) {
	p, err := load(src, newOptions(opts))
	if err != nil {
		return nil, err
	}
	return engine.VMParser(p), nil
}

// CompileGrammar compiles a grammar given as its syntax tree, such as one a program builds, to a
// parser that runs on the bytecode VM. The parser is a *vm.Parser.
func CompileGrammar(g *ast.Grammar, opts ...Option) (runtime.Parser, error) {
	p, err := build(g, newOptions(opts))
	if err != nil {
		return nil, err
	}
	return engine.VMParser(p), nil
}

// NewReferenceParser compiles the grammar src to the reference parser, which runs the grammar's
// Core IR as Go closures. It is the parser that the other backends are tested against; it is
// slower than the parser of Compile, and the only one that inserts missing tokens
// (runtime.WithMissingTokens). Its Documents have only the methods of runtime.Document.
func NewReferenceParser(src grammar.Source, opts ...Option) (runtime.Parser, error) {
	p, err := load(src, newOptions(opts))
	if err != nil {
		return nil, err
	}
	return engine.Reference(p), nil
}

// Marshal compiles the grammar src and encodes it in the .sxirc format in mode: the bytecode that
// mode serves and the start rule. vm.LoadParser loads it without the grammar or the compiler.
func Marshal(src grammar.Source, mode vm.CompiledMode, opts ...Option) ([]byte, error) {
	p, err := load(src, newOptions(opts))
	if err != nil {
		return nil, err
	}
	return p.Marshal(engine.CompiledMode(mode))
}

// OutputSchema exports the output schema of the grammar src: the types of the values that its
// start rule returns, of the rules those values contain and of the types they refer to. The
// parsers' OutputSchema with runtime.WithAllSchemaRules exports every rule and type.
func OutputSchema(src grammar.Source, opts ...Option) (*schema.Output, error) {
	p, err := load(src, newOptions(opts))
	if err != nil {
		return nil, err
	}
	return p.OutputSchema()
}
