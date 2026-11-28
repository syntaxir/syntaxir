//go:build sxir_reference_input_preparation

package libgo

import "unicode/utf8"

// prepareCodePoints is the preparation in three passes over s: the code points, the validity, and the
// offsets.
func prepareCodePoints(s string, rs []rune, offs []int32) (valid bool) {
	i := 0
	for _, r := range s {
		rs[i] = r
		i++
	}
	valid = utf8.ValidString(s)
	if offs != nil {
		i = 0
		for off := range s {
			offs[i] = int32(off)
			i++
		}
		offs[i] = int32(len(s))
	}
	return valid
}
