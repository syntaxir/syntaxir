// Package tsprim holds the primitives of the TypeScript target at the Exec stage: the names of the
// operations that the TypeScript passes make and the printer prints, their immediate operands, and the
// classification of the primitives that both read. It is data: the passes (package pass) and the
// printer (package print) import it, and it imports only the primitives of the Exec stage.
package tsprim

import "syntaxir.dev/sxir/internal/ir/code/exec/prim"

// The primitive operations of the TypeScript target that the passes make.
const (
	SpendOp  = "ts.spend"
	MergeOp  = "ts.unisolateMerge"
	FailedOp = "ts.failed"
)

// SpendRun is the immediate operand of a call of SpendOp: the number of charges in a row.
type SpendRun struct{ N int }

// Failed is the immediate operand of a call of FailedOp: the variable of the bool that the Init of
// the If sets.
type Failed struct{ Flag string }

// TwoResults reports whether a call of the primitive name has two results, the value and whether it
// matched, which an assignment tests.
func TwoResults(name string) bool {
	switch name {
	case prim.OpMatchLit, prim.OpCallRule, prim.OpCallOutline, prim.OpResumeNext:
		return true
	}
	return false
}
