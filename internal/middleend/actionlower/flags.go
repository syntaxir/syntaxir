package actionlower

import "syntaxir.dev/sxir/internal/pass"

// The flags of the lowering of action expressions (Lower), which the steps that run it declare as
// parameters (Options.TextDispatch, Options.FieldCompare).
const (
	TextDispatch = "text-dispatch"
	FieldCompare = "field-compare"
)

func init() {
	pass.Register(
		pass.Flag{Name: TextDispatch, Kind: pass.KindLowering, Default: 1, Doc: "a predicate that compares the text of a capture with literals tests them at once when the parse runs without limit controls"},
		pass.Flag{Name: FieldCompare, Kind: pass.KindLowering, Default: 1, Doc: "typed Go compares fields in the actions and predicates of typed rules without boxing string fields"},
	)
}
