package engine

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// parseCharged parses input with the recursive bytecode VM of prog as Parse does, and returns the
// tree, the error and the budget spend of the parse.
func parseCharged(t *testing.T, prog *Program, input string) (string, string, uint64) {
	t.Helper()
	return parseChargedWith(t, prog, input, Bytecode)
}

// parseChargedWith is parseCharged on the backend b.
func parseChargedWith(t *testing.T, prog *Program, input string, b Backend) (string, string, uint64) {
	t.Helper()
	return parseChargedTuned(t, prog, input, b, state.Tuning{})
}

// parseChargedTuned is parseChargedWith with the test-only settings tune.
func parseChargedTuned(t *testing.T, prog *Program, input string, b Backend, tune state.Tuning) (string, string, uint64) {
	t.Helper()
	n, msg, used := parseChargedOpts(t, prog, input, ParseOptions{Backend: b, Tune: tune})
	tree := "<nil>"
	if n != nil {
		tree = n.String()
	}
	return tree, msg, used
}

// parseChargedOpts is parseCharged with the options o (of which it reads the backend, the
// recording of expectations and the test-only settings), returning the tree as a node.
func parseChargedOpts(t *testing.T, prog *Program, input string, o ParseOptions) (*value.Node, string, uint64) {
	t.Helper()
	start := prog.ResolveStart("")
	active, _, control, err := prog.admitParse(start, o, optionsFamily(o), false, input)
	if err != nil {
		t.Fatal(err)
	}
	control.Check()
	p := state.AcquirePooledParser(&active.sharedProgram)
	defer p.ReleaseBatchScratch()
	p.Input = state.NewControlledTextInput(input, o.Unit, true, p.Scratch, control)
	control.AttachMemory(p)
	p.MaxDepth, _ = state.DepthLimit(o, o.Backend)
	p.Expectations = o.Expectations
	p.DeferMemo = true
	p.SetTuning(o.Tune)
	n, err := active.Run(p, o.Backend, start)
	if !p.Aborted {
		control.Check()
	}
	msg := "<nil>"
	if err != nil {
		msg = err.Error()
	}
	var used uint64 // nothing is charged where budget controls are compiled out
	if control != nil {
		used = control.Used
	}
	return n, msg, used
}

// jsonFlowPrograms returns the JSON grammar compiled as usual, and the same grammar compiled to a
// module of its own whose value rule body mutate, if not nil, changes before it is installed. The
// first is the reference: the tests parse it with the closure backend.
func jsonFlowPrograms(t *testing.T, mutate func(*bytecode.Flow)) (*Program, *Program) {
	t.Helper()
	old, flows, n := flowPrograms(t, "../../../testdata/grammars/json/json.sxir", func(name string) bool { return name == "value" }, func(_ string, f *bytecode.Flow) {
		if mutate != nil {
			mutate(f)
		}
	})
	if n != 1 {
		t.Fatalf("the value rule has no Flow body (%d bodies)", n)
	}
	return old, flows
}

// flowPrograms returns the grammar of the file path compiled as usual, and the same grammar whose VM
// runs a module compiled for it alone (checked.Source.Module), in which mutate, if
// not nil, changes the Flow body of each rule that pick accepts before it is installed; n is the
// number of such rule bodies. The VM runs every rule from its Flow body, so the tests compare the
// second program with the first parsed by the closure backend, which shares no code with the VM.
func flowPrograms(t *testing.T, path string, pick func(rule string) bool, mutate func(rule string, f *bytecode.Flow)) (old, flows *Program, n int) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return flowProgramsOf(t, string(src), pick, mutate)
}

// flowProgramsOf is flowPrograms for the grammar src.
func flowProgramsOf(t testing.TB, src string, pick func(rule string) bool, mutate func(rule string, f *bytecode.Flow)) (old, flows *Program, n int) {
	t.Helper()
	old = mustCompile(t, src)
	cp := old.src.(interface{ Checked() *frontend.Result }).Checked()
	bc, err := checked.Of(cp, pass.Settings{}).Module()
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range bc.Rules {
		if f := bc.Flows[i]; f != nil && pick(bc.Strings[r.Name]) {
			n++
			if mutate != nil {
				mutate(bc.Strings[r.Name], f)
			}
		}
	}
	return old, New(flowSource{Source: old.src, m: bc}), n
}

// flowReference returns the tree and the error of the closure backend for input, the reference of
// the Flow bodies, which share no code with it. It returns the spend of the closure backend too,
// unless the bytecode backend does not yet charge the units of the closure backend on grammar
// (knownChargeDivergences): then the spend is that of the recursive VM on prog, which pins the charges of a Flow body to those of the
// bodies that the compiler selects.
func flowReference(t *testing.T, prog *Program, grammar, input string) (string, string, uint64) {
	t.Helper()
	tree, msg, used := parseChargedWith(t, prog, input, Closure)
	if knownChargeDivergences[grammar+"|bytecode"] {
		_, _, used = parseCharged(t, prog, input)
	}
	return tree, msg, used
}

// mutateCall changes the first call of a Flow body to call the null rule (jsonNull), a different rule
// than the one the body calls.
func mutateCall(f *bytecode.Flow) {
	for i := range f.Code {
		if in := &f.Code[i]; in.Op == bytecode.FlowCall || in.Op == bytecode.FlowCallJump {
			in.Imm = bytecode.FlowCallOf{Rule: jsonNull, Level: in.Imm.(bytecode.FlowCallOf).Level}
			return
		}
	}
}

// flowSource is the source of a grammar whose module is m.
type flowSource struct {
	Source
	m *bytecode.Module
}

func (s flowSource) Module() (*bytecode.Module, error) { return s.m, nil }

var jsonDifferentialInputs = []string{
	`{}`, `[]`, `1`, `"a\"b"`, `true`, `null`, `-0.5e+10`,
	`{"a":[1,2,{"b":null}],"c":"xA"}`, `[[[[]]]]`, `{"k":{"k":{"k":[]}}}`,
	`  [ -1.5e+3 , false ]  `, `[1,]`, `{"a" 1}`, `"unterminated`, `tru`, `-`, `01`, `[1,2`, ``,
}

// TestFlowBodyJSONValueDifferential runs the JSON corpus through the bytecode VM, which runs the
// value rule from its Flow body: the trees, the errors and the budget spends must be those of
// flowReference.
func TestFlowBodyJSONValueDifferential(t *testing.T) {
	old, spiked := jsonFlowPrograms(t, nil)
	for _, in := range jsonDifferentialInputs {
		ot, oe, ou := flowReference(t, old, "json", in)
		st, se, su := parseCharged(t, spiked, in)
		if ot != st || oe != se || ou != su {
			t.Errorf("input %q: closure tree %s err %s spend %d; flow tree %s err %s spend %d", in, ot, oe, ou, st, se, su)
		}
	}
}

// saveAndLoad saves prog as a compiled grammar and loads it.
func saveAndLoad(t *testing.T, prog *Program) *Program {
	t.Helper()
	data, err := prog.MarshalBinary("")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// jsonNull is the index of the null rule of the JSON grammar, which the changed call of mutateCall
// calls: the null rule calls no rule, while a call of main would close a cycle of calls through
// main, which is not marked Rec, and the loader rejects such a module.
const jsonNull = 16

// TestFlowBodyJSONValueFileForm saves the grammar whose value rule runs its Flow body as a compiled
// grammar and loads it: the loaded module has the same Flow bodies, and runs the JSON corpus with the
// trees, errors and spends of flowReference. A loaded body whose call is changed changes a tree, so
// the loaded program runs the body it read.
func TestFlowBodyJSONValueFileForm(t *testing.T) {
	old, spiked := jsonFlowPrograms(t, nil)
	if m := spiked.Module(); m.Strings[m.Rules[jsonNull].Name] != "null" {
		t.Fatalf("rule %d is %s, not null", jsonNull, m.Strings[m.Rules[jsonNull].Name])
	}
	_, called := jsonFlowPrograms(t, mutateCall)
	loaded, loadedCalled := saveAndLoad(t, spiked), saveAndLoad(t, called)
	changed := false
	for _, in := range jsonDifferentialInputs {
		ot, _, _ := parseChargedWith(t, old, in, Closure)
		ct, _, _ := parseCharged(t, loadedCalled, in)
		changed = changed || ot != ct
	}
	if !changed {
		t.Error("a changed call in the loaded Flow body changed no tree: the loaded program does not run its body")
	}
	want := spiked.Module().Flows
	if got := loaded.Module().Flows; len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded Flow bodies %v, want %v", got, want)
	}
	for _, in := range jsonDifferentialInputs {
		ot, oe, ou := flowReference(t, old, "json", in)
		lt, le, lu := parseCharged(t, loaded, in)
		if ot != lt || oe != le || ou != lu {
			t.Errorf("input %q: closure tree %s err %s spend %d; loaded flow tree %s err %s spend %d", in, ot, oe, ou, lt, le, lu)
		}
	}
}

// TestFlowBodyDifferentialDetectsMutations checks that the differential harness sees a changed Flow body:
// a call of the value rule's body to a different rule changes the trees of the corpus, and an extra
// advance over the character at the start of the body, which the body then resets, changes no result
// but counts the input it moves over, so it changes the spends. (A read alone is no event.)
func TestFlowBodyDifferentialDetectsMutations(t *testing.T) {
	old, called := jsonFlowPrograms(t, mutateCall)
	_, peeked := jsonFlowPrograms(t, func(f *bytecode.Flow) {
		mark, width := f.Regs, f.Regs+1
		f.Regs += 2
		spike := []bytecode.FlowInstr{
			{Op: bytecode.FlowMark, Dst: []int{mark}},
			{Op: bytecode.FlowPeek, Dst: []int{-1, width, -1}},
			{Op: bytecode.FlowAdvance, Args: []int{width}},
			{Op: bytecode.FlowReset, Args: []int{mark}},
		}
		for i := range f.Code {
			if sh, _ := f.Code[i].Op.Shape(); sh.Target {
				f.Code[i].Target += len(spike)
			}
		}
		f.Code = append(spike, f.Code...)
	})
	trees, spends := false, false
	for _, in := range jsonDifferentialInputs {
		ot, oe, ou := flowReference(t, old, "json", in)
		ct, _, _ := parseCharged(t, called, in)
		pt, pe, pu := parseCharged(t, peeked, in)
		trees = trees || ot != ct
		if pt != ot || pe != oe {
			t.Errorf("input %q: an extra advance that the body resets changed the result: %s %s, want %s %s", in, pt, pe, ot, oe)
		}
		spends = spends || pu != ou
	}
	if !trees {
		t.Error("a changed call in the Flow body changed no tree of the corpus: the Flow body does not run")
	}
	if !spends && state.BudgetControlsEnabled { // a build without budget controls charges nothing
		t.Error("an extra advance in the Flow body changed no spend of the corpus: the harness does not compare charges")
	}
}

// TestFlowBodyJSONValueIterative runs the JSON corpus through the iterative model, which runs the
// value rule from its Flow body: the trees, the errors and the budget spends must be those of
// flowReference, and a changed call in the body must change a tree, so that the iterative model runs it.
func TestFlowBodyJSONValueIterative(t *testing.T) {
	old, spiked := jsonFlowPrograms(t, nil)
	_, called := jsonFlowPrograms(t, mutateCall)
	changed := false
	for _, in := range jsonDifferentialInputs {
		ot, oe, ou := flowReference(t, old, "json", in)
		st, se, su := parseChargedWith(t, spiked, in, BytecodeIterative)
		if ot != st || oe != se || ou != su {
			t.Errorf("input %q: closure tree %s err %s spend %d; flow tree %s err %s spend %d", in, ot, oe, ou, st, se, su)
		}
		ct, _, _ := parseChargedWith(t, called, in, BytecodeIterative)
		changed = changed || ct != ot
	}
	if !changed {
		t.Error("a changed call in the Flow body changed no tree: the iterative model does not run it")
	}
}

// TestFlowBodyIterativeDeepNesting checks that the iterative model runs a Flow body without host
// recursion: JSON arrays nested deeper than a small host stack allows the recursive model, whose
// value rule runs its Flow body at every level, parse with the tree, error and spend that the
// iterative model gives them with the host stack of the test.
func TestFlowBodyIterativeDeepNesting(t *testing.T) {
	old, spiked := jsonFlowPrograms(t, nil)
	const depth = 20000
	in := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	ot, oe, ou := parseChargedWith(t, old, in, BytecodeIterative)
	if oe != "<nil>" {
		t.Fatalf("the parse fails: %s", oe)
	}
	prev := debug.SetMaxStack(1 << 20) // too small for the recursive model
	st, se, su := parseChargedWith(t, spiked, in, BytecodeIterative)
	debug.SetMaxStack(prev)
	if ot != st || oe != se || ou != su {
		t.Errorf("err %s spend %d; with a small stack err %s spend %d; trees equal: %v", oe, ou, se, su, ot == st)
	}
}

// grammarCorpus returns the inputs of the grammar of the file path: the sample files next to it, and
// prefixes of each, which end the input inside its constructs and so take the failing paths.
func grammarCorpus(t *testing.T, path string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		in := string(data)
		out = append(out, in)
		for _, k := range []int{1, 2, 3, 5, 8} {
			if cut := len(in) * k / 9; cut > 0 && utf8.ValidString(in[:cut]) {
				out = append(out, in[:cut])
			}
		}
	}
	return out
}

// TestFlowBodiesTestdataGrammars runs the samples of the testdata grammars, and prefixes of them,
// through both VM models, which run every rule from its Flow body: the trees, the errors and the
// spends must be those of flowReference, and the module must load.
func TestFlowBodiesTestdataGrammars(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	total := 0
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			old, flows, n := flowPrograms(t, path, func(string) bool { return true }, nil)
			total += n
			// The loader accepts the module with its Flow bodies: their calls close no cycle of
			// calls through a rule that is not marked Rec.
			if err := program.ValidateModule(flows.Module()); err != nil {
				t.Fatalf("module with %d Flow bodies: %v", n, err)
			}
			for _, in := range grammarCorpus(t, path) {
				for _, b := range []Backend{Bytecode, BytecodeIterative} {
					ot, oe, ou := flowReference(t, old, filepath.Base(filepath.Dir(path)), in)
					ft, fe, fu := parseChargedWith(t, flows, in, b)
					if ot != ft || oe != fe || ou != fu {
						t.Errorf("%v, input of %d bytes: closure err %s spend %d; flow err %s spend %d; trees equal: %v", b, len(in), oe, ou, fe, fu, ot == ft)
					}
				}
			}
			t.Logf("%d rules run their Flow bodies", n)
		})
	}
	if total == 0 {
		t.Fatal("no rule of the testdata grammars has a Flow body")
	}
}

// TestFlowBodyOperatorsLines requires every skip and line of the Operators Rules of the testdata
// grammars that have them to get a Flow body, which TestFlowBodiesTestdataGrammars then runs in both
// models.
func TestFlowBodyOperatorsLines(t *testing.T) {
	for _, g := range []string{"cel", "golang", "python"} {
		t.Run(g, func(t *testing.T) {
			_, flows, _ := flowPrograms(t, "../../../testdata/grammars/"+g+"/"+g+".sxir", func(string) bool { return true }, nil)
			m := flows.Module()
			lines := 0
			for i, p := range m.OperatorsRules {
				for j, l := range p.Operands {
					if l.Flow == nil {
						t.Errorf("operand line %d of Operators Rule %d has no Flow body", j, i)
					}
					lines++
				}
				for _, o := range slices.Concat(p.Prefix, p.InfixPostfix) {
					if o.Line.Flow == nil {
						t.Errorf("operator %d of Operators Rule %d has no Flow body", o.ID, i)
					}
					lines++
				}
			}
			if lines == 0 {
				t.Fatal("the grammar has no Operators Rule line")
			}
		})
	}
}

// TestFlowBodyFrames runs rules whose framed bodies keep the captures of repetition elements in
// frames of their own (NEWFRAME, SETFRAME, CLEARVALS, FRAMEVAL, ATTACHFRAME) through both VM models:
// the trees and the errors must be those of the closure backend. An element whose optional capture does
// not match must not keep the capture of the element before it, and a capture of the whole element
// wraps its value.
func TestFlowBodyFrames(t *testing.T) {
	grammars := []struct {
		src    string
		inputs []string
	}{
		{`rule main = items:(k:"a"? v:"b")+ pairs:(whole:(x:"c" "d") y:"e"?)* $$`,
			[]string{"ab", "abbab", "abb", "bbb", "abcd", "abcdecd", "abcdcde", "abcdx", "ac", "", "a"}},
		{`package p
type X = node[string]
rule main = ms:members $$ => $ms
rule members = (first:item rest:(-";" m:item)*) => concat(list($first), map($rest, (r) => r.$m))
rule item X = "x"`,
			[]string{"", "x", "x;", "x;x;x", "x;;x", "x;y", "xx"}},
	}
	ops := map[bytecode.FlowOp]bool{}
	for _, g := range grammars {
		old, flows, n := flowProgramsOf(t, g.src, func(string) bool { return true }, nil)
		if n == 0 {
			t.Fatalf("%s: no rule runs a Flow body", g.src)
		}
		for _, f := range flows.src.(flowSource).m.Flows {
			for _, in := range f.Code {
				ops[in.Op] = true
			}
		}
		for _, in := range g.inputs {
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				ot, oe, _ := parseChargedWith(t, old, in, Closure)
				ft, fe, _ := parseChargedWith(t, flows, in, b)
				if ot != ft || oe != fe {
					t.Errorf("%v, input %q: closure tree %s err %s; flow tree %s err %s", b, in, ot, oe, ft, fe)
				}
			}
		}
	}
	for _, op := range []bytecode.FlowOp{bytecode.FlowNewFrame, bytecode.FlowSetFrame, bytecode.FlowClearVals, bytecode.FlowFrameVal,
		bytecode.FlowAttachFrame, bytecode.FlowSetCapture, bytecode.FlowMarkPos, bytecode.FlowFrame, bytecode.FlowShapeCapture} {
		if !ops[op] {
			t.Errorf("no Flow body runs %v", op)
		}
	}
}

// streamElements runs a stream parse of input from the start rule of prog on the backend b and
// returns the elements it delivers, one per line, and the error.
func streamElements(t *testing.T, prog *Program, input string, b Backend) (string, string) {
	t.Helper()
	var out strings.Builder
	err := prog.ParseStreamWith(prog.ResolveStart(""), strings.NewReader(input), func(n *value.Node) error {
		if n == nil {
			out.WriteString("<nil>\n")
		} else {
			out.WriteString(n.String() + "\n")
		}
		return nil
	}, ParseOptions{Backend: b})
	msg := "<nil>"
	if err != nil {
		msg = err.Error()
	}
	return out.String(), msg
}

// TestFlowBodyStreams runs stream parses of grammars whose start rule has a #stream repetition through
// both VM models, with the start rule's stream body selected from its Flow form: the delivered
// elements and the errors must be those of the closure backend, and so must the result of a batch
// parse, which runs the rule body instead.
func TestFlowBodyStreams(t *testing.T) {
	csv, err := os.ReadFile("../../../testdata/grammars/csv/csv.sxir")
	if err != nil {
		t.Fatal(err)
	}
	grammars := []struct {
		name, src string
		inputs    []string
	}{
		{"csv", string(csv), append(grammarCorpus(t, "../../../testdata/grammars/csv/csv.sxir"), "", "a,b\r\n", "a,\"b\r\n")},
		// The elements discard their text.
		{"lines", "rule main = line* #stream $$\nrule line = -[a-z]+ -\"\\n\"\n", []string{"", "ab\n", "ab\ncd\n", "ab\ncd", "a\n\n"}},
	}
	for _, g := range grammars {
		t.Run(g.name, func(t *testing.T) {
			old, flows, _ := flowProgramsOf(t, g.src, func(string) bool { return true }, nil)
			m := flows.Module()
			start := -1
			for i, ri := range m.Rules {
				if m.Strings[ri.Name] == flows.ResolveStart("") {
					start = i
				}
			}
			if start < 0 || m.Streams[start] == nil {
				t.Fatalf("the start rule has no stream body (streams %v)", slices.Collect(maps.Keys(m.Streams)))
			}
			for _, in := range g.inputs {
				for _, b := range []Backend{Bytecode, BytecodeIterative} {
					oe, oerr := streamElements(t, old, in, Closure)
					fe, ferr := streamElements(t, flows, in, b)
					if oe != fe || oerr != ferr {
						t.Errorf("%v, input %q: closure elements\n%s err %s; flow elements\n%s err %s", b, in, oe, oerr, fe, ferr)
					}
					ot, oerr, _ := parseChargedWith(t, old, in, Closure)
					ft, ferr, _ := parseChargedWith(t, flows, in, b)
					if ot != ft || oerr != ferr {
						t.Errorf("%v, batch input %q: closure err %s; flow err %s; trees equal: %v", b, in, oerr, ferr, ot == ft)
					}
				}
			}
		})
	}
}
