package print_test

import (
	"math"
	"os/exec"
	"testing"

	"zeroparse.dev/zp/internal/testing/complexitytest"
)

// TestOperatorsRefusedPartTSLinear checks the cost contract of an Operators Rule that nests in
// generated TypeScript recognizers: on the probe of complexitytest, the levels that end at one
// position do not parse the right operand of a part that one of them refused again, so the spend
// is linear, and a library without the bound of the loop makes it quadratic.
func TestOperatorsRefusedPartTSLinear(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	sizes := []int{100, 200, 400}
	var in []string
	for _, n := range sizes {
		in = append(in, complexitytest.OperatorsRefusedPartInput(n))
	}
	sources, inputs := []string{complexitytest.OperatorsRefusedPartGrammar}, [][]string{in}
	slopes := func(row []uint64) (lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for i := 1; i < len(row); i++ {
			s := math.Log2(float64(row[i]) / float64(row[i-1]))
			lo, hi = min(lo, s), max(hi, s)
		}
		return lo, hi
	}
	spends, _ := tsSpends(t, node, sources, inputs, []string{}, nil, "recognizeWithOptions")
	t.Logf("spends at %v: %v", sizes, spends)
	if _, hi := slopes(spends[0]); hi > 1.2 {
		t.Errorf("spends %v grow faster than linearly", spends[0])
	}
	unbounded := [][2]string{{"if (level <= min || level > bound ||", "if (level <= min ||"}}
	spends, _ = tsSpends(t, node, sources, inputs, []string{}, unbounded, "recognizeWithOptions")
	t.Logf("spends without the bound: %v", spends)
	if lo, _ := slopes(spends[0]); lo < 1.7 {
		t.Errorf("without the bound: spends %v are not quadratic", spends[0])
	}
}
