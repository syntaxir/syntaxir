package main

import (
	"strings"
	"testing"
)

// TestFoldOfUnknownNeedsCast checks that a rule declared int whose value is a foldl or foldr over a list of
// the type that is not known, whose callback returns an element, does not compile without a cast,
// and that with the cast the parse of a list element fails where the cast runs instead of returning
// the list as an int.
func TestFoldOfUnknownNeedsCast(t *testing.T) {
	const g = `rule g = ("(" x:g ")" => list(list($x[0]))) / ("a" => list(1))
`
	for _, fold := range []string{"foldl", "foldr"} {
		plain := writeFile(t, fold+"-plain.sxir", g+"@start\nrule n int = x:g => "+fold+"(0, $x, (a, e) => e)\n")
		if out, err := runCLI(t, "", "parse", "-g", plain, "-i", "(a)"); err == nil || !strings.Contains(err.Error(), "cannot use unknown as int in result of rule n") {
			t.Errorf("%s: got %q, %v", fold, out, err)
		}
		cast := writeFile(t, fold+"-cast.sxir", g+"@start\nrule n int = x:g => "+fold+"(0, $x, (a, e) => e).(int)\n")
		if out, err := runCLI(t, "", "parse", "-g", cast, "-i", "(a)"); err == nil {
			t.Errorf("%s: the parse of a list element gives %q", fold, out)
		}
		if out, err := runCLI(t, "", "parse", "-g", cast, "-i", "a"); err != nil || !strings.Contains(out, "1") {
			t.Errorf("%s: got %q, %v", fold, out, err)
		}
	}
}
