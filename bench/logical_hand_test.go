package bench

import (
	"testing"

	ghand "syntaxir.dev/sxir/internal/bench/parsers/handlogical"
	gjsonlogical "syntaxir.dev/sxir/internal/bench/parsers/jsonlogical"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	rt "syntaxir.dev/sxir/pkg/runtime/libgo"
)

// TestLogicalHandCounts checks the counts of logical units of the JSON grammar against totals
// counted by hand from the grammar (testdata/grammars/json/json.sxir) and the event table of design
// record 076.
func TestLogicalHandCounts(t *testing.T) {
	for _, c := range []struct {
		in   string
		want uint64
		why  string
	}{
		// main ws value object array string number int frac exp ws: 11 calls; "1": 1.
		{`1`, 12, "11 calls + 1 input"},
		// main ws value object array string number int bool ws: 10 calls; "true": 4.
		{`true`, 14, "10 calls + 4 input"},
		// main ws value object array string number int bool null ws: 11 calls; "null": 4.
		{`null`, 15, "11 calls + 4 input"},
		// main ws value object array string chars escape ws: 9 calls (escape fails at the closing quote,
		// its dispatch guard disabled); `"ab"`: 4; the action builds no value.
		{`"ab"`, 13, "9 calls + 4 input"},
		// A failed parse: main ws value object array string number int bool null: 10 calls, no input
		// (a literal that fails advances over nothing), and the syntax error: 1.
		{`tru`, 11, "10 calls + 1 error"},
		// [1,2]: calls main ws value object array ws elements value object array string number int
		// frac exp ws ws value object array string number int frac exp ws ws: 28; input 5. The
		// action of elements, concat(list($first), map($rest, (r) => r.$v)) fused: list builds 1,
		// map visits 1 and builds 1, concat builds 2; that of array, concat($es): builds 2.
		{`[1,2]`, 40, "28 calls + 5 input + 7 action"},
	} {
		for _, u := range []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints} {
			var a, b uint64
			_, err1 := gjsonlogical.Parse(c.in, rt.WithUnit(u), rt.WithUnitsReport(&a))
			_, err2 := gjsonlogical.ParseValue(c.in, rt.WithUnit(u), rt.WithUnitsReport(&b))
			if (err1 == nil) != (c.why != "10 calls + 1 error") || (err2 == nil) != (err1 == nil) {
				t.Fatal(c.in, err1, err2)
			}
			if a != c.want || b != c.want {
				t.Errorf("%s (%v): Parse %d, ParseValue %d units, want %d (%s)", c.in, u, a, b, c.want, c.why)
			}
		}
	}
}

// TestLogicalHandCountsActions checks the counts of the actions, predicates and comparisons of
// bench/grammars/logical_hand.sxir against totals counted by hand.
func TestLogicalHandCountsActions(t *testing.T) {
	for _, c := range []struct {
		rule, in string
		want     uint64
		why      string
	}{
		// Calls: tag, name x3 = 4. Input: 12. Predicate text($a) == text($c): 2 (shorter operand).
		// Action text($a) + text($b): a string of 5 bytes. Total 4 + 12 + 2 + 5.
		{"tag", "<ab>xyz</ab>", 23, "4 calls + 12 input + 2 compared + 5 built"},
		// Calls: nums, num x3 = 4. Input: 7. Action: list($first) builds 1; map($rest, (r) => r.$n)
		// visits 2 and builds 2; concat builds 3; filter visits 3, compares 3 strings of 1 byte with
		// "0" (1 each) and builds 2. Total 4 + 7 + 1 + 4 + 3 + 3 + 3 + 2.
		{"nums", "[1,0,2]", 27, "4 calls + 7 input + 16 action"},
	} {
		for _, u := range []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints} {
			var n uint64
			if _, err := ghand.ParseRule(c.rule, c.in, rt.WithUnit(u), rt.WithUnitsReport(&n)); err != nil {
				t.Fatal(c.rule, err)
			}
			if n != c.want {
				t.Errorf("%s %q (%v): %d units, want %d (%s)", c.rule, c.in, u, n, c.want, c.why)
			}
		}
	}
}
