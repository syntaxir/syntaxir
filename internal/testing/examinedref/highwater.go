package examinedref

// This file is the stream high-water check of the examined end (record 077, Examined Oracle): a stream
// parse reads its input from a reader that delivers one byte per read, so the input the parse has read
// at any moment is exactly what it needed so far. No call can have examined input the reader has not
// delivered, and when the parse returns, every byte delivered, and the end of the input if the reader
// reported it, must have been examined by the start rule or by the check of a whole-input parse that the
// input ends where the start rule stopped.
//
// Like perturb.go, the file imports the standard library only, so that the tests of generated parsers
// can copy it into the module of their consumer.

import (
	"fmt"
	"io"
	"unicode/utf8"
)

// CountingReader delivers its input one byte per Read and records how much it delivered and whether it
// reported the end of the input.
type CountingReader struct {
	data string
	// Delivered is the number of bytes delivered, and EOF reports that a Read returned io.EOF.
	Delivered int
	EOF       bool
}

// NewCountingReader returns a CountingReader of s.
func NewCountingReader(s string) *CountingReader { return &CountingReader{data: s} }

// Read delivers the next byte, or reports io.EOF once every byte has been delivered. It never returns a
// byte together with io.EOF, so that the end is reported only when a read asks for more.
func (r *CountingReader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if r.Delivered == len(r.data) {
		r.EOF = true
		return 0, io.EOF
	}
	b[0] = r.data[r.Delivered]
	r.Delivered++
	return 1, nil
}

// Read is the state of a CountingReader at a moment of a parse.
type Read struct {
	Delivered int
	EOF       bool
}

// Covers reports whether the reader state r has delivered need.
func (r Read) Covers(need Read) bool {
	return r.Delivered >= need.Delivered && (r.EOF || !need.EOF)
}

// Needed returns the input that a parse must have read to have examined the slots [0, e) of in, in
// bytes or in code points: the bytes, and whether the end of the input. In bytes, slot i < len(in) is
// byte i and slot len(in) the end. In code points, slot i is the i-th code point as the parsers decode the
// input, and deciding it reads its bytes up to the first that completes it or makes it invalid, or to the
// end of the input when the input ends inside a valid prefix of a character (spec/runtime/documents.md,
// Slots); the end slot reads the end. ok is false if e is beyond the end slot.
func Needed(in string, bytes bool, e int) (need Read, ok bool) {
	if e <= 0 {
		return Read{}, e == 0
	}
	if bytes {
		switch {
		case e <= len(in):
			return Read{Delivered: e}, true
		case e == len(in)+1:
			return Read{Delivered: len(in), EOF: true}, true
		}
		return Read{Delivered: len(in), EOF: true}, false
	}
	off, slot := 0, 0
	for ; off < len(in) && slot < e; slot++ {
		k := 1
		for off+k <= len(in) && !utf8.FullRuneInString(in[off:off+k]) {
			k++
		}
		if off+k > len(in) {
			need = Read{Delivered: len(in), EOF: true}
		} else if off+k > need.Delivered {
			need.Delivered = off + k
		}
		_, size := utf8.DecodeRuneInString(in[off:])
		off += size
	}
	if slot < e {
		// The end slot.
		need = Read{Delivered: len(in), EOF: true}
		slot++
	}
	return need, slot == e
}

// CheckHighWater checks the trace of a stream parse of in against what the reader had delivered: reads[i]
// is the state of the reader when the parser reported trace[i], and final its state when the parse
// returned. Every exit must examine only input delivered by its event; the trace must have one exit of
// the start rule (depth 1), and final must be exactly the input needed for its examined end, extended to
// one past its end when it matched, by the slot that the whole-input check examines.
func CheckHighWater(in string, bytes bool, trace []Event, reads []Read, final Read) []string {
	var out []string
	if len(reads) != len(trace) {
		return []string{fmt.Sprintf("%d reader states for %d events", len(reads), len(trace))}
	}
	var roots []Event
	for i, x := range trace {
		if x.Kind != EventExit {
			continue
		}
		if x.Depth == 1 {
			roots = append(roots, x)
		}
		need, ok := Needed(in, bytes, x.Examined)
		if !ok || !reads[i].Covers(need) {
			out = append(out, fmt.Sprintf("event %d, %v, examines input not delivered: it needs %+v, the reader delivered %+v", i, x, need, reads[i]))
		}
	}
	if len(roots) != 1 {
		return append(out, fmt.Sprintf("%d exits of the start rule, not one", len(roots)))
	}
	root := roots[0]
	h := root.Examined
	if root.Matched && root.End+1 > h {
		h = root.End + 1
	}
	need, ok := Needed(in, bytes, h)
	if !ok || need != final {
		out = append(out, fmt.Sprintf("the parse read %+v, but the start rule (%v) and the whole-input check examined to %d, which needs %+v", final, root, h, need))
	}
	return out
}
