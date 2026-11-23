package vmlower

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	flowpkg "syntaxir.dev/sxir/internal/ir/code/flow"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// opPatterns are the patterns of one operation: the instruction of each Flow operation that the
// bytecode Flow bodies have. Each instruction is one Flow operation whose charges and effects its VM
// body reproduces. opIndex holds the index of the pattern of each operation. Both are set by init,
// since the selection of an operation's arguments selects the operations they call.
var (
	opPatterns []pattern
	opIndex    map[string]int
)

func init() {
	opPatterns = []pattern{
		{Op: flowpkg.OpMarkState, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowMark, Dst: dst})
			return nil
		}},
		{Op: flowpkg.OpReset, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowReset, Args: a[:1]})
			return nil
		}},
		{Op: flowpkg.OpPeek, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			if len(dst) != 3 {
				return fmt.Errorf("flow.peek with %d results", len(dst))
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowPeek, Dst: dst})
			return nil
		}},
		{Op: flowpkg.OpGuardFails, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			g, err := s.guardOf(c.Imm.(*ir.First))
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowGuard, Dst: dst, Args: a, Imm: g})
			return nil
		}},
		{Op: flowpkg.OpGuardLitFails, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			g, err := litGuardOf(c.Imm.(*ir.First))
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowGuardLit, Dst: dst, Imm: g})
			return nil
		}},
		{Op: flowpkg.OpExpect, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowExpect, Args: a[:1], Imm: s.expectOf(c)})
			return nil
		}},
		{Op: flowpkg.OpUnfresh, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowUnfresh, Args: a})
			return nil
		}},
		{Op: flowpkg.OpCallRule, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			x, err := s.ruleCallOf(c)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowCall, Dst: dst, Imm: x})
			return nil
		}},
		{Op: flowpkg.OpStep, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			// The step of the innermost loop, whose register holds the position of its last step.
			var r int
			ok := len(s.loops) > 0
			if ok {
				r, ok = s.progress[s.loops[len(s.loops)-1]]
			}
			if !ok {
				return fmt.Errorf("flow.step outside the body of a loop")
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowStep, Dst: []int{r}, Args: []int{r}})
			return nil
		}},
		{Op: flowpkg.OpSilent, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			x := c.Imm.(prim.Silence)
			if x.Delta != 1 && x.Delta != -1 {
				return fmt.Errorf("flow.silent by %d", x.Delta)
			}
			// A lookahead that is no observation has no site.
			site := -1
			if x.Site != 0 {
				site = x.Site
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowSilent, Imm: bytecode.FlowSilenceOf{Delta: x.Delta, Site: site, Ok: x.Delta == -1 && x.Ok}})
			return nil
		}},
		{Op: flowpkg.OpAdvance, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowAdvance, Args: a})
			return nil
		}},
		{Op: flowpkg.OpInClass, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowInClass, Dst: dst, Args: a, Imm: int(s.c.class(c.Imm.(*ir.Class)))})
			return nil
		}},
		{Op: flowpkg.OpAtAnchor, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowAtAnchor, Dst: dst, Imm: int(c.Imm.(ir.AnchorKind))})
			return nil
		}},
		{Op: flowpkg.OpMatchLit, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowMatchLit, Dst: dst, Imm: s.litOf(c.Imm.(*ir.Lit))})
			return nil
		}},
		{Op: flowpkg.OpIsolate, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowIsolate, Dst: dst})
			return nil
		}},
		{Op: flowpkg.OpUnisolate, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowUnisolate, Dst: dst, Args: a})
			return nil
		}},
		{Op: flowpkg.OpMergeExpected, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowMergeExpected, Args: a})
			return nil
		}},
		{Op: flowpkg.OpMergeRenamed, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			a, err := s.args(c.Args)
			if err != nil {
				return err
			}
			s.emit(bytecode.FlowInstr{Op: bytecode.FlowMergeRenamed, Args: a, Imm: bytecode.FlowExpectOf{Exp: s.expID(c.Imm.(ir.ExpID))}})
			return nil
		}},
		{Op: flowpkg.OpNewTerminalNode, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowNewTerminal, c, dst, nil)
		}},
		{Op: flowpkg.OpNewBranchNode, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			op := bytecode.FlowNewSeq
			if c.Imm.(prim.List) {
				op = bytecode.FlowNewList
			}
			return s.op(op, c, dst, nil)
		}},
		{Op: flowpkg.OpNewVals, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowNewVals, c, dst, int(c.Imm.(prim.Room)))
		}},
		{Op: flowpkg.OpAppend, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			op := bytecode.FlowAppend
			if c.Imm.(prim.Spread) {
				op = bytecode.FlowSpread
			}
			return s.op(op, c, dst, nil)
		}},
		{Op: flowpkg.OpText, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowText, c, dst, nil)
		}},
		{Op: flowpkg.OpLiteralText, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowLiteralText, c, dst, int(s.c.str(string(c.Imm.(prim.Chars)))))
		}},
		{Op: flowpkg.OpStr, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowStr, c, dst, int(s.c.str(string(c.Imm.(prim.Chars)))))
		}},
		{Op: flowpkg.OpKidsLen, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowKidsLen, c, dst, nil)
		}},
		{Op: flowpkg.OpKids, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowKids, c, dst, nil)
		}},
		{Op: flowpkg.OpDropKids, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowDropKids, c, dst, nil)
		}},
		{Op: flowpkg.OpPushKid, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowPushKid, c, dst, nil)
		}},
		{Op: flowpkg.OpShapeSingle, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowShapeSingle, c, dst, shapeOf(c.Imm.(ir.Shape)))
		}},
		{Op: flowpkg.OpScopeValue, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowScopeValue, c, dst, shapeOf(c.Imm.(ir.Shape)))
		}},
		{Op: flowpkg.OpShapeCapture, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowShapeCapture, c, dst, shapeOf(c.Imm.(ir.Shape)))
		}},
		{Op: flowpkg.OpSetCapture, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSetCapture, c, dst, int(c.Imm.(prim.Slot)))
		}},
		{Op: flowpkg.OpMarkPos, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowMarkPos, c, dst, nil)
		}},
		{Op: flowpkg.OpFrame, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowFrame, c, dst, nil)
		}},
		{Op: flowpkg.OpNewFrame, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowNewFrame, c, dst, s.scope(c.Imm.(*prim.Names)))
		}},
		{Op: flowpkg.OpSetFrame, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSetFrame, c, dst, nil)
		}},
		{Op: flowpkg.OpClearVals, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowClearVals, c, dst, nil)
		}},
		{Op: flowpkg.OpFrameVal, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowFrameVal, c, dst, int(c.Imm.(prim.Slot)))
		}},
		{Op: flowpkg.OpAttachFrame, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowAttachFrame, c, dst, s.scope(c.Imm.(prim.Attach).Names))
		}},
		{Op: flowpkg.OpRunHot, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowRunHot, c, dst, int(c.Imm.(prim.Site)))
		}},
		{Op: flowpkg.OpRunBegin, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			x := c.Imm.(prim.RunWalk)
			return s.op(bytecode.FlowRunBegin, c, dst, bytecode.FlowRunWalkOf{Site: x.Site, Build: x.Build, KeyEnv: x.KeyEnv})
		}},
		{Op: flowpkg.OpResumeOn, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowResumeOn, c, dst, nil)
		}},
		{Op: flowpkg.OpResumeStart, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			x := c.Imm.(prim.Resume)
			// Every negative maximum is no bound, which the instruction encodes as -1.
			return s.op(bytecode.FlowResumeStart, c, dst, bytecode.FlowResumeOf{Site: x.Site, Min: x.Min, Max: max(x.Max, -1), Shiftable: x.Shiftable})
		}},
		{Op: flowpkg.OpResumeMore, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowResumeMore, c, dst, nil)
		}},
		{Op: flowpkg.OpResumeNext, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowResumeNext, c, dst, nil)
		}},
		{Op: flowpkg.OpResumeBegin, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowResumeBegin, c, dst, nil)
		}},
		{Op: flowpkg.OpResumeEnd, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowResumeEnd, c, dst, nil)
		}},
		{Op: flowpkg.OpResumeFinish, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowResumeFinish, c, dst, nil)
		}},
		{Op: flowpkg.OpRunWalked, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowRunWalked, c, dst, int(c.Imm.(prim.Site)))
		}},
		{Op: flowpkg.OpSkipVisit, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSkipVisit, c, dst, nil)
		}},
		{Op: flowpkg.OpSkipHit, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSkipHit, c, dst, nil)
		}},
		{Op: flowpkg.OpSkipValue, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSkipValue, c, dst, nil)
		}},
		{Op: flowpkg.OpSkipEnd, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSkipEnd, c, dst, nil)
		}},
		{Op: flowpkg.OpSkipKids, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowSkipKids, c, dst, nil)
		}},
		{Op: flowpkg.OpScanReach, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowScanReach, c, dst, int(c.Imm.(prim.Site)))
		}},
		{Op: flowpkg.OpScanEnd, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowScanEnd, c, dst, int(c.Imm.(prim.Site)))
		}},
		{Op: flowpkg.OpScanKids, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowScanKids, c, dst, int(c.Imm.(prim.Site)))
		}},
		{Op: flowpkg.OpPred, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			x := c.Imm.(prim.Predicate)
			if !x.Frame {
				return fmt.Errorf("a predicate whose captures are arguments has no bytecode Flow form")
			}
			t, err := s.actionSite(x.Action, x.Names)
			if err != nil {
				return err
			}
			return s.op(bytecode.FlowPred, c, dst, t)
		}},
		{Op: flowpkg.OpBind, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			x := c.Imm.(prim.Binding)
			if !x.Frame {
				return fmt.Errorf("a binding whose captures are arguments has no bytecode Flow form")
			}
			t, err := s.actionSite(x.Action, x.Names)
			if err != nil {
				return err
			}
			return s.op(bytecode.FlowBind, c, dst, bytecode.FlowBindOf{Var: s.c.in.Run.Vars.Index(x.Var), Action: t})
		}},
		{Op: flowpkg.OpSkipBegin, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			// A walk of a skip-run site begins without values, keyed by the variables unless the site
			// is free of them. A skip-run site and a run site
			// share the numbering of the parser's walk records, so RUNBEGIN begins either.
			site := int(c.Imm.(prim.Site))
			return s.op(bytecode.FlowRunBegin, c, dst, bytecode.FlowRunWalkOf{Site: site, KeyEnv: !s.c.in.Elab.EnvFree(site)})
		}},
		{Op: flowpkg.OpRecLen, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowRecLen, c, dst, nil)
		}},
		{Op: flowpkg.OpRestore, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowRestore, c, dst, nil)
		}},
		{Op: flowpkg.OpKeep, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowKeep, c, dst, nil)
		}},
		{Op: flowpkg.OpMakeError, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowMakeError, c, dst, nil)
		}},
		{Op: flowpkg.OpAppendError, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowAppendError, c, dst, nil)
		}},
		{Op: flowpkg.OpNewErrorNode, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowNewErrorNode, c, dst, nil)
		}},
		{Op: flowpkg.OpStreamOn, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowStreamOn, c, dst, s.rule)
		}},
		{Op: flowpkg.OpStreamStart, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowStreamStart, c, dst, nil)
		}},
		{Op: flowpkg.OpEmitStream, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			return s.op(bytecode.FlowEmitStream, c, dst, nil)
		}},
		{Op: flowpkg.OpDeclareNames, Sel: func(s *flowSel, c *code.Call, dst []int) error {
			// It does nothing to the state: the names are those of the frame's scope.
			return nil
		}},
	}
	opIndex = map[string]int{}
	for i := range opPatterns {
		p := &opPatterns[i]
		p.Name = p.Op
		opIndex[p.Op] = i
	}
}

// guardOf returns the immediate of the guard g of a class (flow.guardFails).
func (s *flowSel) guardOf(g *ir.First) (bytecode.FlowGuardOf, error) {
	if g.Lit {
		return bytecode.FlowGuardOf{}, fmt.Errorf("flow.guardFails with the guard of a literal")
	}
	return bytecode.FlowGuardOf{Class: int(s.c.class(g.Class)), Depth: g.Depth}, nil
}

// litGuardOf returns the immediate of the guard g of a literal (flow.guardLitFails).
func litGuardOf(g *ir.First) (bytecode.FlowLitGuardOf, error) {
	if !g.Lit {
		return bytecode.FlowLitGuardOf{}, fmt.Errorf("flow.guardLitFails with the guard of a class")
	}
	return bytecode.FlowLitGuardOf{Rune: g.Rune, Fold: g.Fold, Depth: g.Depth}, nil
}

// expectOf returns the immediate of the expectation of the call c of flow.expect.
func (s *flowSel) expectOf(c *code.Call) bytecode.FlowExpectOf {
	x := c.Imm.(prim.Expectation)
	return bytecode.FlowExpectOf{Exp: s.expID(x.Exp), Message: x.Message}
}

// ruleCallOf returns the immediate of the call c of flow.callRule, whose rule must have the same
// name in the Core IR and in the module.
func (s *flowSel) ruleCallOf(c *code.Call) (bytecode.FlowCallOf, error) {
	x := c.Imm.(prim.RuleCall)
	if want, got := s.c.core.Rules[x.Rule].Name, s.c.ruleName(x.Rule); want != got {
		return bytecode.FlowCallOf{}, fmt.Errorf("rule %d is %s in the Core IR and %s in the module", x.Rule, want, got)
	}
	return bytecode.FlowCallOf{Rule: x.Rule, Level: x.Level}, nil
}

// litOf returns the immediate of the literal lit. Its operands are interned in a fixed order:
// the text, then the expectation.
func (s *flowSel) litOf(lit *ir.Lit) bytecode.FlowLitOf {
	of := bytecode.FlowLitOf{Fold: lit.Fold}
	if lit.Fold {
		of.Str = int(s.c.str(foldedRunes(lit.Text)))
	} else {
		of.Str = int(s.c.str(lit.Text))
	}
	of.Exp = int(program.NumFixedDescs) + int(s.c.litDesc(lit))
	return of
}
