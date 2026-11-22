package vmlower_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	flowpkg "syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// selectGrammar compiles the grammar at path from scratch; it returns the module file body, the
// module's strings and the matches of each pattern.
func selectGrammar(t *testing.T, path string) (file []byte, strs []string, counts []vmlower.PatternCount) {
	t.Helper()
	_, bc, counts := compileGrammar(t, grammarProgram(t, path))
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
		_, bc, counts := compileGrammar(t, grammarProgram(t, path))
		for _, c := range counts {
			matches[c.Pattern] += c.Matches
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
		"scanReachAt": bytecode.FlowScanReachAt, "scanEndAt": bytecode.FlowScanEndAt,
	} {
		if matches[pattern] == 0 || !ops[op] {
			t.Errorf("pattern %s matched %d times and %v is in no body", pattern, matches[pattern], op)
		}
	}
	// A walk of a scan site compares its count with the span of the parse only in SCANREACHAT and
	// SCANENDAT: selected alone, the comparison takes the span that the lowering assumed
	// (flow.ScanHotSpan), and a parse tuned to another span would look for no record. Every walk of
	// the grammars, a Document's resumable ones included, must be selected whole.
	for _, op := range []string{flowpkg.OpScanReach, flowpkg.OpScanEnd} {
		if matches[op] != 0 {
			t.Errorf("%d walks of scan sites select %s apart from the test of their span", matches[op], op)
		}
	}
	if !ops[bytecode.FlowJumpTrue] {
		t.Error("no condition is selected as a JUMPTRUE")
	}
	t.Logf("matches: %v", matches)
}

// TestValuesAreSelectedIntoLocals requires the selection to write each value that a local takes into
// the local's register: no instruction of the bodies of the testdata grammars writes a register that
// the next instruction only copies elsewhere, with no other instruction of the body reading it.
func TestValuesAreSelectedIntoLocals(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	for _, path := range paths {
		_, bc, _ := compileGrammar(t, grammarProgram(t, path))
		for rule, f := range bc.Flows {
			reads := map[int]int{} // the number of instructions that read each register
			for _, in := range f.Code {
				for _, a := range in.Args {
					reads[a]++
				}
			}
			for i := 1; i < len(f.Code); i++ {
				prev, in := f.Code[i-1], f.Code[i]
				if in.Op == bytecode.FlowMove && len(prev.Dst) == 1 && prev.Dst[0] == in.Args[0] && reads[in.Args[0]] == 1 {
					t.Errorf("%s: rule %s: %v at %d writes r%d only for the MOVE after it", path, bc.Strings[bc.Rules[rule].Name], prev.Op, i-1, in.Args[0])
					break
				}
			}
		}
	}
}
