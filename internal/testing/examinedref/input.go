package examinedref

import (
	"unicode/utf8"

	"syntaxir.dev/sxir/pkg/runtime/input"
)

// text is the input of an evaluation in its position unit. Matching reads one character at a time
// (spec/runtime/parsing.md, Input and Positions); what reading a character examines follows the
// slots of spec/runtime/documents.md (Reuse Model).
type text struct {
	unit  input.PositionUnit
	runes []rune // the characters, one per position, when the unit is code points
	bytes string // the input, one byte per position, when the unit is bytes
}

func newText(s string, unit input.PositionUnit) *text {
	t := &text{unit: unit, bytes: s}
	if unit == input.CodePoints {
		// The conversion reads each byte that is not part of a valid UTF-8 sequence as U+FFFD
		// occupying one position, as the specification requires.
		t.runes = []rune(s)
	}
	return t
}

// n is the length of the input in the unit.
func (t *text) n() int {
	if t.unit == input.CodePoints {
		return len(t.runes)
	}
	return len(t.bytes)
}

// char reads the character at pos. It returns the character, the position after it and the
// examined end h after the read; ok is false at the end of the input, which examines slot n.
//
// In code points a character is one slot. In bytes, decoding examines the bytes from pos up to the
// first that completes the character or makes it invalid, or slot n when the input ends inside a
// valid prefix of a character; an invalid byte is U+FFFD of one position.
func (t *text) char(pos int) (c rune, next, h int, ok bool) {
	n := t.n()
	if pos >= n {
		return 0, pos, pos + 1, false
	}
	if t.unit == input.CodePoints {
		return t.runes[pos], pos + 1, pos + 1, true
	}
	b0 := t.bytes[pos]
	if b0 < utf8.RuneSelf {
		return rune(b0), pos + 1, pos + 1, true
	}
	size, lo, hi := leading(b0)
	if size == 0 {
		return utf8.RuneError, pos + 1, pos + 1, true
	}
	for k := 1; k < size; k++ {
		i := pos + k
		if i >= n {
			// The input ends inside a valid prefix: the end slot decides that it is invalid.
			return utf8.RuneError, pos + 1, n + 1, true
		}
		c := t.bytes[i]
		if k == 1 {
			if c < lo || c > hi {
				return utf8.RuneError, pos + 1, i + 1, true
			}
		} else if c < 0x80 || c > 0xBF {
			return utf8.RuneError, pos + 1, i + 1, true
		}
	}
	r, _ := utf8.DecodeRuneInString(t.bytes[pos : pos+size])
	return r, pos + size, pos + size, true
}

// leading returns the length of the sequence that a leading byte begins and the range of its second
// byte (RFC 3629), or size 0 for a byte that begins no sequence.
func leading(b byte) (size int, lo, hi byte) {
	switch {
	case b >= 0xC2 && b <= 0xDF:
		return 2, 0x80, 0xBF
	case b == 0xE0:
		return 3, 0xA0, 0xBF
	case b >= 0xE1 && b <= 0xEC, b == 0xEE, b == 0xEF:
		return 3, 0x80, 0xBF
	case b == 0xED:
		return 3, 0x80, 0x9F
	case b == 0xF0:
		return 4, 0x90, 0xBF
	case b >= 0xF1 && b <= 0xF3:
		return 4, 0x80, 0xBF
	case b == 0xF4:
		return 4, 0x80, 0x8F
	}
	return 0, 0, 0
}

// lineFeedBefore reports whether the character before pos is a line feed. It examines slot pos-1,
// which lies below every evaluation that starts at pos. A line feed is one byte, never part of
// another character, so in bytes it is the byte before pos.
func (t *text) lineFeedBefore(pos int) bool {
	if t.unit == input.CodePoints {
		return t.runes[pos-1] == '\n'
	}
	return t.bytes[pos-1] == '\n'
}

// lineEndAt reports whether pos is the end of the input or holds a line feed or a carriage return.
// It examines slot pos only: those characters are one byte, never part of another character.
func (t *text) lineEndAt(pos int) bool {
	if pos >= t.n() {
		return true
	}
	if t.unit == input.CodePoints {
		return t.runes[pos] == '\n' || t.runes[pos] == '\r'
	}
	return t.bytes[pos] == '\n' || t.bytes[pos] == '\r'
}

// lineCol returns the 1-based line and column of pos (Lines and Columns, parsing.md): a line starts
// after each line feed, and the column counts positions of the unit from the start of the line.
func (t *text) lineCol(pos int) (line, col int) {
	line, start := 1, 0
	for i := 0; i < pos; i++ {
		if (t.unit == input.CodePoints && t.runes[i] == '\n') || (t.unit != input.CodePoints && t.bytes[i] == '\n') {
			line, start = line+1, i+1
		}
	}
	return line, pos - start + 1
}

// slice returns the matched text of [start, end): in code points the characters, with U+FFFD for
// an invalid byte, and in bytes the input bytes.
func (t *text) slice(start, end int) string {
	if t.unit == input.CodePoints {
		return string(t.runes[start:end])
	}
	return t.bytes[start:end]
}

// length is the length of a string value in the unit.
func (t *text) length(s string) int {
	if t.unit == input.CodePoints {
		return utf8.RuneCountInString(s)
	}
	return len(s)
}

// matchChar matches the character want of a literal at pos; fold matches ASCII letters in either
// case. It returns the position after the match and the examined end h. Reading a character examines
// it whether it matches or is rejected, and the end of the input is slot n. In bytes, a character
// other than U+FFFD is compared with the bytes of its UTF-8 encoding one at a time: the first byte
// that differs decides the rejection, since no character that begins with other bytes can be want,
// so the bytes after it are not examined. U+FFFD also matches one invalid byte, which only decoding
// tells, so it is matched by decoding the character.
func (t *text) matchChar(pos int, want rune, fold bool) (next, h int, ok bool) {
	if t.unit == input.CodePoints || want == utf8.RuneError {
		c, next, h, ok := t.char(pos)
		return next, h, ok && sameChar(c, want, fold)
	}
	var enc [utf8.UTFMax]byte
	size := utf8.EncodeRune(enc[:], want)
	for k := 0; k < size; k++ {
		i := pos + k
		if i >= len(t.bytes) {
			return pos, i + 1, false
		}
		b := t.bytes[i]
		if b != enc[k] && !(size == 1 && sameChar(rune(b), want, fold)) {
			return pos, i + 1, false
		}
	}
	return pos + size, pos + size, true
}
