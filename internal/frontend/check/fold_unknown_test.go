package check

import (
	"strings"
	"testing"
)

// unknownList is a rule whose type keeps growing, so that it is widened to the type that is not
// known, while each of its values is a list.
const unknownList = `rule g = ("(" x:g ")" => list(list($x[0]))) / ("a" => list(1))
`

// TestFoldAccumulatorOfUnknownBody checks that a foldl or foldr whose callback gives a value of the
// type that is not known has an accumulator of that type: the callback fits the initial value's
// type only because the check of its value moves to run time, so the result may be any value, and
// a declared type then asks for a cast.
func TestFoldAccumulatorOfUnknownBody(t *testing.T) {
	t.Parallel()
	for _, fold := range []string{"foldl", "foldr"} {
		p := mustCompile(t, unknownList+"rule m = x:g => "+fold+"(0, $x, (a, e) => e)\nrule k = x:g => "+fold+"(0, $x, (a, e) => a + len(e))")
		if got, _ := inferredType(p, "m"); got != "unknown" {
			t.Errorf("%s: m type = %s; want unknown", fold, got)
		}
		// A callback whose value is known keeps the accumulator.
		if got, _ := inferredType(p, "k"); got != "int" {
			t.Errorf("%s: k type = %s; want int", fold, got)
		}
		got := compileError(t, unknownList+"rule n int = x:g => "+fold+"(0, $x, (a, e) => e)")
		if want := "cannot use unknown as int in result of rule n"; !strings.Contains(got, want) {
			t.Errorf("%s: got %s, want %s", fold, got, want)
		}
		mustCompile(t, unknownList+"rule n int = x:g => "+fold+"(0, $x, (a, e) => e).(int)")
	}
}
