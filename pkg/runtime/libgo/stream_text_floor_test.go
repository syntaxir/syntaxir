package libgo

import (
	"fmt"
	"testing"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestStreamRangeTextBeforeCommitFails checks the text of a node in a stream around the last commit
// point: a range that begins at or after it is the input of the range, and one that begins before it,
// which the checker keeps every grammar from taking, ends the parse with an internal error, whether
// or not the buffer still holds its input.
func TestStreamRangeTextBeforeCommitFails(t *testing.T) {
	t.Parallel()
	const text = "abcdefgh"
	for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		for _, discard := range []bool{false, true} {
			p := &Parser{unit: unit, Stream: &streamInput{baseLine: 1, baseCol: 1}}
			s := p.Stream
			if unit == sxirinput.Bytes {
				s.bs = []byte(text)
			} else {
				s.in = []rune(text)
			}
			if discard {
				p.streamDiscard(3)
			}
			s.textFloor = 4
			for _, r := range [][2]int{{4, 4}, {4, 6}, {5, 8}} {
				if got := p.rangeText(r[0], r[1]); got != text[r[0]:r[1]] {
					t.Errorf("%v, discard %v: the text of %v is %q, want %q", unit, discard, r, got, text[r[0]:r[1]])
				}
			}
			for _, r := range [][2]int{{0, 2}, {3, 4}, {3, 3}, {2, 6}} {
				got := func() (err error) {
					defer func() {
						if f, ok := recover().(fatal); ok {
							err = f.err
						}
					}()
					p.rangeText(r[0], r[1])
					return nil
				}()
				want := fmt.Sprintf("internal error: the text of [%d, %d) begins before the commit point 4", r[0], r[1])
				if got == nil || got.Error() != want {
					t.Errorf("%v, discard %v: the text of %v ends the parse with %v, want %s", unit, discard, r, got, want)
				}
			}
		}
	}
}
