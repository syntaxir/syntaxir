package libgo

import (
	"slices"
	"strings"
	"unicode/utf8"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// The text of a Document in CodePoints. A parse reads the code points (documentState.in) directly, as
// it reads the decoded input of a parse from scratch, and takes token text from the chunks of the text
// (docChunk). An edit replaces the code points in place, moving those after it when its length
// changes, and rebuilds only the chunks it touches: neither an edit nor a parse decodes or copies the
// whole text, as decoding the text for each parse and splicing it as one string would.

// docChunk is a piece of the text of a Document in CodePoints: the code points [start, start+n) of
// the text, as the string s. offs holds the byte offset in s of every docOffsStride-th of its code
// points, from the first; it is nil when s is ASCII, where offsets equal indexes.
type docChunk struct {
	s     string
	start int
	n     int
	offs  []int32
}

// docChunkTarget is the number of code points of a chunk, between docChunkTarget and twice that
// unless the text is shorter (cutChunks). An edit that leaves a chunk shorter than docChunkMin
// joins it to a neighbor.
// docOffsStride is the distance between the code points of a chunk whose byte offsets it keeps:
// the offsets cost 4/docOffsStride bytes per code point, and finding the offset of a code point
// decodes fewer than docOffsStride code points.
const (
	docChunkTarget = 4096
	docChunkMin    = docChunkTarget / 4
	docOffsStride  = 32
)

// byteAt returns the byte offset in c.s of its code point i (0 <= i <= c.n).
func (c *docChunk) byteAt(i int) int {
	if c.offs == nil {
		return i
	}
	if i == c.n {
		return len(c.s)
	}
	b := int(c.offs[i/docOffsStride])
	for range i % docOffsStride {
		_, size := utf8.DecodeRuneInString(c.s[b:])
		b += size
	}
	return b
}

// setText sets the text of d, in CodePoints, to text, valid UTF-8, and its code points to in.
func (d *documentState) setText(text string, in []rune) {
	d.in = in
	d.chunks = cutChunks(make([]docChunk, 0, len(in)/docChunkTarget+1), text, 0, len(in))
	d.lastChunk, d.offsLen = 0, offsLen(d.chunks)
	d.text, d.textOK = text, true
}

// cutChunks appends to cs the chunks of s, valid UTF-8 of n code points that start at the code point
// start of the text: n/docChunkTarget chunks of about equal length, or one if n is shorter, and none
// for an empty s. The chunks are substrings of s. editText relies on that count.
func cutChunks(cs []docChunk, s string, start, n int) []docChunk {
	if n == 0 {
		return cs
	}
	pieces := max(1, n/docChunkTarget)
	size, extra := n/pieces, n%pieces
	off := 0
	for i := range pieces {
		m := size
		if i < extra {
			m++
		}
		c := docChunk{start: start, n: m}
		if len(s)-off == n {
			// The rest of s is ASCII.
			c.s = s[off : off+m]
		} else {
			end := off
			for range m {
				_, size := utf8.DecodeRuneInString(s[end:])
				end += size
			}
			c.s = s[off:end]
			if len(c.s) != m {
				c.offs = make([]int32, 0, (m+docOffsStride-1)/docOffsStride)
				b := 0
				for j := range m {
					if j%docOffsStride == 0 {
						c.offs = append(c.offs, int32(b))
					}
					_, size := utf8.DecodeRuneInString(c.s[b:])
					b += size
				}
			}
		}
		cs = append(cs, c)
		off += len(c.s)
		start += m
		n -= m
	}
	return cs
}

// chunkAt returns the index of the chunk of the code point pos, or of the last chunk if pos is the
// length of the text. The text has at least one chunk.
func (d *documentState) chunkAt(pos int) int {
	cs := d.chunks
	if i := d.lastChunk; i < len(cs) && cs[i].start <= pos && (pos < cs[i].start+cs[i].n || i == len(cs)-1) {
		return i
	}
	i, _ := slices.BinarySearchFunc(cs, pos, func(c docChunk, pos int) int {
		switch {
		case c.start+c.n <= pos:
			return -1
		case c.start > pos:
			return 1
		}
		return 0
	})
	i = min(i, len(cs)-1)
	d.lastChunk = i
	return i
}

// textOf returns the text of the code points [start, end): a substring of a chunk, or a copy when the
// range spans chunks, which it reports.
func (d *documentState) textOf(start, end int) (string, bool) {
	if start == end {
		return "", false
	}
	i := d.chunkAt(start)
	c := &d.chunks[i]
	if end <= c.start+c.n {
		return c.s[c.byteAt(start-c.start):c.byteAt(end-c.start)], false
	}
	var b strings.Builder
	for ; start < end; i++ {
		c := &d.chunks[i]
		to := min(end, c.start+c.n)
		b.WriteString(c.s[c.byteAt(start-c.start):c.byteAt(to-c.start)])
		start = to
	}
	return b.String(), true
}

// units returns the length of the text in the position unit, without reading it.
func (d *documentState) units() int {
	if d.unit == sxirinput.Bytes {
		return len(d.text)
	}
	return len(d.in)
}

// currentText returns the text of d.
func (d *documentState) currentText() string {
	if d.unit == sxirinput.Bytes || d.textOK {
		return d.text
	}
	var b strings.Builder
	n := 0
	for i := range d.chunks {
		n += len(d.chunks[i].s)
	}
	b.Grow(n)
	for i := range d.chunks {
		b.WriteString(d.chunks[i].s)
	}
	d.text, d.textOK = b.String(), true
	return d.text
}

// editText replaces the code points [start, end) of the text with replacement, valid UTF-8 of k code
// points: in the code points, in place, and in the chunks that hold them, which it rebuilds from their
// text before start, the replacement and their text from end, joining a neighbor to them if that is
// shorter than docChunkMin. The chunks after them shift by the delta.
func (d *documentState) editText(start, end int, replacement string) {
	k := utf8.RuneCountInString(replacement)
	n := len(d.in)
	m := n + k - (end - start)
	d.in = spliceRunes(d.in, start, end, k, m)
	i := start
	for _, r := range replacement {
		d.in[i] = r
		i++
	}
	d.text, d.textOK = "", false
	cs := d.chunks
	if len(cs) == 0 {
		d.chunks = cutChunks(cs, replacement, 0, k)
		d.lastChunk, d.offsLen = 0, offsLen(d.chunks)
		return
	}
	lo := d.chunkAt(start)
	hi := lo
	if end > start {
		hi = d.chunkAt(end - 1)
	}
	first, last := &cs[lo], &cs[hi]
	s := first.s[:first.byteAt(start-first.start)] + replacement + last.s[last.byteAt(end-last.start):]
	at := first.start
	count := m - n + last.start + last.n - at
	hi++
	if count < docChunkMin && hi-lo < len(cs) {
		if hi < len(cs) {
			s, count = s+cs[hi].s, count+cs[hi].n
			hi++
		} else {
			lo--
			s, count, at = cs[lo].s+s, count+cs[lo].n, cs[lo].start
		}
	}
	for j := hi; j < len(cs); j++ {
		cs[j].start += m - n
	}
	// The new chunks take the places of cs[lo:hi], which first make room for them in place, so that
	// an edit allocates no list of chunks.
	np := 0
	if count > 0 {
		np = max(1, count/docChunkTarget)
	}
	d.offsLen -= offsLen(cs[lo:hi])
	if delta := np - (hi - lo); delta > 0 {
		cs = slices.Grow(cs, delta)[:len(cs)+delta]
		copy(cs[hi+delta:], cs[hi:len(cs)-delta])
	} else if delta < 0 {
		copy(cs[hi+delta:], cs[hi:])
		clear(cs[len(cs)+delta:])
		cs = cs[:len(cs)+delta]
	}
	d.offsLen += offsLen(cutChunks(cs[lo:lo], s, at, count))
	d.chunks = cs
	d.lastChunk = lo
}

// offsLen returns the number of the offsets of the chunks cs.
func offsLen(cs []docChunk) int {
	n := 0
	for i := range cs {
		n += len(cs[i].offs)
	}
	return n
}

// spliceRunes makes room in s, of n elements, for k elements in place of [start, end): the
// elements from end on move to start+k, and the result has m = n+k-(end-start) elements. It grows s,
// with room for later insertions, when its capacity is too small.
func spliceRunes(s []rune, start, end, k, m int) []rune {
	n := len(s)
	if m > cap(s) {
		grown := make([]rune, m, m+m/16)
		copy(grown, s[:start])
		copy(grown[start+k:], s[end:])
		return grown
	}
	s = s[:max(n, m)]
	copy(s[start+k:], s[end:n])
	return s[:m]
}
