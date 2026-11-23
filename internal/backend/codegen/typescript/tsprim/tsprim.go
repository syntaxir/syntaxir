// Package tsprim holds the primitives of the TypeScript target at the Exec stage: the names of the
// operations that the TypeScript passes make and the printer prints, their immediate operands, and the
// classification of the primitives that both read. It is data: the passes (package pass) and the
// printer (package print) import it, and it imports only the primitives of the Exec stage.
package tsprim

import "syntaxir.dev/sxir/internal/ir/code/exec/prim"

// The primitive operations of the TypeScript target that the passes make.
const (
	MergeOp  = "ts.unisolateMerge"
	FailedOp = "ts.failed"
)

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

// printed holds the primitives that the TypeScript printer prints: the primitives of the Exec stage
// that a TypeScript table calls and the operations of the TypeScript passes. The others come from
// the typed table, from the forms of the Flow passes that only Go prints, or from the Go stream. The
// analysis ts-printable rejects a table that calls another; the printer's tests tie its dispatch to
// this set in both directions.
var printed = map[string]bool{
	// Effects.
	prim.OpStep: true, prim.OpSilent: true, prim.OpAdvance: true, prim.OpExpect: true, prim.OpRestore: true,
	prim.OpMergeExpected: true, prim.OpMergeRenamed: true, prim.OpAppendError: true, prim.OpPushKid: true,
	prim.OpDropKids: true, prim.OpAppend: true, prim.OpSetCapture: true, prim.OpClearVals: true,
	prim.OpFreeFrame: true, prim.OpDeclareNames: true, prim.OpReset: true, prim.OpDropTrail: true,
	prim.OpSkipBegin: true, prim.OpRunBegin: true, prim.OpSkipVisit: true, prim.OpSkipValue: true,
	prim.OpSkipEnd: true, prim.OpRunWalked: true, prim.OpScanEnd: true, prim.OpResumeBegin: true,
	prim.OpResumeEnd: true, prim.OpResumeFinish: true, prim.OpObserveSkip: true, prim.OpSetEnv: true,
	prim.OpSetFrame: true, prim.OpSkippedCalls: true,
	// The demands of the table of a stream, which its resumable functions print as yields.
	prim.OpInputDemand: true,
	// Values.
	prim.OpEnv: true, prim.OpFrame: true, prim.OpRecLen: true, prim.OpKidsLen: true, prim.OpSkipHit: true,
	prim.OpRunHot: true, prim.OpSkipKids: true, prim.OpScanReach: true, prim.OpScanKids: true,
	prim.OpResumeOn: true, prim.OpResumeStart: true, prim.OpResumeMore: true, prim.OpResumeNext: true,
	prim.OpPeek: true, prim.OpMatchLit: true, prim.OpGuardFails: true, prim.OpGuardLitFails: true,
	prim.OpInClass: true, prim.OpAtAnchor: true, prim.OpAtEnd: true, prim.OpCallRule: true, prim.OpCallOutline: true,
	prim.OpIsolate: true, prim.OpUnisolate: true, prim.OpKeep: true, prim.OpMakeError: true,
	prim.OpNewErrorNode: true, prim.OpText: true, prim.OpLiteralText: true, prim.OpStr: true,
	prim.OpNewTerminalNode: true, prim.OpNewBranchNode: true, prim.OpNewVals: true, prim.OpKids: true,
	prim.OpShapeSingle: true, prim.OpShapeCapture: true, prim.OpScopeValue: true, prim.OpAttachFrame: true,
	prim.OpMarkState: true, prim.OpMarkPos: true, prim.OpNewFrame: true, prim.OpFrameVal: true,
	prim.OpPred: true, prim.OpBind: true, prim.OpInDocument: true,
	// The operations of the TypeScript passes.
	MergeOp: true, FailedOp: true,
}

// Printed reports whether the TypeScript printer prints a call of the primitive name.
func Printed(name string) bool { return printed[name] }

// declared holds the types of the variables that the TypeScript printer declares: a mark of the whole
// parser state is four variables, the others one each. An environment or the context of a typed
// action is never a variable of a TypeScript function.
var declared = map[prim.Type]bool{
	prim.TypeInt: true, prim.TypeBool: true, prim.TypeRune: true, prim.TypeValue: true, prim.TypeExpMark: true,
	prim.TypeExpIDs: true, prim.TypeVals: true, prim.TypeMark: true, prim.TypeFrame: true, prim.TypeError: true,
	prim.TypeRun: true,
}

// Declared reports whether the TypeScript printer declares a variable of the type t.
func Declared(t prim.Type) bool { return declared[t] }
