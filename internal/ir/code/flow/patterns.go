package flow

import "syntaxir.dev/sxir/internal/ir/code"

// The patterns of the Flow stage (code.RegisterPatterns), in the order canonicalize tries them: the
// patterns of the base that keep the invariant that every block has a label of its own. Removing an
// unused label or splicing a block without one would break it, so the Flow stage has neither; an If
// with a constant condition leaves the statements of its branch, which declare no local since the
// variables of a Flow function are the function's. The Flow dialect's operations have no pattern of
// their own yet.
func init() {
	code.RegisterPatterns[code.Flow](code.FoldConstants, code.FoldBranches, code.DropAfterExit)
}
