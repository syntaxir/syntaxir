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
}

// VerifyVariants returns the option sets of the per-pass verification tests. The tests set the
// package name, the start rule and Debug.VerifyEach themselves.
func VerifyVariants() []VerifyVariant {
	return []VerifyVariant{
		{Name: "ordinary", Opts: common.GenOptions{Types: true, TypedOnly: true}},
		{Name: "size", Opts: common.GenOptions{Types: true, TypedOnly: true, Optimize: "size"}},
		{Name: "class-tables", Opts: common.GenOptions{Types: true, TypedOnly: true, Opt: map[string]int{"class-tables": 2}}},
		{Name: "recognize", Opts: common.GenOptions{Types: true, TypedOnly: true, Recognize: true}},
		{Name: "document", Opts: common.GenOptions{Types: true, Document: true}},
		{Name: "stream", Opts: common.GenOptions{Types: true, Stream: true}, Stream: true},
	}
}

// TSRefused maps the corpus cases that the TypeScript backend refuses by design to the text of the
// refusal. The TypeScript per-pass test asserts the refusal instead of verifying these cases.
var TSRefused = map[string]string{
	"testdata/typed/spanname.sxir": "cannot be generated for TypeScript",
}
