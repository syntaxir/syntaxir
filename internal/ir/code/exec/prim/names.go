package prim

import (
	"maps"
	"reflect"
	"slices"

	"syntaxir.dev/sxir/internal/ir/core"
)

// Component is the state component of the primitives: the state of the runtime that each target
// implements.
const Component = "runtime"

// Names of the primitives. Each stands for the Flow operation of the same name (flow.<name> becomes
// runtime.<name>), whose signature, contract, cost, laws and immediate it has (design record 073).
const (
	OpStep            = "runtime.step"
	OpSilent          = "runtime.silent"
	OpPeek            = "runtime.peek"
	OpAdvance         = "runtime.advance"
	OpExpect          = "runtime.expect"
	OpSkippedCalls    = "runtime.skippedCalls"
	OpInDocument      = "runtime.inDocument"
	OpObserveSkip     = "runtime.observeSkip"
	OpRestore         = "runtime.restore"
	OpMergeExpected   = "runtime.mergeExpected"
	OpMergeRenamed    = "runtime.mergeRenamed"
	OpPushKid         = "runtime.pushKid"
	OpDropKids        = "runtime.dropKids"
	OpStreamStart     = "runtime.streamStart"
	OpSkipBegin       = "runtime.skipBegin"
	OpSkipVisit       = "runtime.skipVisit"
	OpSkipEnd         = "runtime.skipEnd"
	OpRunBegin        = "runtime.runBegin"
	OpRunWalked       = "runtime.runWalked"
	OpSkipValue       = "runtime.skipValue"
	OpScanEnd         = "runtime.scanEnd"
	OpEmitStream      = "runtime.emitStream"
	OpAppend          = "runtime.append"
	OpSetWhere        = "runtime.setWhere"
	OpDropCreated     = "runtime.dropCreated"
	OpUnfresh         = "runtime.unfresh"
	OpReset           = "runtime.reset"
	OpSetCapture      = "runtime.setCapture"
	OpClearVals       = "runtime.clearVals"
	OpDropTrail       = "runtime.dropTrail"
	OpFreeFrame       = "runtime.freeFrame"
	OpAppendError     = "runtime.appendError"
	OpDeclareNames    = "runtime.declareNames"
	OpEnv             = "runtime.env"
	OpSetEnv          = "runtime.setEnv"
	OpFrame           = "runtime.frame"
	OpSetFrame        = "runtime.setFrame"
	OpRecLen          = "runtime.recLen"
	OpKidsLen         = "runtime.kidsLen"
	OpStreamOn        = "runtime.streamOn"
	OpSkipHit         = "runtime.skipHit"
	OpRunHot          = "runtime.runHot"
	OpSkipKids        = "runtime.skipKids"
	OpScanReach       = "runtime.scanReach"
	OpScanKids        = "runtime.scanKids"
	OpResumeOn        = "runtime.resumeOn"
	OpResumeStart     = "runtime.resumeStart"
	OpResumeMore      = "runtime.resumeMore"
	OpResumeNext      = "runtime.resumeNext"
	OpResumeBegin     = "runtime.resumeBegin"
	OpResumeEnd       = "runtime.resumeEnd"
	OpResumeFinish    = "runtime.resumeFinish"
	OpHasLit          = "runtime.hasLit"
	OpMatchLit        = "runtime.matchLit"
	OpMatchClass      = "runtime.matchClass"
	OpInClass         = "runtime.inClass"
	OpGuardFails      = "runtime.guardFails"
	OpGuardLitFails   = "runtime.guardLitFails"
	OpAtAnchor        = "runtime.atAnchor"
	OpSkipTerminal    = "runtime.skipTerminal"
	OpSkipClasses     = "runtime.skipClasses"
	OpCallRule        = "runtime.callRule"
	OpCallOutline     = "runtime.callOutline"
	OpIsolate         = "runtime.isolate"
	OpUnisolate       = "runtime.unisolate"
	OpText            = "runtime.text"
	OpLiteralText     = "runtime.literalText"
	OpStr             = "runtime.str"
	OpNewMatch        = "runtime.newMatch"
	OpNewNode         = "runtime.newNode"
	OpNewTerminalNode = "runtime.newTerminalNode"
	OpNewBranchNode   = "runtime.newBranchNode"
	OpNewVals         = "runtime.newVals"
	OpKids            = "runtime.kids"
	OpShapeSingle     = "runtime.shapeSingle"
	OpShapeCapture    = "runtime.shapeCapture"
	OpScopeValue      = "runtime.scopeValue"
	OpAttachCaptures  = "runtime.attachCaptures"
	OpAttachFrame     = "runtime.attachFrame"
	OpMarkState       = "runtime.markState"
	OpMarkPos         = "runtime.markPos"
	OpNewFrame        = "runtime.newFrame"
	OpFrameVal        = "runtime.frameVal"
	OpKeep            = "runtime.keep"
	OpMakeError       = "runtime.makeError"
	OpErrorValue      = "runtime.errorValue"
	OpNewErrorNode    = "runtime.newErrorNode"
	OpFreshNode       = "runtime.freshNode"
	OpConvertResult   = "runtime.convertResult"
	OpUseCtx          = "runtime.useCtx"
	OpPred            = "runtime.pred"
	OpBind            = "runtime.bind"
	OpEval            = "runtime.eval"
	OpCtxFinish       = "runtime.ctxFinish"
)

// imms holds the type of the immediate of each primitive, nil for a primitive without one.
var imms = map[string]reflect.Type{
	OpStep:            nil,
	OpSilent:          reflect.TypeFor[Silence](),
	OpPeek:            nil,
	OpAdvance:         nil,
	OpExpect:          reflect.TypeFor[Expectation](),
	OpSkippedCalls:    reflect.TypeFor[SkippedCalls](),
	OpInDocument:      nil,
	OpObserveSkip:     reflect.TypeFor[ObservedSkip](),
	OpRestore:         nil,
	OpMergeExpected:   nil,
	OpMergeRenamed:    reflect.TypeFor[ExpID](),
	OpPushKid:         nil,
	OpDropKids:        nil,
	OpStreamStart:     nil,
	OpSkipBegin:       reflect.TypeFor[Site](),
	OpSkipVisit:       nil,
	OpSkipEnd:         nil,
	OpRunBegin:        reflect.TypeFor[RunWalk](),
	OpRunWalked:       reflect.TypeFor[Site](),
	OpSkipValue:       nil,
	OpScanEnd:         reflect.TypeFor[Site](),
	OpEmitStream:      nil,
	OpAppend:          reflect.TypeFor[Spread](),
	OpSetWhere:        reflect.TypeFor[Chars](),
	OpDropCreated:     nil,
	OpUnfresh:         nil,
	OpReset:           nil,
	OpSetCapture:      reflect.TypeFor[Slot](),
	OpClearVals:       nil,
	OpDropTrail:       nil,
	OpFreeFrame:       nil,
	OpAppendError:     nil,
	OpDeclareNames:    reflect.TypeFor[*Names](),
	OpEnv:             nil,
	OpSetEnv:          nil,
	OpFrame:           nil,
	OpSetFrame:        nil,
	OpRecLen:          nil,
	OpKidsLen:         nil,
	OpStreamOn:        nil,
	OpSkipHit:         nil,
	OpRunHot:          reflect.TypeFor[Site](),
	OpSkipKids:        nil,
	OpScanReach:       reflect.TypeFor[Site](),
	OpScanKids:        reflect.TypeFor[Site](),
	OpResumeOn:        nil,
	OpResumeStart:     reflect.TypeFor[Resume](),
	OpResumeMore:      nil,
	OpResumeNext:      nil,
	OpResumeBegin:     nil,
	OpResumeEnd:       nil,
	OpResumeFinish:    nil,
	OpHasLit:          reflect.TypeFor[Lit](),
	OpMatchLit:        reflect.TypeFor[Lit](),
	OpMatchClass:      reflect.TypeFor[ClassID](),
	OpInClass:         reflect.TypeFor[ClassID](),
	OpGuardFails:      reflect.TypeFor[Guard](),
	OpGuardLitFails:   reflect.TypeFor[Guard](),
	OpAtAnchor:        reflect.TypeFor[Anchor](),
	OpSkipTerminal:    reflect.TypeFor[*core.Terminal](),
	OpSkipClasses:     reflect.TypeFor[ClassRun](),
	OpCallRule:        reflect.TypeFor[RuleCall](),
	OpCallOutline:     reflect.TypeFor[Outline](),
	OpIsolate:         nil,
	OpUnisolate:       nil,
	OpText:            nil,
	OpLiteralText:     reflect.TypeFor[Chars](),
	OpStr:             reflect.TypeFor[Chars](),
	OpNewMatch:        nil,
	OpNewNode:         reflect.TypeFor[List](),
	OpNewTerminalNode: nil,
	OpNewBranchNode:   reflect.TypeFor[List](),
	OpNewVals:         reflect.TypeFor[Room](),
	OpKids:            nil,
	OpShapeSingle:     reflect.TypeFor[Shape](),
	OpShapeCapture:    reflect.TypeFor[Shape](),
	OpScopeValue:      reflect.TypeFor[Shape](),
	OpAttachCaptures:  reflect.TypeFor[Attach](),
	OpAttachFrame:     reflect.TypeFor[Attach](),
	OpMarkState:       reflect.TypeFor[StateMark](),
	OpMarkPos:         nil,
	OpNewFrame:        reflect.TypeFor[*Names](),
	OpFrameVal:        reflect.TypeFor[Slot](),
	OpKeep:            nil,
	OpMakeError:       nil,
	OpErrorValue:      nil,
	OpNewErrorNode:    nil,
	OpFreshNode:       nil,
	OpConvertResult:   reflect.TypeFor[Conversion](),
	OpUseCtx:          nil,
	OpPred:            reflect.TypeFor[Predicate](),
	OpBind:            reflect.TypeFor[Binding](),
	OpEval:            reflect.TypeFor[Action](),
	OpCtxFinish:       nil,
}

// ImmType returns the Go type of the immediate of the primitive name, nil if it has none, and whether
// name is a primitive.
func ImmType(name string) (reflect.Type, bool) {
	t, ok := imms[name]
	return t, ok
}

// IsOp reports whether name is the name of a primitive.
func IsOp(name string) bool {
	_, ok := imms[name]
	return ok
}

// OpNames returns the names of the primitives, sorted.
func OpNames() []string { return slices.Sorted(maps.Keys(imms)) }
