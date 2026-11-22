package check_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// deepStructReads returns the reads of type declarations that the zero-value decision of a chain of
// depth struct types, each holding the next twice, takes: the same type is reached by 2^depth paths.
// Whether a struct type has a zero value reads the declarations once for each struct type that it
// expands, so the count (check.CountTypeReads) is the work of that decision in steps, which the
// compiler's cancellation control does not poll after the build.
func deepStructReads(t *testing.T, depth int) uint64 {
	t.Helper()
	prog := deepStructProgram(t, depth)
	var reads int
	checker := check.NewChecker(prog.Model)
	check.CountTypeReads(checker, &reads)
	if !checker.HasZero(types.NamedTy{Name: "S0", Kind: 's'}) {
		t.Fatal("the chain has no zero value")
	}
	return uint64(reads)
}

// deepStructProgram compiles a chain of depth struct types, each holding the next twice, below Top.
func deepStructProgram(t *testing.T, depth int) *compile.Program {
	t.Helper()
	var src strings.Builder
	for i := range depth {
		fmt.Fprintf(&src, "type S%d = { X S%d; Y S%d }\n", i, i+1, i+1)
	}
	fmt.Fprintf(&src, "type S%d = { V int }\ntype Top = { K node[string]; In S0 }\n", depth)
	src.WriteString("@start\nrule main Top = k:\"x\" => Top{K: $k}\n")
	return compiletest.Program(t, src.String(), check.Options{}).Program
}

// growsLinearly reports whether a count for twice the depth is at most three times the count for the
// depth (a linear count doubles; an exponential one at the depths of the test grows by 2^10).
func growsLinearly(small, large uint64) bool { return large <= 3*small }

// TestZeroValueOfDeepStructTypesIsLinear pins that the zero descriptors of a chain of struct types, each
// holding the next twice, are found in a number of steps linear in the number of types: the same type is
// reached by 2^depth paths, and visiting every path takes minutes at depth 40. The steps are the reads of
// the type declarations (check.CountTypeReads), compared at depths 10 and 20, where a visit of every path
// still ends at once (2^20 reads) so that a regression fails the comparison instead of hanging the test.
func TestZeroValueOfDeepStructTypesIsLinear(t *testing.T) {
	t.Parallel()
	small, large := deepStructReads(t, 10), deepStructReads(t, 20)
	t.Logf("%d reads at depth 10, %d at depth 20", small, large)
	if !growsLinearly(small, large) {
		t.Errorf("%d reads at depth 10 and %d at depth 20: the zero-value decision grows faster than the depth", small, large)
	}
	// The descriptors themselves are those of the chain at the depth of the original check.
	prog := deepStructProgram(t, 40)
	if z := prog.Checker().StructZeroOf("Top"); z == nil || z.Zeros["In"] != "{S0}" || z.Zeros["K"] != check.NoZeroDesc {
		t.Errorf("descriptors of Top: %+v", z)
	}
}

// TestZeroGrowthDetectorSeesExponentialReads is the negative control of
// TestZeroValueOfDeepStructTypesIsLinear: the growth measure rejects counts that double with every
// level, the counts of a visit of every path, and accepts linear ones.
func TestZeroGrowthDetectorSeesExponentialReads(t *testing.T) {
	if growsLinearly(1<<10, 1<<20) {
		t.Error("exponential reads passed as linear")
	}
	if !growsLinearly(30, 60) {
		t.Error("linear reads were reported as growing faster")
	}
}
