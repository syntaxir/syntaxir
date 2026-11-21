package vmlower_test

import (
	"bytes"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// selectGrammar compiles the grammar at path from scratch and selects the body of each of its rules
// that has one, one rule at a time; it returns the module file body, the module's strings and the
// matches of each pattern summed over the rules.
func selectGrammar(t *testing.T, path string) (file []byte, strs []string, counts []vmlower.PatternCount) {
	t.Helper()
	prog := grammarProgram(t, path)
	ft, bc := flowTable(t, prog)
	for _, id := range slices.Sorted(maps.Keys(ft.Bodies)) {
		got, err := vmlower.SelectFlows(bc, vmlower.Input{Run: prog.Runtime(), Elab: prog}, ft.IR.M, ft.Plan, map[int]*code.Module{id: ft.Bodies[id]})
		if err != nil {
			continue // an operation without a bytecode Flow form yet
		}
		if counts == nil {
			counts = got
			continue
		}
		for i := range got {
			if got[i].Pattern != counts[i].Pattern {
				t.Fatalf("pattern %d is %s in one selection and %s in another", i, got[i].Pattern, counts[i].Pattern)
			}
			counts[i].Matches += got[i].Matches
		}
	}
	w := &bytecode.Encoder{Strs: map[string]int{}}
	w.WriteModule(bc)
	return w.Body.Bytes(), bc.Strings, counts
}

// TestSelectionIsDeterministic selects the bodies of the testdata grammars twice, each time from a
// fresh compilation, and requires the same module file and the same pattern counts: the selection
// depends on nothing but the bodies, not on the order of a map or on a previous compilation.
func TestSelectionIsDeterministic(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	for _, path := range paths {
		f1, s1, c1 := selectGrammar(t, path)
		f2, s2, c2 := selectGrammar(t, path)
		if !bytes.Equal(f1, f2) || !slices.Equal(s1, s2) {
			t.Errorf("%s: two selections wrote different module files", path)
		}
		if !reflect.DeepEqual(c1, c2) {
			t.Errorf("%s: two selections counted %v and %v", path, c1, c2)
		}
	}
}

// TestSuperinstructionsAreSelected requires each pattern of statements of the selection table to
// match in the bodies of the testdata grammars, and the instruction it selects to be in a body: a
// pattern that never matches is a pattern whose shape the Flow functions do not have.
func TestSuperinstructionsAreSelected(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	matches := map[string]int{}
	ops := map[bytecode.FlowOp]bool{}
	for _, path := range paths {
		prog := grammarProgram(t, path)
		ft, bc := flowTable(t, prog)
		for _, id := range slices.Sorted(maps.Keys(ft.Bodies)) {
			counts, err := vmlower.SelectFlows(bc, vmlower.Input{Run: prog.Runtime(), Elab: prog}, ft.IR.M, ft.Plan, map[int]*code.Module{id: ft.Bodies[id]})
			if err != nil {
				continue
			}
			for _, c := range counts {
				matches[c.Pattern] += c.Matches
			}
		}
		for _, f := range bc.Flows {
			for _, in := range f.Code {
				ops[in.Op] = true
			}
		}
	}
	for pattern, op := range map[string]bytecode.FlowOp{
		"matchClass": bytecode.FlowMatchClass, "guardJump": bytecode.FlowGuardJump, "guardLitJump": bytecode.FlowGuardLitJump,
		"assertJump":   bytecode.FlowAssertJump,
		"matchLitJump": bytecode.FlowMatchLitJump, "callJump": bytecode.FlowCallJump,
	} {
		if matches[pattern] == 0 || !ops[op] {
			t.Errorf("pattern %s matched %d times and %v is in no body", pattern, matches[pattern], op)
		}
	}
	if !ops[bytecode.FlowJumpTrue] {
		t.Error("no condition is selected as a JUMPTRUE")
	}
	t.Logf("matches: %v", matches)
}
