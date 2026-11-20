package print_test

import (
	"math"
	"testing"

	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/complexitytest"
)

// operatorsBoundMutation drops the bound of the Operators Rule loop from both parsers of the
// embedded library, the untyped and the typed: a level then tries again every operator part that a
// level ending at the same position refused.
var operatorsBoundMutation = [][2]string{
	{"level <= min || level > bound ||", "level <= min || level > bound && false ||"},
	{"level <= min || level > bound ||", "level <= min || level > bound && false ||"},
}

// TestOperatorsRefusedPartGeneratedLinear checks the cost contract of an Operators Rule that nests
// in generated parsers: on the probe of complexitytest, the levels that end at one position do not
// parse the right operand of a part that one of them refused again, so the spend is linear, and a
// library without the bound of the loop makes it quadratic.
func TestOperatorsRefusedPartGeneratedLinear(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testOperatorsRefusedPartGeneratedLinear)
}

func testOperatorsRefusedPartGeneratedLinear(t *testing.T) {
	goBin := skipRunsGoBin(t)
	codegentest.NeedsBudgetControls(t)
	controls := []complexitytest.Control{{Name: "Operators Rule refused part", Source: complexitytest.OperatorsRefusedPartGrammar, Input: complexitytest.OperatorsRefusedPartInput}}
	sizes := []int{100, 200, 400}
	slopes := func(row []uint64) (lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for i := 1; i < len(row); i++ {
			s := math.Log2(float64(row[i]) / float64(row[i-1]))
			lo, hi = min(lo, s), max(hi, s)
		}
		return lo, hi
	}
	spends, _ := libSpends(t, goBin, controls, sizes, nil, "operators_exec.go", nil)
	t.Logf("spends at %v: %v", sizes, spends)
	if _, hi := slopes(spends[0]); hi > 1.2 {
		t.Errorf("spends %v grow faster than linearly", spends[0])
	}
	spends, _ = libSpends(t, goBin, controls, sizes, nil, "operators_exec.go", operatorsBoundMutation)
	t.Logf("spends without the bound: %v", spends)
	if lo, _ := slopes(spends[0]); lo < 1.7 {
		t.Errorf("without the bound: spends %v are not quadratic", spends[0])
	}
}
