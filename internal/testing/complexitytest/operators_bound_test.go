package complexitytest

import "testing"

// TestOperatorsRefusedPartLinear checks on every backend that the levels of an Operators Rule
// that end at one position do not parse the right operand of an operator part again once one of
// them refused it.
func TestOperatorsRefusedPartLinear(t *testing.T) {
	t.Parallel()
	skipWithoutBudget(t)
	p, err := Compile(OperatorsRefusedPartGrammar)
	if err != nil {
		t.Fatal(err)
	}
	sizes := double(250)
	if reduced() {
		sizes = double(100)
	}
	for _, b := range Backends {
		s, err := Measure(p, OperatorsRefusedPartInput, sizes, b, Cap)
		if err != nil {
			t.Fatalf("%s: %v", BackendName(b), err)
		}
		if got := Classify(s); got != Linear {
			t.Errorf("%s: %v (slopes %.2f) classified %v, want Linear", BackendName(b), s.Costs, s.Slopes(), got)
		}
	}
}
