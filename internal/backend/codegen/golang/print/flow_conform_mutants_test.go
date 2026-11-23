package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// flowConformMutant breaks one primitive of the Go target: a patch of the text of a file of the
// library, or, when file is empty, of the printed form of the operation op (for the operations that
// the printer prints in place). The conformance of the Flow operations must report a failure for the
// operation op that the mutant breaks, on the cases that call it.
type flowConformMutant struct {
	name string
	op   string // the operation whose checks must fail
	file string // the file of the library that the patch changes, or "" for the printed form of op
	from string // the text to replace, which the patch finds in the file or the printed form
	to   string
	// cases are the names of the cases of flowConformCases that run on the mutant.
	cases []string
}

// flowConformMutants hold, for the operations that have specific checks, the negative controls that
// show the checks catch a broken library.
var flowConformMutants = []flowConformMutant{
	{name: "a match that starts one code point late", op: flow.OpNewMatch, file: "typed.go",
		from: "TNode: TNode[string]{Span: sxirinput.Span{Start: start, End: end}, Value: text}", to: "TNode: TNode[string]{Span: sxirinput.Span{Start: start + 1, End: end}, Value: text}",
		cases: []string{"typed"}},
	{name: "a match whose text differs", op: flow.OpNewMatch, file: "typed.go",
		from: "TNode: TNode[string]{Span: sxirinput.Span{Start: start, End: end}, Value: text}", to: `TNode: TNode[string]{Span: sxirinput.Span{Start: start, End: end}, Value: text + "?"}`,
		cases: []string{"typed"}},
	{name: "a terminal that starts one code point late", op: flow.OpNewTerminalNode, file: "lib.go",
		from: "Start: int32(start), End: int32(p.Pos), Text: text, Terminal: true", to: "Start: int32(start) + 1, End: int32(p.Pos), Text: text, Terminal: true",
		cases: []string{"plain"}},
	{name: "a branch that drops its children", op: flow.OpNewBranchNode, file: "lib.go",
		from: "Start: int32(start), End: int32(p.Pos), Children: children, Fresh: true", to: "Start: int32(start), End: int32(p.Pos), Children: nil, Fresh: true",
		cases: []string{"plain"}},
	{name: "a node that drops its last child", op: flow.OpNewNode, file: "typed.go",
		from: "*n = TSeq{Span: sxirinput.Span{Start: start, End: end}, Typ: typ, Kids: kids, fresh: true}", to: "*n = TSeq{Span: sxirinput.Span{Start: start, End: end}, Typ: typ, Kids: kids[:max(len(kids)-1, 0)], fresh: true}",
		cases: []string{"typed"}},
	{name: "an expectation record that keeps the entries of an earlier farthest failure", op: flow.OpExpect, file: "call_exec.go",
		from: "p.farthest = pos\n\t\tp.exp = p.exp[:p.expBase]\n\t\tp.expBits = 0", to: "p.farthest = pos",
		cases: []string{"plain", "recover"}},
	{name: "an isolation that begins its record at the wrong position", op: flow.OpIsolate, file: "lib.go",
		from: "p.expBase, p.farthest, p.expBits = len(p.exp), pos, 0", to: "p.expBase, p.farthest, p.expBits = len(p.exp), pos+1, 0",
		cases: []string{"recover", "bind"}},
	{name: "an isolation that returns no expectations", op: flow.OpUnisolate, file: "lib.go",
		from: "far, inner = p.farthest, p.exp[p.expBase:]", to: "far, inner = p.farthest, p.exp[p.expBase:p.expBase]",
		cases: []string{"recover", "bind"}},
	{name: "an isolation that restores the wrong record", op: flow.OpUnisolate, file: "lib.go",
		from: "p.expBase, p.farthest, p.expBits = m.base, m.far, m.bits", to: "p.expBase, p.farthest, p.expBits = m.base, m.far+1, m.bits",
		cases: []string{"recover", "bind"}},
	{name: "a merge at the wrong position", op: flow.OpMergeExpected, file: "call_exec.go",
		from: "for i = 0; i < len(s); i++ {\n\t\t\tp.Expect(far, s[i])", to: "for i = 0; i < len(s); i++ {\n\t\t\tp.Expect(far+1, s[i])",
		cases: []string{"recover", "bind"}},
	{name: "a kept set that loses its last expectation", op: flow.OpKeep, file: "call_exec.go",
		from: "p.lastKept = p.arena[off:len(p.arena):len(p.arena)]", to: "p.lastKept = p.arena[off : len(p.arena)-1 : len(p.arena)-1]",
		cases: []string{"recover", "lean"}},
	{name: "a frame that is not made current", op: flow.OpSetFrame, from: "p.Frame = ", to: "_ = ",
		cases: []string{"leftrec", "typed"}},
	{name: "an environment that is not set", op: flow.OpSetEnv, from: "p.Env = ", to: "_ = ",
		cases: []string{"bind"}},
	{name: "a capture that is not set", op: flow.OpSetCapture, file: "lib.go",
		from: "p.Frame.Vals[slot] = v", to: "_ = v",
		cases: []string{"typed"}},
	{name: "a typed capture that is not set", op: flow.OpSetCapture, file: "typed.go",
		from: "p.Frame.Vals[slot] = v", to: "_ = v",
		cases: []string{"typedops"}},
	{name: "a skipped run that takes the position back", op: flow.OpSkipHit, file: "skip_walks.go",
		from: "p.MergeExpected(r.far, r.exp)\n\tp.Pos = r.end", to: "p.MergeExpected(r.far, r.exp)\n\tp.Pos = p.Pos - 1",
		cases: []string{"runsites", "runvalues"}},
	{name: "the end of a walk that moves the position", op: flow.OpRunWalked, file: "skip_walks.go",
		from: "c.end = max(c.end, p.Pos)\n}", to: "c.end = max(c.end, p.Pos)\n\tp.Pos++\n}",
		cases: []string{"runsites", "runvalues"}},
	{name: "a scan of the untyped library that leaves the values of a recorded walk gathered", op: flow.OpScanKids, file: "scan_sites.go",
		from: "p.DropKids(base)\n\t\treturn w.rec.kids", to: "return w.rec.kids",
		cases: []string{"scanvalsu"}},
	{name: "a context that keeps the structs it made", op: flow.OpDropCreated, file: "typed.go",
		from: "clear(p.Created[base:])\n\tp.Created = p.Created[:base]", to: "clear(p.Created[base:])",
		cases: []string{"lean"}},
	{name: "a node that stays owned by its maker", op: flow.OpUnfresh, from: "TSetFresh(false)", to: "TSetFresh(true)",
		cases: []string{"lean", "typed"}},
	{name: "an append that loses the element", op: flow.OpAppend, from: "append(", to: "rt.ConfAppendLoses(",
		cases: []string{"plain", "lean"}},
	{name: "a result that is not its own tuple", op: flow.OpShapeSingle, file: "lib.go",
		from: "\t\t}\n\t\treturn v\n\t}\n\tvar kids []*sxirvalue.Node", to: "\t\t}\n\t\treturn nil\n\t}\n\tvar kids []*sxirvalue.Node",
		cases: []string{"shapes", "plain"}},
	{name: "a scope tuple that ends past the position", op: flow.OpScopeValue, file: "lib.go",
		from: "Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true})", to: "Start: int32(start), End: int32(p.Pos) + 1, Children: kids, Fresh: true})",
		cases: []string{"shapes", "plain"}},
	{name: "a capture without value that ends past the position", op: flow.OpShapeCapture, file: "lib.go",
		from: "Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Fresh: true})", to: "Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos) + 1, Fresh: true})",
		cases: []string{"shapes"}},
	{name: "a value that loses its captures", op: flow.OpAttachCaptures, file: "typed.go",
		from: "\t\t\tt.set(name, vals[i])\n\t\t}\n\t}\n\treturn t", to: "\t\t\tt.set(name, vals[i])\n\t\t}\n\t}\n\treturn nil",
		cases: []string{"lean"}},
	{name: "a value that loses its captures where none is set", op: flow.OpAttachFrame, file: "lib.go",
		from: "if !found {\n\t\treturn v\n\t}", to: "if !found {\n\t\treturn nil\n\t}",
		cases: []string{"shapes"}},
	{name: "a value that loses its captures where none is set (typed)", op: flow.OpAttachCaptures, file: "typed.go",
		from: "if !found {\n\t\treturn v\n\t}", to: "if !found {\n\t\treturn nil\n\t}",
		cases: []string{"typed"}},
	{name: "a clear that leaves the slots of the frame set", op: flow.OpClearVals, from: "clear(", to: "rt.ConfNoClear(",
		cases: []string{"typed", "lean"}},
	{name: "a stream parse that reports no delivery at its top call", op: flow.OpStreamOn, file: "stream_exec.go",
		from: "p.Stream.start == id", to: "p.Stream.start != id",
		cases: []string{"stream"}},
	{name: "a stream that forgets the bytes in use", op: flow.OpStreamStart, file: "stream.go",
		from: "\tp.memKept += p.memUsed\n", to: "",
		cases: []string{"stream"}},
	{name: "an element that is committed but not delivered", op: flow.OpEmitStream, file: "stream_exec.go",
		from: "\tp.streamDeliver(v)\n", to: "\t_ = v\n",
		cases: []string{"stream"}},
	{name: "an element that is delivered as nil", op: flow.OpEmitStream, file: "stream_exec.go",
		from: "\tp.streamDeliver(v)\n", to: "\tp.streamDeliver(nil)\n",
		cases: []string{"stream"}},
	{name: "an action that returns no value", op: flow.OpCtxFinish, file: "typed.go",
		from: "t.TSetFresh(false)\n\treturn t", to: "t.TSetFresh(false)\n\treturn nil",
		cases: []string{"typed", "lean"}},
	{name: "an advance that counts one unit more than it advances", op: flow.OpAdvance, from: "p.Units += ", to: "p.Units += 1 + ",
		cases: []string{"plain"}},
	{name: "a recovery that counts no unit for the error it builds", op: flow.OpMakeError, from: "p.MakeErrorLogical(", to: "p.MakeError(",
		cases: []string{"recover"}},
	{name: "an append of a recovered error that counts a unit", op: flow.OpAppendError, from: "p.PushRecovered(", to: "p.Units++\np.PushRecovered(",
		cases: []string{"recover"}},
	{name: "a literal that keeps the position it reached where it fails", op: flow.OpMatchLit, file: "lib.go",
		from: "p.Expect(start, desc)\n\t\t\t\tp.Pos = start\n", to: "p.Expect(start, desc)\n",
		cases: []string{"recmemo"}},
	{name: "a memo key that counts its variables but not the bytes of their strings", op: flow.OpCallRule, file: "budget.go",
		from: "n += uint64(len(s))", to: "_ = s",
		cases: []string{"logicalkeys"}},
	{name: "a stored result that counts no unit for the recovered errors it takes", op: flow.OpCallRule, file: "call_exec.go",
		from: "p.Units += uint64(len(p.Recovered) - rec)", to: "p.Units += 0",
		cases: []string{"recmemo"}},
	{name: "a growth step that counts no unit", op: flow.OpCallRule, file: "call_exec.go",
		from:  "\t\t\t\tp.Units++\n\t\t\t\tif p.Units > p.UnitsCheck {\n\t\t\t\t\tp.PollUnits()\n\t\t\t\t}\n\t\t\t\tv, ok := p.invoke(r, min)",
		to:    "\t\t\t\tv, ok := p.invoke(r, min)",
		cases: []string{"leftrec"}},
	{name: "a skip that counts no key for the memoized calls it makes", op: flow.OpSkippedCalls, file: "units.go",
		from: "\tp.KeyUnits(r)\n\tif p.FirstCall(r) {", to: "\tif p.FirstCall(r) {",
		cases: []string{"logicalkeys"}},
	{name: "a skip that counts one call more than it makes", op: flow.OpSkippedCalls, file: "units.go",
		from: "func (p *Parser) SkipUnits(n uint64) {\n\tp.Units += n\n", to: "func (p *Parser) SkipUnits(n uint64) {\n\tp.Units += n + 1\n",
		cases: []string{"logical"}},
	{name: "a suffix copy that counts no unit", op: flow.OpSkipKids, file: "skip_runs_lib.go",
		from: "p.unitsSized(n) // a suffix copy", to: "_ = n // a suffix copy",
		cases: []string{"runvalues", "runmin", "runlook", "runkeys"}},
	{name: "an insertion that counts no unit for the error it builds", op: flow.OpInsertMissing, from: "p.InsertMissingLogical(", to: "p.InsertMissing(",
		cases: []string{"missing"}},
	{name: "a copy of the insertions of a result that counts no unit", op: flow.OpCallRule, file: "missing.go",
		from: "\tp.unitsSized(len(s.path) - n)\n", to: "",
		cases: []string{"missing"}},
	{name: "a reused result that counts no unit for the insertions it records again", op: flow.OpCallRule, file: "missing.go",
		from: "\tp.unitsSized(len(seed))\n\tvar dup", to: "\tvar dup",
		cases: []string{"missing"}},
	{name: "a reused result that counts only the errors it records one by one", op: flow.OpCallRule, file: "missing.go",
		from: "\tp.unitsSized(len(all))\n\tfor _, e := range all {\n\t\tif !dup[e] {\n", to: "\tfor _, e := range all {\n\t\tif !dup[e] {\n\t\t\tp.unitsSized(1)\n",
		cases: []string{"missing"}},
	{name: "a walk that counts its key as it starts where the element binds a variable of it", op: flow.OpRunBegin, file: "skip_walks.go",
		from: "if len(w.key.bind) == 0 && !p.skipMut(skipIgnoreEnv)", to: "if !p.skipMut(skipIgnoreEnv)",
		cases: []string{"runkeys"}},
	{name: "a walk that counts its key at each lookup where the element binds none of its variables", op: flow.OpSkipHit, file: "skip_walks.go",
		from: "if w.key != nil && len(w.key.bind) > 0 && p.countsWalkKeys()", to: "if w.key != nil && p.countsWalkKeys()",
		cases: []string{"runvalues"}},
	{name: "an error node that ends past the position", op: flow.OpNewErrorNode, file: "lib.go",
		from: "Kind: valueimpl.ErrorKind, Start: int32(start), End: int32(p.Pos),", to: "Kind: valueimpl.ErrorKind, Start: int32(start), End: int32(p.Pos) + 1,",
		cases: []string{"recover"}},
}

func TestFlowConformCatchesBrokenLibraries(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	byName := map[string]flowConformCase{}
	for _, c := range flowConformCases {
		byName[c.name] = c
	}
	for _, m := range flowConformMutants {
		t.Run(m.name, func(t *testing.T) {
			var cases []flowConformCase
			for _, n := range m.cases {
				c, ok := byName[n]
				if !ok {
					t.Fatalf("no case %q", n)
				}
				cases = append(cases, c)
			}
			found := false
			var mut flowMutation
			if m.file != "" {
				mut.lib = func(name, src string) string {
					if name != m.file || strings.Count(src, m.from) == 0 {
						return src
					}
					found = true
					return strings.ReplaceAll(src, m.from, m.to)
				}
			} else {
				mut.print = func(op, printed string) string {
					if op != m.op || strings.Count(printed, m.from) == 0 {
						return printed
					}
					found = true
					return strings.ReplaceAll(printed, m.from, m.to)
				}
			}
			msgs, covered := flowConformRun(t, cases, mut)
			if !found {
				t.Fatalf("no %q in %s%s: the mutant is stale", m.from, m.file, m.op)
			}
			for _, msg := range msgs {
				if strings.Contains(msg, m.op+" at") {
					return
				}
			}
			t.Errorf("the checks of %s passed on %s (%d other failures); the cases call it %d times", m.op, m.name, len(msgs), covered[m.op])
		})
	}
}
