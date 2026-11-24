package flow

import "syntaxir.dev/sxir/internal/ir/code"

// The patterns of the Flow stage (code.RegisterPatterns), in the order canonicalize tries them: the
// patterns of the base that keep the invariant that every block has a label of its own. Removing an
// unused label or splicing a block without one would break it, so the Flow stage has neither. It has
// neither fold-constants nor fold-branches either: the lowering (internal/middleend/flowlower) writes every
// condition from variables and calls, never from constants alone, and no Flow pass makes one, so no
// pattern that folds a constant has a match at this stage (none applied on any case of the generation
// corpus, for the VM or for the Go and TypeScript generators). It keeps drop-after-exit, which removes the
// code that follows the jump of a failure the lowering writes. The Flow dialect's operations have no pattern
// of their own yet.
func init() {
	code.RegisterPatterns[code.Flow](code.DropAfterExit)
}
