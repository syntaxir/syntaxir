package complexity

import "testing"

func TestBudgetDerivation(t *testing.T) {
	for _, c := range []struct {
		name, src string
		per       uint64
	}{
		// "ab" counts its two bytes and an error, "c" one byte and an error; the start rule is
		// evaluated twice at a position (Lemma 3).
		{"literals", "@start\nrule main = \"ab\" \"c\" $$\n", 10},
		// A class counts four units and an error; the repetition covers each position twice.
		{"repetition", "@start\nrule main = [a-z]* $$\n", 20},
		// a is not memoized, so its two call sites evaluate it twice each: main costs 2 * 2 for its
		// two calls and a costs 4 * 2 for its literal (Lemma 10).
		{"transient rule", "@start\nrule main = a a $$\nrule a = \"x\"\n", 12},
		// e is in a cycle of one rule, so it is evaluated 2 * (1 + 1) times: 4 * 7 for its call and
		// three literals, and 2 * 1 for the call of e in main (Lemma 5).
		{"left recursion", "@start\nrule main = e $$\nrule e = e \"+\" \"a\" / \"a\"\n", 30},
		// The inner repetition counts its body twice, and the outer one its body twice, the inner
		// repetition's body included: 2 + 2 + 2 + 2 + 2.
		{"nested repetitions", "@start\nrule main = (\"a\" \"b\"*)* $$\n", 20},
		// Two captures, two literals and the action: 1 + 2 + 1 + 2 + 1, evaluated twice.
		// "a" and ";" count 2 each, the recovery 2 (the error built, and the error charged again at a
		// take, Lemma 14), its skip a class under a repetition (4 + 1, twice) and ";" (2), and the
		// take at the store that ends the evaluation 1: 2 + 2 + 2 + 10 + 2 + 1, evaluated twice.
		{"recovery", "@start\nrule main = (\"a\" \";\") #recover(skip=[^;]* \";\") $$\n", 38},
		// With a recovery, a call adds 1 for the list that it may record: main is 2 for the call and 1
		// for the take, evaluated twice; a is "x" 2, the recovery 2, its skip 12 and the take 1,
		// evaluated twice, as main evaluates it twice and calls it once (Lemma 10): 6 + 34.
		{"recovery with a call", "@start\nrule main = a $$\nrule a = (\"x\" #recover(skip=[^;]* \";\"))\n", 40},
		{"captures and action", "@start\nrule main = x:\"a\" y:\"b\" => x\n", 14},
	} {
		got := classifySource(t, c.src)
		if got.Class != ClassLinear {
			t.Fatalf("%s: class %v", c.name, got.Class)
		}
		if got.Budget == nil {
			t.Fatalf("%s: no budget", c.name)
		}
		t.Logf("%s: %+v", c.name, *got.Budget)
		if got.Budget.PerUnit != c.per || got.Budget.Fixed != c.per+budgetEntry {
			t.Errorf("%s: budget %+v, want per unit %d", c.name, *got.Budget, c.per)
		}
	}
}

func TestBudgetOnlyForLinear(t *testing.T) {
	for _, src := range []string{
		"@start\nrule main = ( \"a\"* \"b\" / \"a\" )* $$\n", // polynomial
	} {
		got := classifySource(t, src)
		if got.Class == ClassLinear || got.Budget != nil {
			t.Errorf("class %v, budget %v", got.Class, got.Budget)
		}
	}
}

// TestBudgetCombinations checks that the bound includes K, the number of combinations of the
// values of the variables with finite values.
func TestBudgetCombinations(t *testing.T) {
	got := classifySource(t, "var v int = 0\n@start\nrule main = {v := 1} a $$\nrule a = {v == 1} \"x\"\n")
	if got.Class != ClassLinear || got.Budget == nil || got.Combinations != 2 || got.Budget.Combinations != 2 ||
		got.Budget.PerUnit != 20 || got.Budget.Fixed != 22 {
		t.Errorf("class %v, K %d, budget %+v", got.Class, got.Combinations, got.Budget)
	}
}

func TestBudgetUnits(t *testing.T) {
	b := Budget{PerUnit: 20, Fixed: 22}
	if u, ok := b.Units(10); !ok || u != 222 {
		t.Errorf("Units(10) = %d, %v", u, ok)
	}
	if _, ok := b.Units(1 << 63); ok {
		t.Error("Units(2^63) fits a counter")
	}
}

// TestBudgetMemoKey checks the term of the memo key: a rule that reads a variable and that the
// memo answers adds one unit for each call of it, besides the two predicates that read and bind
// the variable. Both grammars have one start rule that calls a twice, and a that calls b; a is
// memoized because it has a call and two call sites. Per unit of K the key adds
// 2 sites * 2 evaluations of main = 4, the predicate in main 1 * 2 = 2 and the one in a 1 * 2 = 2.
func TestBudgetMemoKey(t *testing.T) {
	plain := classifySource(t, "@start\nrule main = a \"1\" a \"2\" $$\nrule a = b\nrule b = \"x\"\n")
	keyed := classifySource(t, "var v int = 0\n@start\nrule main = {v := 1} a \"1\" a \"2\" $$\nrule a = {v == 1} b\nrule b = \"x\"\n")
	if plain.Budget == nil || keyed.Budget == nil {
		t.Fatalf("budgets %v %v, classes %v %v", plain.Budget, keyed.Budget, plain.Class, keyed.Class)
	}
	if !plain.Memoized["a"] || !keyed.Memoized["a"] {
		t.Fatalf("a is not memoized: %v %v", plain.Memoized, keyed.Memoized)
	}
	if want := plain.Budget.PerUnit + 8; keyed.Budget.PerUnit != want*uint64(keyed.Combinations) {
		t.Errorf("keyed per unit %d with K = %d, want %d times K (plain %d)", keyed.Budget.PerUnit, keyed.Combinations, want, plain.Budget.PerUnit)
	}
}
