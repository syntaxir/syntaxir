package bytecode_test

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// flowModule returns a module of two rules, the second with a Flow body that uses every operation
// and every kind of immediate, and a class for its guard and class test. The instructions after
// the first RETURN are unreachable; they are there for the encoding.
func flowModule() *bytecode.Module {
	f := &bytecode.Flow{Regs: 8, Code: []bytecode.FlowInstr{
		{Op: bytecode.FlowConst, Dst: []int{0}},
		{Op: bytecode.FlowConst, Dst: []int{1}, Imm: -1 << 40},
		{Op: bytecode.FlowConst, Dst: []int{2}, Imm: true},
		{Op: bytecode.FlowConst, Dst: []int{3}, Imm: false},
		{Op: bytecode.FlowPos, Dst: []int{1}},
		{Op: bytecode.FlowMark, Dst: []int{4}},
		{Op: bytecode.FlowPeek, Dst: []int{5, -1, 6}},
		{Op: bytecode.FlowGuardLit, Dst: []int{7}, Imm: bytecode.FlowLitGuardOf{Rune: 'é', Fold: true, Depth: 2}},
		{Op: bytecode.FlowGuard, Dst: []int{7}, Args: []int{5, 6}, Imm: bytecode.FlowGuardOf{Class: 0}},
		{Op: bytecode.FlowJumpFalse, Args: []int{7}, Target: 14},
		{Op: bytecode.FlowExpect, Args: []int{1}, Imm: bytecode.FlowExpectOf{Exp: bytecode.FixedExps + 1, Message: true}},
		{Op: bytecode.FlowReset, Args: []int{4}},
		{Op: bytecode.FlowSetPos, Args: []int{1}},
		{Op: bytecode.FlowJump, Target: 17},
		{Op: bytecode.FlowCall, Dst: []int{0, 7}, Imm: bytecode.FlowCallOf{Rule: 0, Level: 3}},
		{Op: bytecode.FlowUnfresh, Args: []int{0}},
		{Op: bytecode.FlowNot, Dst: []int{2}, Args: []int{7}},
		{Op: bytecode.FlowMove, Dst: []int{3}, Args: []int{2}},
		{Op: bytecode.FlowReturn, Args: []int{0, 3}},
		{Op: bytecode.FlowAdd, Dst: []int{1}, Args: []int{1, 1}},
		{Op: bytecode.FlowSub, Dst: []int{1}, Args: []int{1, 1}},
		{Op: bytecode.FlowEq, Dst: []int{2}, Args: []int{1, 1}},
		{Op: bytecode.FlowNe, Dst: []int{2}, Args: []int{1, 1}},
		{Op: bytecode.FlowLt, Dst: []int{2}, Args: []int{5, 1}},
		{Op: bytecode.FlowLe, Dst: []int{2}, Args: []int{1, 1}},
		{Op: bytecode.FlowStep, Dst: []int{1}, Args: []int{1}},
		{Op: bytecode.FlowSilent, Imm: bytecode.FlowSilenceOf{Delta: 1, Site: 3}},
		{Op: bytecode.FlowSilent, Imm: bytecode.FlowSilenceOf{Delta: -1, Site: -1, Ok: true}},
		{Op: bytecode.FlowAdvance, Args: []int{1}},
		{Op: bytecode.FlowInClass, Dst: []int{2}, Args: []int{5}, Imm: 0},
		{Op: bytecode.FlowAtAnchor, Dst: []int{2}, Imm: 3},
		{Op: bytecode.FlowMatchLit, Dst: []int{0, 2}, Imm: bytecode.FlowLitOf{Str: 0, Exp: bytecode.FixedExps, Fold: true}},
		{Op: bytecode.FlowIsolate, Dst: []int{4}},
		{Op: bytecode.FlowUnisolate, Dst: []int{1, 6}, Args: []int{4}},
		{Op: bytecode.FlowMergeExpected, Args: []int{1, 6}},
		{Op: bytecode.FlowNewVals, Dst: []int{4}, Imm: 2},
		{Op: bytecode.FlowText, Dst: []int{0}, Args: []int{1}},
		{Op: bytecode.FlowNewTerminal, Dst: []int{0}, Args: []int{1, 0}},
		{Op: bytecode.FlowAppend, Dst: []int{4}, Args: []int{4, 0}},
		{Op: bytecode.FlowSpread, Dst: []int{4}, Args: []int{4, 0}},
		{Op: bytecode.FlowNewSeq, Dst: []int{0}, Args: []int{1, 4}},
		{Op: bytecode.FlowNewList, Dst: []int{0}, Args: []int{1, 4}},
		{Op: bytecode.FlowLiteralText, Dst: []int{0}, Args: []int{1}, Imm: 0},
		{Op: bytecode.FlowStr, Dst: []int{0}, Imm: 1},
		{Op: bytecode.FlowKidsLen, Dst: []int{1}},
		{Op: bytecode.FlowPushKid, Args: []int{0}},
		{Op: bytecode.FlowKids, Dst: []int{4}, Args: []int{1}},
		{Op: bytecode.FlowDropKids, Args: []int{1}},
		{Op: bytecode.FlowShapeSingle, Dst: []int{0, 2}, Args: []int{0, 1}, Imm: bytecode.FlowShapeOf{Elems: true, Shared: true, K: 2, Mode: 2}},
		{Op: bytecode.FlowScopeValue, Dst: []int{0}, Args: []int{0, 1}, Imm: bytecode.FlowShapeOf{Tuple: true, K: 1}},
		{Op: bytecode.FlowSetCapture, Args: []int{0}, Imm: 0},
		{Op: bytecode.FlowMarkPos, Dst: []int{1}, Args: []int{4}},
		{Op: bytecode.FlowFrame, Dst: []int{5}},
		{Op: bytecode.FlowNewFrame, Dst: []int{6}, Imm: 0},
		{Op: bytecode.FlowSetFrame, Args: []int{6}},
		{Op: bytecode.FlowClearVals, Args: []int{6}},
		{Op: bytecode.FlowFrameVal, Dst: []int{0}, Args: []int{6}, Imm: 1},
		{Op: bytecode.FlowAttachFrame, Dst: []int{0}, Args: []int{0, 1, 6}, Imm: 0},
		{Op: bytecode.FlowShapeCapture, Dst: []int{0}, Args: []int{0, 1}, Imm: bytecode.FlowShapeOf{K: 1, Mode: 1}},
		{Op: bytecode.FlowRunHot, Dst: []int{2}, Imm: 1},
		{Op: bytecode.FlowRunBegin, Imm: bytecode.FlowRunWalkOf{Site: 1, Build: true, KeyEnv: true}},
		{Op: bytecode.FlowSkipVisit},
		{Op: bytecode.FlowSkipHit, Dst: []int{2}},
		{Op: bytecode.FlowSkipValue, Args: []int{0}},
		{Op: bytecode.FlowSkipKids, Dst: []int{4}},
		{Op: bytecode.FlowSkipEnd},
		{Op: bytecode.FlowRunWalked, Args: []int{1}, Imm: 1},
		{Op: bytecode.FlowScanReach, Dst: []int{1}, Args: []int{1, 1}, Imm: 5},
		{Op: bytecode.FlowScanEnd, Args: []int{1, 1}, Imm: 4},
		{Op: bytecode.FlowScanKids, Dst: []int{4}, Args: []int{1, 1, 1}, Imm: 5},
		{Op: bytecode.FlowPred, Dst: []int{2}, Imm: 0},
		{Op: bytecode.FlowBind, Dst: []int{2}, Imm: bytecode.FlowBindOf{Var: 0, Action: 0}},
		{Op: bytecode.FlowCut, Dst: []int{2}},
		{Op: bytecode.FlowSetCut, Args: []int{2}},
		{Op: bytecode.FlowKeep, Dst: []int{6}, Args: []int{6}},
		{Op: bytecode.FlowMakeError, Dst: []int{7}, Args: []int{1, 6}},
		{Op: bytecode.FlowAppendError, Args: []int{7}},
		{Op: bytecode.FlowNewErrorNode, Dst: []int{0}, Args: []int{1, 7}},
		{Op: bytecode.FlowRecLen, Dst: []int{1}},
		{Op: bytecode.FlowRestore, Args: []int{1, 1}},
		{Op: bytecode.FlowJumpTrue, Args: []int{2}, Target: 17},
		{Op: bytecode.FlowMatchClass, Dst: []int{5, -1, 6}, Target: 17, Imm: bytecode.FlowClassTestOf{Class: 0, Expect: bytecode.FlowExpectOf{Exp: 2}}},
		{Op: bytecode.FlowGuardJump, Args: []int{5, 6}, Target: 17, Imm: bytecode.FlowGuardTestOf{
			Guard: bytecode.FlowGuardOf{Class: 0, Depth: 1}, Expect: bytecode.FlowExpectOf{Exp: bytecode.FixedExps, Message: true}}},
		{Op: bytecode.FlowGuardLitJump, Target: 17, Imm: bytecode.FlowLitGuardTestOf{
			Guard: bytecode.FlowLitGuardOf{Rune: 'x', Depth: 1}, Expect: bytecode.FlowExpectOf{Exp: bytecode.FixedExps, Message: true}}},
		{Op: bytecode.FlowAssertJump, Target: 17, Imm: bytecode.FlowAnchorTestOf{Anchor: 2, Expect: bytecode.FlowExpectOf{Exp: 3}}},
		{Op: bytecode.FlowMatchLitJump, Dst: []int{0, 2}, Target: 17, Imm: bytecode.FlowLitOf{Str: 1, Exp: bytecode.FixedExps + 1}},
		{Op: bytecode.FlowCallJump, Dst: []int{0, 7}, Target: 17, Imm: bytecode.FlowCallOf{Rule: 0, Level: 1}},
		{Op: bytecode.FlowStreamOn, Dst: []int{2}, Imm: 1},
		{Op: bytecode.FlowStreamStart},
		{Op: bytecode.FlowEmitStream, Args: []int{0}},
		{Op: bytecode.FlowResumeOn, Dst: []int{2}},
		{Op: bytecode.FlowResumeStart, Dst: []int{4}, Imm: bytecode.FlowResumeOf{Site: 3, Min: 1, Max: -1, Shiftable: true}},
		{Op: bytecode.FlowResumeMore, Dst: []int{2}, Args: []int{4}},
		{Op: bytecode.FlowResumeNext, Dst: []int{0, 2}, Args: []int{4}},
		{Op: bytecode.FlowResumeBegin, Args: []int{4}},
		{Op: bytecode.FlowResumeEnd, Args: []int{4, 2, 0}},
		{Op: bytecode.FlowResumeFinish, Args: []int{4}},
		{Op: bytecode.FlowMergeRenamed, Args: []int{1, 6}, Imm: bytecode.FlowExpectOf{Exp: bytecode.FixedExps + 1}},
		{Op: bytecode.FlowReturn, Args: []int{0, 3}},
	}}
	return &bytecode.Module{
		Strings: []string{"a", "b", "[a-z]"},
		Classes: []bytecode.Class{{Ranges: []rune{'a', 'z'}, Expectation: 2}},
		Rules: []bytecode.RuleInfo{
			{Name: 0, Action: -1, NodeType: -1, OperatorsRule: -1, Where: -1},
			{Name: 1, Action: -1, NodeType: -1, OperatorsRule: -1, Where: -1, Scope: []int{0}},
		},
		Scopes:  [][]int{{0, 1}},
		RunKeys: []bytecode.RunKeyInfo{{Site: 1}},
		Starts:  []int{0},
		Actions: []bytecode.Action{{}},
		Vars:    []bytecode.VarInfo{{Str: -1}},
		Flows:   map[int]*bytecode.Flow{0: matchNothing(), 1: f},
	}
}

// matchNothing returns a Flow body that matches the empty input with the null value.
func matchNothing() *bytecode.Flow {
	return &bytecode.Flow{Regs: 2, Code: []bytecode.FlowInstr{
		{Op: bytecode.FlowConst, Dst: []int{0}},
		{Op: bytecode.FlowConst, Dst: []int{1}, Imm: true},
		{Op: bytecode.FlowReturn, Args: []int{0, 1}},
	}}
}

func writeModule(m *bytecode.Module) []byte {
	w := &bytecode.Encoder{Strs: map[string]int{}}
	w.WriteModule(m)
	return w.Body.Bytes()
}

func noFieldTypes(string) error { return nil }

// TestFlowBodyEncoding checks that a Flow body survives writing and reading with every operation
// and kind of immediate, that the loaded module validates, and that reading rejects an unknown
// operation, a constant of an unknown kind or a value its kind does not have, a rule with two
// bodies and bodies out of rule order, which Encoder never writes. The module has no stream body; TestStreamBodyEncoding checks those.
func TestFlowBodyEncoding(t *testing.T) {
	m := flowModule()
	if err := bytecode.ValidateModule(m, noFieldTypes); err != nil {
		t.Fatal(err)
	}
	data := writeModule(m)
	r := &bytecode.Decoder{Data: data, Strs: m.Strings}
	got := r.ReadModule()
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Pos != len(data) {
		t.Fatalf("read %d of %d bytes", r.Pos, len(data))
	}
	if !reflect.DeepEqual(got.Flows, m.Flows) {
		t.Fatalf("flows read back as %+v, want %+v", got.Flows[1], m.Flows[1])
	}
	if err := bytecode.ValidateModule(got, noFieldTypes); err != nil {
		t.Fatal(err)
	}
	// The body is written last but for the count of stream bodies, 0: rule 1 (zigzag 2), 8
	// registers, then its instructions; the last is RETURN with the arguments 0 and 3 (zigzag 0
	// and 6).
	end := []byte{byte(bytecode.FlowReturn), 0, 6, 0}
	if !bytes.HasSuffix(data, end) {
		t.Fatalf("the module does not end with the RETURN of the body and no stream body: % x", data[len(data)-4:])
	}
	head := bytes.LastIndex(data, []byte{2, 8, byte(len(m.Flows[1].Code))})
	if head < 0 {
		t.Fatal("no header of the body")
	}
	// A body with one instruction, a CONST of each tail, so that the bytes after the header are
	// the instruction alone.
	one := func(in ...byte) []byte {
		return append(append(bytes.Clone(data[:head]), 2, 8, 1), in...)
	}
	// The body of rule 1 twice, as the second and third of three bodies.
	body := data[head : len(data)-1]
	two := append(bytes.Clone(data[:head]), body...)
	two = append(two, body...)
	// Before it, the count of the bodies, 2, and the body of rule 0: rule 0, 2 registers, 3
	// instructions, CONST nil to r0, CONST true to r1 and RETURN r0 r1.
	rule0 := []byte{2, 0, 2, 3, byte(bytecode.FlowConst), 0, bytecode.ConstNil, 0, byte(bytecode.FlowConst), 2, bytecode.ConstBool, 2, byte(bytecode.FlowReturn), 0, 2}
	count := head - len(rule0)
	if count < 0 || !bytes.Equal(data[count:head], rule0) {
		t.Fatalf("the body of rule 1 does not follow the count and the body of rule 0")
	}
	two[count] = 3
	// The bodies of rules 1 and 0 in that order, which Encoder never writes, followed by no stream
	// body.
	swapped := append(bytes.Clone(data[:count]), 2)
	swapped = append(append(append(swapped, body...), rule0[1:]...), 0)
	for _, c := range []struct {
		name string
		data []byte
		want string
	}{
		{"unknown operation", one(0x7f), "invalid flow operation"},
		{"constant of an unknown kind", one(byte(bytecode.FlowConst), 0, 9, 0), "invalid flow constant"},
		{"boolean constant 2", one(byte(bytecode.FlowConst), 0, bytecode.ConstBool, 4), "invalid flow constant"},
		{"nil constant 1", one(byte(bytecode.FlowConst), 0, bytecode.ConstNil, 2), "invalid flow constant"},
		{"two bodies of a rule", two, "Flow bodies of rule 1 after rule 1, not in increasing rule order"},
		{"bodies out of rule order", swapped, "Flow bodies of rule 0 after rule 1, not in increasing rule order"},
	} {
		r := &bytecode.Decoder{Data: c.data, Strs: m.Strings}
		r.ReadModule()
		if r.Err == nil || !strings.Contains(r.Err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, r.Err, c.want)
		}
	}
}

// TestStreamBodyEncoding checks that a stream body (Module.Streams) survives writing and reading
// beside the Flow body of its rule, that the loaded module validates, and that reading rejects a
// rule with two stream bodies and loading a stream body of a rule out of range or one that is not
// valid.
func TestStreamBodyEncoding(t *testing.T) {
	m := flowModule()
	m.Streams = map[int]*bytecode.Flow{1: m.Flows[1]}
	if err := bytecode.ValidateModule(m, noFieldTypes); err != nil {
		t.Fatal(err)
	}
	data := writeModule(m)
	r := &bytecode.Decoder{Data: data, Strs: m.Strings}
	got := r.ReadModule()
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if !reflect.DeepEqual(got.Flows, m.Flows) || !reflect.DeepEqual(got.Streams, m.Streams) {
		t.Fatalf("bodies read back as %+v and %+v", got.Flows, got.Streams)
	}
	if err := bytecode.ValidateModule(got, noFieldTypes); err != nil {
		t.Fatal(err)
	}
	// The stream bodies come last: one body of rule 1, written as the Flow body before them is.
	head := bytes.LastIndex(data, []byte{1, 2, 8, byte(len(m.Flows[1].Code))})
	two := append(bytes.Clone(data[:head]), 2)
	two = append(two, data[head+1:]...)
	two = append(two, data[head+1:]...)
	r = &bytecode.Decoder{Data: two, Strs: m.Strings}
	if r.ReadModule(); r.Err == nil || !strings.Contains(r.Err.Error(), "stream bodies of rule 1 after rule 1") {
		t.Errorf("two stream bodies: error %v", r.Err)
	}
	if d := m.Disassemble(); !strings.Contains(d, "flow b.stream regs=8:\n") {
		t.Errorf("disassembly has no stream body:\n%s", d)
	}
	for _, c := range []struct {
		name string
		edit func(m *bytecode.Module)
		want string
	}{
		{"rule", func(m *bytecode.Module) { m.Streams = map[int]*bytecode.Flow{2: m.Flows[1]} }, "stream body of rule 2 out of range"},
		{"body", func(m *bytecode.Module) {
			f := *m.Flows[1]
			f.Code = f.Code[:17]
			m.Streams = map[int]*bytecode.Flow{1: &f}
		}, "stream body of rule b: the body does not end with JUMP or a return"},
		{"streamed rule", func(m *bytecode.Module) {
			m.Streams = map[int]*bytecode.Flow{1: m.Flows[1]}
			m.Flows[1].Code[87].Imm = 2 // STREAMON
		}, "rule 2 out of range"},
	} {
		m := flowModule()
		c.edit(m)
		if err := bytecode.ValidateModule(m, noFieldTypes); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.want)
		}
	}
}

// TestFlowBodyValidation checks that loading rejects a Flow body whose rule, registers, targets,
// shape or immediates are out of range, that may read a register before writing it, that can run
// past its end, or whose call makes a cycle of
// calls through a rule not marked Rec.
func TestFlowBodyValidation(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(m *bytecode.Module, f *bytecode.Flow)
		want string
	}{
		{"rule", func(m *bytecode.Module, f *bytecode.Flow) { m.Flows[2] = f }, "flow body of rule 2 out of range"},
		{"unwritten register", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Regs++
			f.Code[18].Args[1] = f.Regs - 1
		}, "register 8 may be read before it is written"},
		{"destination", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[0].Dst[0] = 8 }, "register 8 out of range"},
		{"discarded argument", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[15].Args[0] = -1 }, "register -1 out of range"},
		{"destination below -1", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[6].Dst[1] = -2 }, "register -2 out of range"},
		{"target", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[13].Target = len(f.Code) }, "invalid jump"},
		{"target of no jump", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[15].Target = 1 }, "invalid jump"},
		{"arguments", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[18].Args = f.Code[18].Args[:1] }, "RETURN at 18 with 0 destinations and 1 arguments"},
		{"operation", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[15].Op = 0x7f }, "invalid flow operation"},
		{"end", func(m *bytecode.Module, f *bytecode.Flow) { f.Code = f.Code[:17] }, "does not end with JUMP or a return"},
		{"empty", func(m *bytecode.Module, f *bytecode.Flow) { f.Code = nil }, "does not end with JUMP or a return"},
		{"constant", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[0].Imm = "s" }, "invalid immediate"},
		{"immediate of no kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[15].Imm = 1 }, "invalid immediate"},
		{"guard kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[7].Imm = bytecode.FlowCallOf{} }, "invalid immediate"},
		{"class", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[8].Imm = bytecode.FlowGuardOf{Class: 1} }, "invalid guard class 1"},
		{"guard of a literal with a class", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[7].Imm = bytecode.FlowGuardOf{} }, "invalid immediate"},
		{"guard of a class with a literal", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[8].Imm = bytecode.FlowLitGuardOf{} }, "invalid immediate"},
		{"folded upper-case letter", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[7].Imm = bytecode.FlowLitGuardOf{Rune: 'A', Fold: true}
		}, "invalid guard character U+0041"},
		{"guarded surrogate", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[7].Imm = bytecode.FlowLitGuardOf{Rune: 0xd800} }, "invalid guard character U+D800"},
		{"literal guard depth", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[7].Imm = bytecode.FlowLitGuardOf{Rune: 'a', Depth: -1}
		}, "invalid guard depth"},
		{"depth", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[8].Imm = bytecode.FlowGuardOf{Depth: -1} }, "invalid guard depth"},
		{"expectation", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[10].Imm = bytecode.FlowExpectOf{Exp: bytecode.FixedExps + len(m.Strings)}
		}, "expectation 8 out of range"},
		{"called rule", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[14].Imm = bytecode.FlowCallOf{Rule: 2} }, "rule 2 out of range"},
		{"level", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[14].Imm = bytecode.FlowCallOf{Level: -1} }, "invalid binding level"},
		{"tested class", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[29].Imm = 1 }, "class 1 out of range"},
		{"class kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[29].Imm = true }, "invalid immediate"},
		{"anchor", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[30].Imm = 4 }, "invalid anchor 4"},
		{"negative anchor", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[30].Imm = -1 }, "invalid anchor -1"},
		{"literal", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[31].Imm = bytecode.FlowLitOf{Str: len(m.Strings), Exp: bytecode.FixedExps}
		}, "literal 3 out of range"},
		{"literal expectation", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[31].Imm = bytecode.FlowLitOf{Exp: bytecode.FixedExps + len(m.Strings)}
		}, "expectation 8 out of range"},
		{"literal kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[31].Imm = bytecode.FlowExpectOf{} }, "invalid immediate"},
		{"lookahead delta", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[26].Imm = bytecode.FlowSilenceOf{Delta: 2} }, "invalid lookahead"},
		{"lookahead site", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[26].Imm = bytecode.FlowSilenceOf{Delta: 1, Site: -2}
		}, "invalid lookahead"},
		{"lookahead entered matched", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[26].Imm = bytecode.FlowSilenceOf{Delta: 1, Ok: true}
		}, "invalid lookahead"},
		{"lookahead kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[27].Imm = 1 }, "invalid immediate"},
		{"immediate of a step", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[25].Imm = 1 }, "invalid immediate"},
		{"count", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[35].Imm = -1 }, "invalid count -1"},
		{"count kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[35].Imm = true }, "invalid immediate"},
		{"string", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[43].Imm = len(m.Strings) }, "string 3 out of range"},
		{"literal text", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[42].Imm = -1 }, "string -1 out of range"},
		{"string kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[43].Imm = "a" }, "invalid immediate"},
		{"shape mode", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[48].Imm = bytecode.FlowShapeOf{Mode: 3} }, "invalid shape"},
		{"shape count", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[49].Imm = bytecode.FlowShapeOf{K: -1} }, "invalid shape"},
		{"shape kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[49].Imm = 1 }, "invalid immediate"},
		{"immediate of a push", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[45].Imm = 1 }, "invalid immediate"},
		{"slot", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[56].Imm = 2 }, "capture slot 2 out of range"},
		{"negative slot", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[50].Imm = -1 }, "capture slot -1 out of range"},
		{"slot past the rule's frame", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[53] = bytecode.FlowInstr{Op: bytecode.FlowFrame, Dst: []int{6}}
			f.Code[56].Imm = 1
		}, "capture slot 1 out of range"},
		{"slot kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[50].Imm = bytecode.FlowShapeOf{} }, "invalid immediate"},
		{"scope", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[57].Imm = 1 }, "scope 1 out of range"},
		{"scope of a frame", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[53].Imm = -1 }, "scope -1 out of range"},
		{"scope kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[57].Imm = nil }, "invalid immediate"},
		{"run site", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[59].Imm = 10000 }, "invalid run site 10000"},
		{"run site zero", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[66].Imm = 0 }, "invalid run site 0"},
		{"run walk site", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[60].Imm = bytecode.FlowRunWalkOf{Site: 10000} }, "invalid run site 10000"},
		{"run walk key", func(m *bytecode.Module, f *bytecode.Flow) { m.RunKeys = nil }, "run site 1 is keyed by the variables but has no run key"},
		{"run walk kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[60].Imm = 1 }, "invalid immediate"},
		{"scan site", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[67].Imm = 10000 }, "invalid scan site 10000"},
		{"scan site zero", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[69].Imm = 0 }, "invalid scan site 0"},
		{"action", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[70].Imm = 1 }, "action 1 out of range"},
		{"action expression kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[70].Imm = nil }, "invalid immediate"},
		{"bind variable", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[71].Imm = bytecode.FlowBindOf{Var: 1} }, "variable 1 out of range"},
		{"bind action expression", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[71].Imm = bytecode.FlowBindOf{Action: -1} }, "action -1 out of range"},
		{"bind kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[71].Imm = 0 }, "invalid immediate"},
		{"resumed repetition", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[91].Imm = bytecode.FlowResumeOf{Max: -1} }, "invalid resumed repetition 0"},
		{"resumed least count", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[91].Imm = bytecode.FlowResumeOf{Site: 1, Min: -1, Max: -1}
		}, "invalid resumed repetition 1 with -1"},
		{"resumed most count", func(m *bytecode.Module, f *bytecode.Flow) {
			f.Code[91].Imm = bytecode.FlowResumeOf{Site: 1, Min: 2, Max: 1}
		}, "invalid resumed repetition 1 with 2 to 1"},
		{"resumed unbounded below -1", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[91].Imm = bytecode.FlowResumeOf{Site: 1, Max: -2} }, "invalid resumed repetition 1 with 0 to -2"},
		{"resumed kind", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[91].Imm = 1 }, "invalid immediate"},
		{"immediate of a resumed step", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[92].Imm = 1 }, "invalid immediate"},
		{"unmarked cycle", func(m *bytecode.Module, f *bytecode.Flow) { f.Code[14].Imm = bytecode.FlowCallOf{Rule: 1} }, "rule b is on a cycle of calls but is not marked recursive"},
	} {
		m := flowModule()
		c.edit(m, m.Flows[1])
		if err := bytecode.ValidateModule(m, noFieldTypes); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.want)
		}
	}
	// The call of a Flow body that makes the cycle is accepted once its rule counts against the
	// nesting limit.
	m := flowModule()
	m.Flows[1].Code[14].Imm = bytecode.FlowCallOf{Rule: 1}
	m.Rules[1].Rec = true
	if err := bytecode.ValidateModule(m, noFieldTypes); err != nil {
		t.Errorf("marked cycle: %v", err)
	}
}

// TestFlowShapes checks that every operation has a shape with a name, so that a module file can
// write it and a disassembly can show it.
func TestFlowShapes(t *testing.T) {
	for op, s := range bytecode.FlowShapes {
		if s.Name == "" {
			t.Errorf("operation %d has no shape", op)
		}
	}
	if _, ok := bytecode.FlowOp(len(bytecode.FlowShapes)).Shape(); ok {
		t.Error("an operation past the table has a shape")
	}
}

// TestFlowBodyDisassembly checks that the disassembly shows each Flow body under its rule, with the
// registers, targets and immediates of its instructions.
func TestFlowBodyDisassembly(t *testing.T) {
	got := flowModule().Disassemble()
	for _, want := range []string{
		"flow b regs=8:\n",
		"     0  CONST      r0 = nil\n",
		"     1  CONST      r1 = -1099511627776\n",
		"     6  PEEK       r5,_,r6 =\n",
		"     7  GUARDLIT   r7 = 'é' fold depth=2\n",
		"     8  GUARD      r7 = r5 r6 \"[a-z]\" depth=0\n",
		"     9  JUMPFALSE  r7 -> 14\n",
		"    10  EXPECT     r1 \"b\" message\n",
		"    14  CALL       r0,r7 = a level=3\n",
		"    18  RETURN     r0 r3\n",
		"    19  ADD        r1 = r1 r1\n",
		"    25  STEP       r1 = r1\n",
		"    26  SILENT     delta=1 site=3 ok=false\n",
		"    29  INCLASS    r2 = r5 \"[a-z]\"\n",
		"    30  ATANCHOR   r2 = 3\n",
		"    31  MATCHLIT   r0,r2 = \"a\" fold \"a\"\n",
		"    33  UNISOLATE  r1,r6 = r4\n",
		"    34  MERGEEXPECTED r1 r6\n",
		"    35  NEWVALS    r4 = 2\n",
		"    42  LITERALTEXT r0 = r1 \"a\"\n",
		"    43  STR        r0 = \"b\"\n",
		"    48  SHAPESINGLE r0,r2 = r0 r1 elems=true shared=true tuple=false k=2 mode=2\n",
		"    49  SCOPEVALUE r0 = r0 r1 elems=false shared=false tuple=true k=1 mode=0\n",
		"    50  SETCAPTURE r0 slot=0\n",
		"    51  MARKPOS    r1 = r4\n",
		"    53  NEWFRAME   r6 = scope=[a b]\n",
		"    56  FRAMEVAL   r0 = r6 slot=1\n",
		"    57  ATTACHFRAME r0 = r0 r1 r6 scope=[a b]\n",
		"    58  SHAPECAPTURE r0 = r0 r1 elems=false shared=false tuple=false k=1 mode=1\n",
		"    59  RUNHOT     r2 = site=1\n",
		"    60  RUNBEGIN   site=1 build=true keyenv=true\n",
		"    66  RUNWALKED  r1 site=1\n",
		"    69  SCANKIDS   r4 = r1 r1 r1 site=5\n",
		"    70  PRED       r2 = action=0\n",
		"    71  BIND       r2 = var=0 action=0\n",
		"    72  CUT        r2 =\n",
		"    73  SETCUT     r2\n",
		"    74  KEEP       r6 = r6\n",
		"    75  MAKEERROR  r7 = r1 r6\n",
		"    76  APPENDERROR r7\n",
		"    77  NEWERRORNODE r0 = r1 r7\n",
		"    78  RECLEN     r1 =\n",
		"    79  RESTORE    r1 r1\n",
		"    87  STREAMON   r2 = b\n",
		"    88  STREAMSTART \n",
		"    89  EMITSTREAM r0\n",
		"    90  RESUMEON   r2 =\n",
		"    91  RESUMESTART r4 = resume=3 min=1 max=-1 shiftable=true\n",
		"    93  RESUMENEXT r0,r2 = r4\n",
		"    95  RESUMEEND  r4 r2 r0\n",
		"    97  MERGERENAMED r1 r6 ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("disassembly has no %q:\n%s", want, got)
		}
	}
}

// TestSpecFlowOperations checks that the instruction table of Flow Bodies in spec/bytecode/README.md
// gives each operation the code, name and numbers of registers of its shape: a module stores the
// codes and reads as many registers as the shape says, so a change to either changes the file
// format.
func TestSpecFlowOperations(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../spec/bytecode/README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, sec, ok := strings.Cut(string(data), "\n## Flow Bodies\n")
	if !ok {
		t.Fatal("spec/bytecode/README.md has no Flow Bodies section")
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	rows := regexp.MustCompile("(?m)^\\| (\\d+) \\| `([A-Z]+)` \\| (\\d+) \\| (\\d+) \\|").FindAllStringSubmatch(sec, -1)
	if len(rows) != len(bytecode.FlowShapes) {
		t.Fatalf("the specification lists %d operations; the runtime has %d", len(rows), len(bytecode.FlowShapes))
	}
	for _, r := range rows {
		code, _ := strconv.Atoi(r[1])
		d, _ := strconv.Atoi(r[3])
		a, _ := strconv.Atoi(r[4])
		s, ok := bytecode.FlowOp(code).Shape()
		if !ok || s.Name != r[2] || s.Dst != d || s.Args != a {
			t.Errorf("the specification has %s as code %d with %d destinations and %d arguments; the runtime has %+v", r[2], code, d, a, s)
		}
	}
}
