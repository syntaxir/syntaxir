package vmlower_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/vmlowertest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

func grammarProgram(t *testing.T, path string) *frontend.Result {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return compiletest.Program(t, string(src), check.Options{})
}

// compileGrammar compiles prog to a bytecode module, and returns the planned Node table it was
// lowered from (vmInput) and the matches of each pattern of statements of the selection table.
func compileGrammar(t *testing.T, prog *frontend.Result) (*plandata.Table[code.Flow], *bytecode.Module, map[string]int) {
	t.Helper()
	in := vmInput(t, prog)
	bc, counts, err := vmlowertest.Lower(in)
	if err != nil {
		t.Fatal(err)
	}
	return in.Table, bc, counts
}

// ruleID returns the ID of the rule name in the table's Core IR.
func ruleID(t *testing.T, ft *plandata.Table[code.Flow], name string) int {
	t.Helper()
	for i, r := range ft.IR.M.Rules {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("no rule %s", name)
	return -1
}

// ruleIDs returns the IDs of the rules whose bodies the table holds, in increasing order.
func ruleIDs(ft *plandata.Table[code.Flow]) []int {
	var ids []int
	for _, s := range ft.Plan.Funcs {
		if s.Key.Use == plandata.RuleBody {
			ids = append(ids, s.Key.Rule)
		}
	}
	slices.Sort(ids)
	return ids
}

// TestSelectJSONValueFlow selects the body of the JSON value rule, a framed body of the plan.
func TestSelectJSONValueFlow(t *testing.T) {
	prog := grammarProgram(t, "../../../testdata/grammars/json/json.sxir")
	ft, bc, _ := compileGrammar(t, prog)
	id := ruleID(t, ft, "value")
	f := bc.Flows[id]
	if f == nil {
		t.Fatal("value has no Flow body")
	}
	t.Logf("value: %d registers, %d instructions", f.Regs, len(f.Code))
}

// TestEveryJSONRuleHasAFlowBody checks that every rule body of the JSON grammar's table is selected,
// and that every rule of the module, value-free twins among them, runs a Flow body.
func TestEveryJSONRuleHasAFlowBody(t *testing.T) {
	prog := grammarProgram(t, "../../../testdata/grammars/json/json.sxir")
	ft, bc, _ := compileGrammar(t, prog)
	for _, id := range ruleIDs(ft) {
		if bc.Flows[id] == nil {
			t.Errorf("%s has no Flow body", ft.IR.M.Rules[id].Name)
		}
	}
	for i, r := range bc.Rules {
		if bc.Flows[i] == nil {
			t.Errorf("rule %s has no Flow body", bc.Strings[r.Name])
		}
	}
}

// TestSelectFlowsChecksTheLayout checks that a body is selected only into a rule frame whose slots
// are the capture layout that the plan gave the rule.
func TestSelectFlowsChecksTheLayout(t *testing.T) {
	prog := grammarProgram(t, "../../../testdata/grammars/json/json.sxir")
	in := vmInput(t, prog)
	id := -1
	for i, r := range in.Table.IR.M.Rules {
		if r.Name == "main" { // its frame has the slot of a capture
			id = i
		}
	}
	if id < 0 || len(in.Table.IR.M.Rules[id].Scope) == 0 {
		t.Fatal("main has no capture slot")
	}
	in.Table.IR.M.Rules[id].Scope = nil
	if _, _, err := vmlowertest.Lower(in); err == nil {
		t.Fatal("a body selected into a frame without its planned slots")
	}
}

// TestSelectedFlowsSurviveTheFileForm compiles the testdata grammars and checks that each module
// validates and that its bodies and scopes read back from the module file as they were written.
func TestSelectedFlowsSurviveTheFileForm(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	total := 0
	for _, path := range paths {
		_, bc, _ := compileGrammar(t, grammarProgram(t, path))
		total += len(bc.Flows)
		if err := program.ValidateModule(bc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		w := &bytecode.Encoder{Strs: map[string]int{}}
		w.WriteModule(bc)
		r := &bytecode.Decoder{Data: w.Body.Bytes(), Strs: bc.Strings}
		got := r.ReadModule()
		if r.Err != nil {
			t.Fatalf("%s: %v", path, r.Err)
		}
		if !reflect.DeepEqual(got.Flows, bc.Flows) || !reflect.DeepEqual(got.Streams, bc.Streams) ||
			!reflect.DeepEqual(got.OperatorsRules, bc.OperatorsRules) || !reflect.DeepEqual(got.Scopes, bc.Scopes) {
			t.Errorf("%s: the Flow bodies read back differ from those written", path)
		}
	}
	if total == 0 {
		t.Fatal("no rule of the testdata grammars has a Flow body")
	}
	t.Logf("%d Flow bodies", total)
}
