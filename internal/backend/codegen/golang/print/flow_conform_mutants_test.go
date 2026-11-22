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
		from: "TNode: TNode[string]{Span{start, end}, text}", to: "TNode: TNode[string]{Span{start + 1, end}, text}",
		cases: []string{"typed"}},
	{name: "a match whose text differs", op: flow.OpNewMatch, file: "typed.go",
		from: "TNode: TNode[string]{Span{start, end}, text}", to: `TNode: TNode[string]{Span{start, end}, text + "?"}`,
		cases: []string{"typed"}},
	{name: "a terminal that starts one code point late", op: flow.OpNewTerminalNode, file: "lib.go",
		from: "Start: int32(start), End: int32(p.Pos), Text: text, Terminal: true", to: "Start: int32(start) + 1, End: int32(p.Pos), Text: text, Terminal: true",
		cases: []string{"plain"}},
	{name: "a branch that drops its children", op: flow.OpNewBranchNode, file: "lib.go",
		from: "Start: int32(start), End: int32(p.Pos), Children: children, Fresh: true", to: "Start: int32(start), End: int32(p.Pos), Children: nil, Fresh: true",
		cases: []string{"plain"}},
	{name: "a node that drops its last child", op: flow.OpNewNode, file: "typed.go",
		from: "*n = TSeq{Span: Span{start, end}, Typ: typ, Kids: kids, fresh: true}", to: "*n = TSeq{Span: Span{start, end}, Typ: typ, Kids: kids[:max(len(kids)-1, 0)], fresh: true}",
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
		from: "p.Spend()\n\t\t\tp.Expect(far, s[i])", to: "p.Spend()\n\t\t\tp.Expect(far+1, s[i])",
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
	{name: "a skipped run that takes the position back", op: flow.OpSkipHit, file: "skip_runs.go",
		from: "p.MergeExpected(r.far, r.exp)\n\tp.Pos = r.end", to: "p.MergeExpected(r.far, r.exp)\n\tp.Pos = p.Pos - 1",
		cases: []string{"runsites", "runvalues"}},
	{name: "the end of a walk that moves the position", op: flow.OpRunWalked, file: "skip_runs.go",
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
	{name: "an action that returns no value", op: flow.OpCtxFinish, file: "typed.go",
		from: "t.TSetFresh(false)\n\treturn t", to: "t.TSetFresh(false)\n\treturn nil",
		cases: []string{"typed", "lean"}},
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
