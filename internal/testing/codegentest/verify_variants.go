package codegentest

import "syntaxir.dev/sxir/internal/backend/codegen/common"

// VerifyVariant is an option set with which the per-pass verification tests generate every corpus
// grammar. The ordinary set comes first; the others select passes that only a mode or an optimization
// runs, so that those passes are verified after every pass too. Stream applies only to grammars that
// use a #stream repetition (the tests check the source for it).
type VerifyVariant struct {
	Name   string
	Opts   common.GenOptions
	Stream bool
	// Long moves the variant to the long tier (build tag sxir_long), which runs it in the
	// TestCorpusVerifiesEveryPassLong tests, so that the default tier stays within its time.
	Long bool
}

// VerifyVariants returns the option sets of the per-pass verification tests. The tests set the
// package name, the start rule and Debug.VerifyEach themselves. The default tier runs the ordinary
// set and Optimize "size", which sets class-tables to 5 (driver profile "size"), so it reaches the
// class-tables pass with the shipped value. The other variants are long: class-tables at the
// opt-in threshold 2, Recognize, Document and Stream.
func VerifyVariants() []VerifyVariant {
	return []VerifyVariant{
		{Name: "ordinary", Opts: common.GenOptions{Types: true, TypedOnly: true}},
		{Name: "size", Opts: common.GenOptions{Types: true, TypedOnly: true, Optimize: "size"}},
		{Name: "class-tables", Opts: common.GenOptions{Types: true, TypedOnly: true, Opt: map[string]int{"class-tables": 2}}, Long: true},
		{Name: "recognize", Opts: common.GenOptions{Types: true, TypedOnly: true, Recognize: true}, Long: true},
		{Name: "document", Opts: common.GenOptions{Types: true, Document: true}, Long: true},
		{Name: "stream", Opts: common.GenOptions{Types: true, Stream: true}, Stream: true, Long: true},
	}
}

// TSRefused maps the corpus cases that the TypeScript backend refuses by design to the text of the
// refusal. The TypeScript per-pass test asserts the refusal instead of verifying these cases.
var TSRefused = map[string]string{
	"testdata/typed/spanname.sxir": "cannot be generated for TypeScript",
}
