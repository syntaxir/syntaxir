package stat

import (
	"math"
	"testing"
)

func TestGeomean(t *testing.T) {
	if got := Geomean(nil); got != 1 {
		t.Errorf("Geomean(nil) = %v, want 1", got)
	}
	if got := Geomean([]float64{0.5, 2}); math.Abs(got-1) > 1e-12 {
		t.Errorf("Geomean(0.5, 2) = %v, want 1", got)
	}
	if got := Geomean([]float64{4, 9}); math.Abs(got-6) > 1e-12 {
		t.Errorf("Geomean(4, 9) = %v, want 6", got)
	}
}

func TestMedianAndRotate(t *testing.T) {
	xs := []float64{3, 1, 2}
	if got := Median(xs); got != 2 || xs[0] != 3 {
		t.Errorf("Median = %v (input changed: %v)", got, xs)
	}
	if got := Median([]float64{1, 2, 3, 4}); got != 2.5 {
		t.Errorf("Median of four = %v", got)
	}
	items := []string{"a", "b", "c"}
	for round, want := range []string{"abc", "bca", "cab", "abc"} {
		got := ""
		for _, s := range Rotate(items, round) {
			got += s
		}
		if got != want {
			t.Errorf("round %d: %s, want %s", round, got, want)
		}
	}
}

// series returns n ratios around centre with a deterministic ripple of the given amplitude.
func series(n int, centre, amp float64) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = centre * (1 + amp*math.Sin(float64(i)*2.399))
	}
	return xs
}

func TestJudge(t *testing.T) {
	aa := series(20, 1, 0.004)
	for _, c := range []struct {
		name   string
		change []float64
		aa     []float64
		want   Verdict
	}{
		{"same as base", series(20, 1, 0.004), aa, Within},
		{"a change inside the spread", series(20, 1.002, 0.004), aa, Within},
		{"a clear slowdown", series(20, 1.05, 0.004), aa, Regression},
		{"a clear speedup", series(20, 0.93, 0.004), aa, Improvement},
		{"a slowdown that noise could make", series(20, 1.03, 0.03), series(20, 1, 0.03), Invalid},
		{"noisy but A/A small", series(20, 1.05, 0.2), aa, Within},
	} {
		if got := Judge(c.change, c.aa, 0.015).Verdict; got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEnough(t *testing.T) {
	aa := series(20, 1, 0.004)
	if Enough(series(5, 1.05, 0.004), aa[:5], 8, 0.015) {
		t.Error("enough before the minimum of rounds")
	}
	if !Enough(series(20, 1.05, 0.002), aa, 8, 0.015) {
		t.Error("a narrow interval at 20 rounds is not enough")
	}
	if Enough(series(8, 1.0, 0.02), series(8, 1, 0.01), 8, 0.015) {
		t.Error("a wide interval around 1 counts as enough")
	}
	if Enough(series(8, 1.0, 0.02), series(8, 1, 0.04), 8, 0.015) {
		t.Error("an invalid A/A spread of a few rounds ends the run: validity is decided at the end")
	}
	// The rule does not look at the location: the same ripple around 1 and around 1.2 gives the same answer.
	for _, centre := range []float64{1, 1.2} {
		if got := Enough(series(12, centre, 0.002), series(12, 1, 0.004), 8, 0.015); got != Enough(series(12, 1, 0.002), series(12, 1, 0.004), 8, 0.015) {
			t.Errorf("the answer depends on the location %v", centre)
		}
	}
}
