package conform

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The length table: len of a string, in each position unit, with the compute units that the call counts
// (spec/runtime/parsing.md#compute-units): its length in bytes, in either position unit, so that the count
// does not depend on how a target holds strings. Every target runs the table through its printed
// composition length under logical compute units, so that their counts agree.

// LengthStrings are the strings of the table: empty, ASCII, and with code points of two, three and four
// bytes, where the length in code points and in bytes differ.
var LengthStrings = []string{"", "ab", "é", "aé€😀", strings.Repeat("ü", 40)}

// LengthWant returns what len(s) returns, and the units it counts, in a parse in code points if
// codePoints is set and in bytes otherwise.
func LengthWant(s string, codePoints bool) (n int, units uint64) {
	if codePoints {
		return utf8.RuneCountInString(s), uint64(len(s))
	}
	return len(s), uint64(len(s))
}

// RunLengths runs every case of the length table on call, which returns what len(s) returns in a target
// and the units it counts there, and returns the cases whose result differs.
func RunLengths(target string, call func(s string, codePoints bool) (n int, units uint64)) error {
	var bad []string
	for _, codePoints := range []bool{false, true} {
		for _, s := range LengthStrings {
			n, u := call(s, codePoints)
			wn, wu := LengthWant(s, codePoints)
			if n != wn || u != wu {
				bad = append(bad, fmt.Sprintf("%s: len(%q) in code points=%v = %d counting %d units, want %d counting %d",
					target, s, codePoints, n, u, wn, wu))
			}
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("length table:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}
