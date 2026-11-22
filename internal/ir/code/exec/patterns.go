package exec

import "syntaxir.dev/sxir/internal/ir/code"

// The patterns of the Exec stage (code.RegisterPatterns), in the order canonicalize tries them: the
// patterns of the statements and expressions of the base. The Exec dialect's operations have no
// pattern of their own yet.
func init() {
	code.RegisterPatterns[code.Exec](code.FoldConstants, code.FoldBranches, code.DropAfterExit,
		code.DropUnusedLabels, code.SpliceBlocks)
}
