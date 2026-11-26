package main

import (
	"strings"
	"testing"
)

// TestFoldOfUnknownNeedsCast checks that a rule declared int whose value is a foldl over a list of
// the type that is not known, whose callback returns an element, does not compile without a cast,
// and that with the cast the parse of a list element fails where the cast runs instead of returning
// the list as an int.
func TestFoldOfUnknownNeedsCast(t *testing.T) {
	const g = `rule g = ("(" x:g ")" => list(list($x[0]))) / ("a" => list(1))
`
	plain := writeFile(t, "plain.sxir", g+"@start\nrule n int = x:g => foldl(0, $x, (a, e) => e)\n")
	if out, err := runCLI(t, "", "parse", "-g", plain, "-i", "(a)"); err == nil || !strings.Contains(err.Error(), "cannot use unknown as int in result of rule n") {
		t.Errorf("got %q, %v", out, err)
	}
	cast := writeFile(t, "cast.sxir", g+"@start\nrule n int = x:g => foldl(0, $x, (a, e) => e).(int)\n")
	if out, err := runCLI(t, "", "parse", "-g", cast, "-i", "(a)"); err == nil {
		t.Errorf("the parse of a list element gives %q", out)
	}
	if out, err := runCLI(t, "", "parse", "-g", cast, "-i", "a"); err != nil || !strings.Contains(out, "1") {
		t.Errorf("got %q, %v", out, err)
	}
}
