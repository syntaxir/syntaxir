package state

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"syntaxir.dev/sxir/pkg/runtime/input"
)

// detachedText materializes input that cannot supply an original-source substring. Its local
// builder owns the output; input/scratch reuse cannot mutate a returned string. Making the text
// counts no compute unit (the text of a node in a stream counts its event where the action expression asks for
// it, RangeText); the memory of the text is charged before it is made.
func (in *Input) detachedText(start, end int) string {
	var out strings.Builder
	if in.Unit == input.Bytes {
		src := in.Bs[start:end]
		in.Control.chargeValueBytes(len(src))
		out.Grow(len(src))
		out.Write(src)
		return out.String()
	}
	src := in.In[start:end]
	size := 0
	for _, r := range src {
		width := utf8.RuneLen(r)
		if width < 0 {
			width = utf8.RuneLen(utf8.RuneError)
		}
		size = CheckedTextSize(size, width)
	}
	in.Control.chargeValueBytes(size)
	out.Grow(size)
	for _, r := range src {
		out.WriteRune(r)
	}
	return out.String()
}

// CheckedTextSize prevents sizing overflow before allocating output storage.
func CheckedTextSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		panic(Fatal{Err: fmt.Errorf("input text exceeds maximum string size")})
	}
	return size + width
}
