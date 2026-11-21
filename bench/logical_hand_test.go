package bench

import (
	"testing"

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
	} {
		for _, u := range []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints} {
			var a, b uint64
			if _, err := gjsonlogical.Parse(c.in, rt.WithUnit(u), rt.WithUnitsReport(&a)); err != nil {
				t.Fatal(err)
			}
			if _, err := gjsonlogical.ParseValue(c.in, rt.WithUnit(u), rt.WithUnitsReport(&b)); err != nil {
				t.Fatal(err)
			}
			if a != c.want || b != c.want {
				t.Errorf("%s (%v): Parse %d, ParseValue %d units, want %d (%s)", c.in, u, a, b, c.want, c.why)
			}
		}
	}
}
