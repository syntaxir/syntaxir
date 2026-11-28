//go:build !sxir_reference_input_preparation

package libgo

import "unicode/utf8"

// prepareCodePoints decodes s into rs, which holds its code points, writes the byte offset of each
// into offs unless offs is nil, with offs[len(rs)] = len(s), and reports whether s is valid UTF-8 (a
// U+FFFD that decodes from three bytes is valid). It reads s once. The reference build
// (sxir_reference_input_preparation) decodes, checks validity and builds the offsets in three passes.
func prepareCodePoints(s string, rs []rune, offs []int32) (valid bool) {
	valid = true
	i := 0
	for off, r := range s {
		if r == utf8.RuneError && valid {
			if _, size := utf8.DecodeRuneInString(s[off:]); size == 1 {
				valid = false
			}
		}
		rs[i] = r
		if offs != nil {
			offs[i] = int32(off)
		}
		i++
	}
	if offs != nil {
		offs[i] = int32(len(s))
	}
	return valid
}
