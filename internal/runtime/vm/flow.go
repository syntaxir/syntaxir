package vm

import (
	"fmt"
	"slices"
	"unsafe"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// flowBody returns the body of rule r run from its Flow form f (bytecode.Flow) with the recursive
// model: the same entry and result as the match code's body, with the operations of f in place of
// the instructions, and each call of a rule a host call, as the match code's calls are.
func flowBody(vm *Program, f *bytecode.Flow) func(p *state.Parser, min int) (*value.Node, bool) {
	return func(p *state.Parser, min int) (*value.Node, bool) { return execFlow(vm, p, f) }
}

// execFlow runs the Flow body f in p with the recursive model.
func execFlow(vm *Program, p *state.Parser, f *bytecode.Flow) (*value.Node, bool) {
	regs := &machineOf(p).regs
	mark := regs.mark()
	fr := flowRun{f: f, regs: regs.push(f.Regs)}
	for {
		if !vm.runFlow(p, &fr) {
			regs.release(mark)
			return fr.v, fr.ok
		}
		x := fr.call()
		var v *value.Node
		var ok bool
		if r := vm.rules[x.Rule]; vm.direct[x.Rule] {
			v, ok = p.CallPlain(r, x.Level) // what the call of the match code does for such a rule
		} else {
			v, ok = p.Call(r, x.Level)
		}
		fr.resume(v, ok)
	}
}

// regStack holds the register files of the Flow bodies that a VM runs, in either model, one above the
// other as the calls nest, so that a call takes its registers without allocating them. It is a list of
// chunks that never move: a register file stays where it is while the calls it makes push theirs.
type regStack struct {
	chunks [][]any
	cur    int // the chunk the next file is taken from
	top    int // the first free register of the chunk cur
}

// regMark is the top of a regStack, which release returns to.
type regMark struct{ cur, top int }

// minRegChunk is the least number of registers of a chunk of a regStack.
const minRegChunk = 1024

func (s *regStack) mark() regMark { return regMark{s.cur, s.top} }

// push returns a register file of n registers above the files in use. The registers hold what the
// last files there held: a body reads no register before it writes it (bytecode.Flow.Unassigned).
func (s *regStack) push(n int) []any {
	if len(s.chunks) == 0 || s.top+n > len(s.chunks[s.cur]) {
		if len(s.chunks) > 0 {
			s.cur++
		}
		for s.cur < len(s.chunks) && len(s.chunks[s.cur]) < n {
			s.chunks = slices.Delete(s.chunks, s.cur, s.cur+1) // too small for this file
		}
		if s.cur == len(s.chunks) {
			s.chunks = append(s.chunks, make([]any, max(n, minRegChunk)))
		}
		s.top = 0
	}
	regs := s.chunks[s.cur][s.top : s.top+n : s.top+n]
	s.top += n
	return regs
}

// release returns the stack to the mark m, taken before the files above it were pushed.
func (s *regStack) release(m regMark) { s.cur, s.top = m.cur, m.top }

// drop empties the stack for the next parse. It keeps the first chunk, cleared so that it keeps no
// value alive, and lets the others go, so that a parse whose calls nested deeply leaves no large
// stack behind.
func (s *regStack) drop() {
	if len(s.chunks) > 0 {
		clear(s.chunks[0])
		clear(s.chunks[1:])
		s.chunks = s.chunks[:1]
	}
	s.cur, s.top = 0, 0
}

// bytes returns the bytes the stack holds.
func (s *regStack) bytes() uint64 {
	var n int
	for _, c := range s.chunks {
		n += len(c)
	}
	return uint64(n) * uint64(unsafe.Sizeof(any(nil)))
}

// flowRun is a run of a Flow body: its registers and the instruction it is at. A run that reaches a
// CALL suspends there, so that a driver can run the call (with host recursion in the recursive
// model, as a frame of its own in the iterative model) and resume it with the result. When the body
// returns, v and ok hold its result.
type flowRun struct {
	f    *bytecode.Flow
	regs []any
	pc   int
	v    *value.Node
	ok   bool
}

// call returns the immediate of the CALL or CALLJUMP that fr is suspended at.
func (fr *flowRun) call() bytecode.FlowCallOf {
	return fr.f.Code[fr.pc].Imm.(bytecode.FlowCallOf)
}

// resume stores the result of the CALL or CALLJUMP that fr is suspended at and continues after it,
// or at the target of a CALLJUMP whose call failed.
func (fr *flowRun) resume(v *value.Node, ok bool) {
	in := &fr.f.Code[fr.pc]
	fr.set(in.Dst[0], v)
	fr.set(in.Dst[1], ok)
	if !ok && in.Op == bytecode.FlowCallJump {
		fr.pc = in.Target
		return
	}
	fr.pc++
}

func (fr *flowRun) set(d int, v any) {
	if d >= 0 {
		fr.regs[d] = v
	}
}

// runFlow runs the Flow body of fr on the parser p until it returns, with its result in fr, or
// reaches a CALL, which it reports. Each operation calls the parser's primitive that the Go form
// of the same operation calls, so that results and charges agree.
func (vm *Program) runFlow(p *state.Parser, fr *flowRun) (call bool) {
	f, regs := fr.f, fr.regs
	for pc := fr.pc; pc < len(f.Code); {
		in := &f.Code[pc]
		pc++
		switch in.Op {
		case bytecode.FlowConst:
			fr.set(in.Dst[0], in.Imm)
		case bytecode.FlowMove:
			fr.set(in.Dst[0], regs[in.Args[0]])
		case bytecode.FlowNot:
			fr.set(in.Dst[0], !regs[in.Args[0]].(bool))
		case bytecode.FlowPos:
			fr.set(in.Dst[0], p.Pos)
		case bytecode.FlowSetPos:
			p.Pos = regs[in.Args[0]].(int)
		case bytecode.FlowCut:
			fr.set(in.Dst[0], p.Cut)
		case bytecode.FlowSetCut:
			p.Cut = regs[in.Args[0]].(bool)
		case bytecode.FlowJump:
			pc = in.Target
		case bytecode.FlowJumpFalse:
			if !regs[in.Args[0]].(bool) {
				pc = in.Target
			}
		case bytecode.FlowJumpTrue:
			if regs[in.Args[0]].(bool) {
				pc = in.Target
			}
		case bytecode.FlowMatchClass:
			x := in.Imm.(bytecode.FlowClassTestOf)
			ch, size, ok := p.Peek()
			fr.set(in.Dst[0], ch)
			fr.set(in.Dst[1], size)
			fr.set(in.Dst[2], ok)
			if !ok || !vm.has(p, int32(x.Class), ch) {
				expect(p, x.Expect)
				pc = in.Target
			}
		case bytecode.FlowGuardJump:
			x := in.Imm.(bytecode.FlowGuardTestOf)
			if vm.guardFails(p, x.Guard, regs[in.Args[0]].(rune), regs[in.Args[1]].(bool)) {
				expect(p, x.Expect)
				pc = in.Target
			}
		case bytecode.FlowGuardLitJump:
			x := in.Imm.(bytecode.FlowLitGuardTestOf)
			if litGuardFails(p, x.Guard) {
				expect(p, x.Expect)
				pc = in.Target
			}
		case bytecode.FlowAssertJump:
			x := in.Imm.(bytecode.FlowAnchorTestOf)
			if !atAnchor(p, x.Anchor) {
				expect(p, x.Expect)
				pc = in.Target
			}
		case bytecode.FlowMatchLitJump:
			ok := vm.flowMatchLit(p, in.Imm.(bytecode.FlowLitOf))
			fr.set(in.Dst[0], nil)
			fr.set(in.Dst[1], ok)
			if !ok {
				pc = in.Target
			}
		case bytecode.FlowReturn:
			fr.v, fr.ok = asNode(regs[in.Args[0]]), regs[in.Args[1]].(bool)
			fr.pc = len(f.Code)
			return false
		case bytecode.FlowMark:
			fr.set(in.Dst[0], p.Mark())
		case bytecode.FlowReset:
			p.Reset(regs[in.Args[0]].(state.Mark))
		case bytecode.FlowPeek:
			ch, size, ok := p.Peek()
			fr.set(in.Dst[0], ch)
			fr.set(in.Dst[1], size)
			fr.set(in.Dst[2], ok)
		case bytecode.FlowGuard:
			fr.set(in.Dst[0], vm.guardFails(p, in.Imm.(bytecode.FlowGuardOf), regs[in.Args[0]].(rune), regs[in.Args[1]].(bool)))
		case bytecode.FlowGuardLit:
			fr.set(in.Dst[0], litGuardFails(p, in.Imm.(bytecode.FlowLitGuardOf)))
		case bytecode.FlowExpect:
			p.Expect(regs[in.Args[0]].(int), expID(in.Imm.(bytecode.FlowExpectOf)))
		case bytecode.FlowCall, bytecode.FlowCallJump:
			fr.pc = pc - 1
			return true
		case bytecode.FlowUnfresh:
			if n := asNode(regs[in.Args[0]]); n != nil {
				nodes.D(n).Fresh = false
			}
		case bytecode.FlowAdd:
			fr.set(in.Dst[0], regs[in.Args[0]].(int)+regs[in.Args[1]].(int))
		case bytecode.FlowSub:
			fr.set(in.Dst[0], regs[in.Args[0]].(int)-regs[in.Args[1]].(int))
		case bytecode.FlowEq:
			fr.set(in.Dst[0], flowEqual(regs[in.Args[0]], regs[in.Args[1]]))
		case bytecode.FlowNe:
			fr.set(in.Dst[0], !flowEqual(regs[in.Args[0]], regs[in.Args[1]]))
		case bytecode.FlowLt:
			fr.set(in.Dst[0], flowInt(regs[in.Args[0]]) < flowInt(regs[in.Args[1]]))
		case bytecode.FlowLe:
			fr.set(in.Dst[0], flowInt(regs[in.Args[0]]) <= flowInt(regs[in.Args[1]]))
		case bytecode.FlowStep:
			p.Spend()
		case bytecode.FlowSilent:
			// As the match code's LOOK and END_LOOK, and its unwinding of a lookahead.
			x := in.Imm.(bytecode.FlowSilenceOf)
			if x.Delta > 0 {
				p.Silent++
				p.LookBegin(x.Site)
			} else {
				p.LookEnd(x.Ok)
				p.Silent--
			}
		case bytecode.FlowAdvance:
			p.Pos += regs[in.Args[0]].(int)
		case bytecode.FlowInClass:
			fr.set(in.Dst[0], vm.has(p, int32(in.Imm.(int)), regs[in.Args[0]].(rune)))
		case bytecode.FlowAtAnchor:
			fr.set(in.Dst[0], atAnchor(p, in.Imm.(int)))
		case bytecode.FlowMatchLit:
			fr.set(in.Dst[0], nil)
			fr.set(in.Dst[1], vm.flowMatchLit(p, in.Imm.(bytecode.FlowLitOf)))
		case bytecode.FlowIsolate:
			fr.set(in.Dst[0], p.Isolate(p.Pos))
		case bytecode.FlowUnisolate:
			far, inner := p.Unisolate(regs[in.Args[0]].(state.ExpMark))
			fr.set(in.Dst[0], far)
			fr.set(in.Dst[1], inner)
		case bytecode.FlowMergeExpected:
			exps, _ := regs[in.Args[1]].([]state.ExpID)
			p.MergeExpected(regs[in.Args[0]].(int), exps)
		case bytecode.FlowNewTerminal:
			text, _ := regs[in.Args[1]].(string)
			fr.set(in.Dst[0], p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Start: int32(regs[in.Args[0]].(int)), End: int32(p.Pos), Text: text, Terminal: true, Fresh: true}))
		case bytecode.FlowNewSeq, bytecode.FlowNewList:
			kind := valueimpl.SeqKind
			if in.Op == bytecode.FlowNewList {
				kind = valueimpl.ListKind
			}
			fr.set(in.Dst[0], p.NewNode(nodes.Data{Kind: kind, Start: int32(regs[in.Args[0]].(int)), End: int32(p.Pos), Children: flowVals(regs[in.Args[1]]), Fresh: true}))
		case bytecode.FlowNewVals:
			fr.set(in.Dst[0], p.Nodes(in.Imm.(int))[:0])
		case bytecode.FlowAppend:
			fr.set(in.Dst[0], append(flowVals(regs[in.Args[0]]), asNode(regs[in.Args[1]])))
		case bytecode.FlowSpread:
			fr.set(in.Dst[0], append(flowVals(regs[in.Args[0]]), nodes.D(asNode(regs[in.Args[1]])).Children...))
		case bytecode.FlowText:
			fr.set(in.Dst[0], p.Text(regs[in.Args[0]].(int), p.Pos))
		case bytecode.FlowLiteralText:
			fr.set(in.Dst[0], p.LiteralText(regs[in.Args[0]].(int), vm.literals[in.Imm.(int)]))
		case bytecode.FlowStr:
			// A string constant keeps its bytes: it is no matching text (nodes.LiteralValue).
			fr.set(in.Dst[0], vm.m.Strings[in.Imm.(int)])
		case bytecode.FlowKidsLen:
			fr.set(in.Dst[0], len(p.KidStack))
		case bytecode.FlowKids:
			fr.set(in.Dst[0], p.Kids(regs[in.Args[0]].(int)))
		case bytecode.FlowDropKids:
			p.TruncateKids(regs[in.Args[0]].(int))
		case bytecode.FlowPushKid:
			// As a framed body of the Go library pushes a value, uncharged.
			p.KidStack = append(p.KidStack, asNode(regs[in.Args[0]]))
		case bytecode.FlowShapeSingle:
			x := in.Imm.(bytecode.FlowShapeOf)
			v, start := asNode(regs[in.Args[0]]), regs[in.Args[1]].(int)
			switch x.Mode {
			case int(program.SinglePass):
				fr.set(in.Dst[0], flowShaper(x).One(v))
				fr.set(in.Dst[1], true)
			case int(program.SingleTuple):
				fr.set(in.Dst[0], flowShapeTuple(p, x, v, start))
				fr.set(in.Dst[1], true)
			default:
				fr.set(in.Dst[0], nil)
				fr.set(in.Dst[1], false)
			}
		case bytecode.FlowScopeValue:
			x := in.Imm.(bytecode.FlowShapeOf)
			v, start := asNode(regs[in.Args[0]]), regs[in.Args[1]].(int)
			if x.Tuple {
				fr.set(in.Dst[0], flowShapeTuple(p, x, v, start))
			} else {
				fr.set(in.Dst[0], flowShaper(x).One(v))
			}
		case bytecode.FlowSetCapture:
			p.SetCapture(flowSlot(p, p.Frame, in.Imm.(int)), asNode(regs[in.Args[0]]))
		case bytecode.FlowMarkPos:
			fr.set(in.Dst[0], regs[in.Args[0]].(state.Mark).Pos)
		case bytecode.FlowFrame:
			fr.set(in.Dst[0], p.Frame)
		case bytecode.FlowNewFrame:
			fr.set(in.Dst[0], p.NewFrame(len(vm.scopes[in.Imm.(int)])))
		case bytecode.FlowSetFrame:
			p.Frame = regs[in.Args[0]].(*state.Frame)
		case bytecode.FlowClearVals:
			clear(regs[in.Args[0]].(*state.Frame).Vals)
		case bytecode.FlowFrameVal:
			f := regs[in.Args[0]].(*state.Frame)
			fr.set(in.Dst[0], f.Vals[flowSlot(p, f, in.Imm.(int))])
		case bytecode.FlowAttachFrame:
			sc, f := vm.scopeOf[in.Imm.(int)], regs[in.Args[2]].(*state.Frame)
			if len(sc.Names) != len(f.Vals) {
				p.Fail("vm: a scope of %d names attached from a frame of %d slots", len(sc.Names), len(f.Vals))
			}
			fr.set(in.Dst[0], p.AttachCaptures(asNode(regs[in.Args[0]]), sc, f, regs[in.Args[1]].(int), p.Pos))
		case bytecode.FlowShapeCapture:
			// As the Go library's ShapeCapture: ShapeSingle, or the unit node of the range, which
			// charges a unit for the value (spec/runtime/parsing.md#cost).
			x := in.Imm.(bytecode.FlowShapeOf)
			v, start := asNode(regs[in.Args[0]]), regs[in.Args[1]].(int)
			switch x.Mode {
			case int(program.SinglePass):
				v = flowShaper(x).One(v)
			case int(program.SingleTuple):
				v = flowShapeTuple(p, x, v, start)
			default:
				v = p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Fresh: true})
			}
			fr.set(in.Dst[0], v)
		case bytecode.FlowRunHot:
			fr.set(in.Dst[0], p.RunHot(in.Imm.(int)))
		case bytecode.FlowRunBegin:
			x := in.Imm.(bytecode.FlowRunWalkOf)
			if x.Build {
				p.SkipBeginList(x.Site, x.KeyEnv)
			} else {
				p.SkipBegin(x.Site, x.KeyEnv)
			}
		case bytecode.FlowRunWalked:
			p.RunWalked(in.Imm.(int), regs[in.Args[0]].(int))
		case bytecode.FlowSkipVisit:
			p.SkipVisit()
		case bytecode.FlowSkipHit:
			fr.set(in.Dst[0], p.SkipHit())
		case bytecode.FlowSkipValue:
			p.SkipValue(asNode(regs[in.Args[0]]))
		case bytecode.FlowSkipEnd:
			p.SkipEndDrop()
		case bytecode.FlowSkipKids:
			fr.set(in.Dst[0], p.SkipKids())
		case bytecode.FlowScanReach:
			fr.set(in.Dst[0], p.ScanReach(in.Imm.(int), regs[in.Args[0]].(int), regs[in.Args[1]].(int)))
		case bytecode.FlowScanEnd:
			p.ScanEnd(in.Imm.(int), regs[in.Args[0]].(int), regs[in.Args[1]].(int))
		case bytecode.FlowScanKids:
			fr.set(in.Dst[0], p.ScanKids(in.Imm.(int), regs[in.Args[0]].(int), regs[in.Args[1]].(int), regs[in.Args[2]].(int)))
		case bytecode.FlowPred:
			// As the match code's PRED, in the current frame.
			fr.set(in.Dst[0], vm.predicate(p, in.Imm.(int)))
		case bytecode.FlowBind:
			// As the match code's BIND, in the current frame.
			x := in.Imm.(bytecode.FlowBindOf)
			fr.set(in.Dst[0], vm.bind(p, x.Var, x.Action))
		case bytecode.FlowKeep:
			// As the match code keeps the expectations of a failed recovery body.
			exps, _ := regs[in.Args[0]].([]state.ExpID)
			fr.set(in.Dst[0], p.Keep(exps))
		case bytecode.FlowMakeError:
			exps, _ := regs[in.Args[1]].([]state.ExpID)
			fr.set(in.Dst[0], p.MakeError(regs[in.Args[0]].(int), exps))
		case bytecode.FlowAppendError:
			p.PushRecovered(regs[in.Args[0]].(*value.SyntaxError))
		case bytecode.FlowNewErrorNode:
			fr.set(in.Dst[0], errorNode(p, regs[in.Args[0]].(int), regs[in.Args[1]].(*value.SyntaxError)))
		case bytecode.FlowRecLen:
			fr.set(in.Dst[0], len(p.Recovered))
		case bytecode.FlowRestore:
			// As the Go library's Restore.
			p.Pos = regs[in.Args[0]].(int)
			p.Recovered = p.Recovered[:regs[in.Args[1]].(int)]
		case bytecode.FlowStreamOn:
			r := vm.rules[in.Imm.(int)]
			fr.set(in.Dst[0], p.StreamsIn(r.ID, r.Rec))
		case bytecode.FlowStreamStart:
			// As the match code's REPEAT of a #stream repetition.
			if state.StreamPrefixIsolation {
				p.StartStreamChunks()
			}
		case bytecode.FlowEmitStream:
			p.EmitStream(asNode(regs[in.Args[0]]), p.Pos)
		case bytecode.FlowResumeOn:
			fr.set(in.Dst[0], p.Runs != nil && p.Silent == 0)
		case bytecode.FlowResumeStart:
			// A run lives until its repetition ends, which may be after nested calls that start runs
			// of their own, so each has its own state.
			x, r := in.Imm.(bytecode.FlowResumeOf), &state.RunState{}
			p.RunStart(r, x.Site, x.Min, x.Max, x.Shiftable)
			fr.set(in.Dst[0], r)
		case bytecode.FlowResumeMore:
			fr.set(in.Dst[0], p.RunMore(regs[in.Args[0]].(*state.RunState)))
		case bytecode.FlowResumeNext:
			v, ok := p.RunNext(regs[in.Args[0]].(*state.RunState))
			fr.set(in.Dst[0], v)
			fr.set(in.Dst[1], ok)
		case bytecode.FlowResumeBegin:
			p.RunBegin(regs[in.Args[0]].(*state.RunState))
		case bytecode.FlowResumeEnd:
			p.RunEnd(regs[in.Args[0]].(*state.RunState), regs[in.Args[1]].(bool), asNode(regs[in.Args[2]]))
		case bytecode.FlowResumeFinish:
			p.RunFinish(regs[in.Args[0]].(*state.RunState))
		default:
			panic(fmt.Sprintf("internal error: vm: flow operation %d", in.Op))
		}
	}
	// A loaded body ends with JUMP or RETURN (bytecode.ValidateModule), so it never gets here.
	panic("internal error: vm: a Flow body ran past its end")
}

// expID returns the parser's ID of the expectation x.
func expID(x bytecode.FlowExpectOf) state.ExpID {
	id := state.ExpID(x.Exp)
	if x.Message {
		id |= state.MsgBit
	}
	return id
}

// expect records the expectation x at the position (flow.expect).
func expect(p *state.Parser, x bytecode.FlowExpectOf) { p.Expect(p.Pos, expID(x)) }

// guardFails reports whether the guard g of a class fails for the character ch, read if ok
// (flow.guardFails): the character is not in the class and the alternative would not nest too deep.
func (vm *Program) guardFails(p *state.Parser, g bytecode.FlowGuardOf, ch rune, ok bool) bool {
	hit := ok && vm.has(p, int32(g.Class), ch)
	return !hit && p.Depth+g.Depth <= p.MaxDepth
}

// litGuardFails reports whether the guard g of a literal fails (flow.guardLitFails): the input at the
// position does not begin with the literal's first character, compared as the literal compares it
// (state.Parser.MatchChar), and the alternative would not nest too deep.
func litGuardFails(p *state.Parser, g bytecode.FlowLitGuardOf) bool {
	_, hit := p.MatchChar(g.Rune, g.Fold)
	return !hit && p.Depth+g.Depth <= p.MaxDepth
}

// atAnchor reports whether the position is at the anchor of an immediate (flow.atAnchor).
func atAnchor(p *state.Parser, anchor int) bool {
	switch anchor {
	case 0:
		return p.AtInputStart()
	case 1:
		return p.AtEOF()
	case 2:
		return p.AtLineStart()
	}
	return p.AtLineEnd()
}

// flowMatchLit matches the literal of x at the position and reports whether it matched (flow.matchLit):
// a failure records the literal's expectation and leaves the position where it was.
func (vm *Program) flowMatchLit(p *state.Parser, x bytecode.FlowLitOf) bool {
	start := p.Pos
	var ok bool
	if x.Fold {
		ok = p.MatchLiteralFold(vm.runes[x.Str])
	} else {
		ok = p.MatchLiteral(vm.runes[x.Str], vm.bytes[x.Str])
	}
	if !ok {
		p.Expect(start, state.ExpID(x.Exp))
		p.Pos = start
	}
	return ok
}

// flowSlot returns the capture slot of the frame f that an instruction names, and fails the parse
// if f has no such slot: a loaded body names slots of the frames it may run with
// (bytecode.ValidateModule), but which frame is current is known only when it runs.
func flowSlot(p *state.Parser, f *state.Frame, slot int) int {
	if slot >= len(f.Vals) {
		p.Fail("vm: capture slot %d of a frame of %d slots", slot, len(f.Vals))
	}
	return slot
}

// asNode returns the node held in a register, nil for the null value.
func asNode(v any) *value.Node {
	n, _ := v.(*value.Node)
	return n
}

// flowInt returns the integer that a register holds: an int or a character.
func flowInt(v any) int {
	if r, ok := v.(rune); ok {
		return int(r)
	}
	return v.(int)
}

// flowEqual reports whether two registers hold equal values; an int and a character compare as
// integers.
func flowEqual(a, b any) bool {
	switch a.(type) {
	case int, rune:
		switch b.(type) {
		case int, rune:
			return flowInt(a) == flowInt(b)
		}
	}
	return a == b
}

// flowVals returns the list of values that a register holds; a register of a list never set holds
// nil.
func flowVals(v any) []*value.Node {
	vs, _ := v.([]*value.Node)
	return vs
}

func flowShaper(x bytecode.FlowShapeOf) program.Shaper {
	return program.Shaper{Elems: x.Elems, Shared: x.Shared, K: x.K}
}

// flowShapeTuple returns the tuple node of the result v of a match from start, as the Go library's
// shapeTuple does.
func flowShapeTuple(p *state.Parser, x bytecode.FlowShapeOf, v *value.Node, start int) *value.Node {
	if x.Elems && !(x.Shared && v != nil) {
		return v
	}
	return state.ShapeTuple(p, flowShaper(x), v, start)
}
