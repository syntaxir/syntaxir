package check

// Recognition is what a compiled program does with the input: build values or only check it.
type Recognition uint8

const (
	// BuildValues builds the values of the grammar (a tree, or what the actions return).
	BuildValues Recognition = iota
	// Recognize only checks whether the input conforms to the grammar, without building a tree.
	// Only the rules that predicates read, and the rules they call, build their values. It is the
	// program of Program.Recognizer and the module of a compiled grammar in recognize mode, which
	// serves Parse and ParsePrefix with the engine's WithRecognize option and no stream parse.
	Recognize
	// RecognizeWithStreamValues is Recognize that also keeps the values of the rules with a
	// #stream repetition and of the rules they call, as a program that hands those elements to a
	// caller needs them.
	RecognizeWithStreamValues
)

// Options configures compilation.
type Options struct {
	// DisableMemo disables memoization (except for left-recursion leaders).
	DisableMemo bool
	// SourceInfo records source information in the values: the rule that produced each node and
	// where its captures are.
	SourceInfo bool
	// DefaultValue makes every rule produce the default value of its expression: value actions
	// (=>) and the declared types of rules are dropped before compilation, so the parser returns
	// the default node tree even for grammars with actions. Predicates still run, because they
	// decide the match, and the rules that make the values predicates read keep their actions and
	// types (defaultValueGrammar).
	DefaultValue bool
	// Recognition selects whether the program builds values or only checks the input.
	Recognition Recognition
	// NoProjections compiles repetitions as written (project.go), for tests: with projections in
	// every backend, only a program without them can check what they produce.
	NoProjections bool
	// EagerAttributeCopies disables copy-on-change normalization for measurements.
	// It affects compilation only; parser execution has no corresponding branch.
	EagerAttributeCopies bool
	// LeftRecursionBindings accepts a binding that may run before a left call into a cycle that
	// reads its variable (checkLeftRecursionBindings), for tests of the runtime's left-recursion
	// guard, which such a binding reaches with another variable environment.
	LeftRecursionBindings bool
	// NoRunSites numbers no run site and no scan site, and NoScanSites no scan site, for tests that
	// compare parses with and without them. They are not written to the text of a pipeline: a program
	// without sites is not reproducible from it.
	NoRunSites, NoScanSites bool
}
