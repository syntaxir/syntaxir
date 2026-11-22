package examinedref_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/examinedref"
)

// TestCountingReader checks that the reader delivers one byte per read, whatever the buffer, and reports the
// end only on a read after the last byte.
func TestCountingReader(t *testing.T) {
	t.Parallel()
	r := examinedref.NewCountingReader("ab")
	buf := make([]byte, 8)
	var got []string
	for {
		n, err := r.Read(buf)
		got = append(got, fmt.Sprintf("%d %q %v %d %v", n, buf[:n], err, r.Delivered, r.EOF))
		if err != nil {
			break
		}
	}
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Errorf("an empty read returned %d, %v", n, err)
	}
	want := []string{`1 "a" <nil> 1 false`, `1 "b" <nil> 2 false`, `0 "" EOF 2 true`}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("reads %q, want %q", got, want)
	}
}

// TestNeeded checks the input needed for an examined end in both units, with the decoding rule of
// spec/runtime/documents.md (Slots) for characters that the input completes, breaks or cuts.
func TestNeeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in    string
		bytes bool
		e     int
		need  examinedref.Read
		ok    bool
	}{
		{"ab", true, 0, examinedref.Read{}, true},
		{"ab", true, 2, examinedref.Read{Delivered: 2}, true},
		{"ab", true, 3, examinedref.Read{Delivered: 2, EOF: true}, true},
		{"ab", true, 4, examinedref.Read{Delivered: 2, EOF: true}, false},
		{"é", false, 1, examinedref.Read{Delivered: 2}, true},
		{"é", false, 2, examinedref.Read{Delivered: 2, EOF: true}, true},
		{"é", false, 3, examinedref.Read{Delivered: 2, EOF: true}, false},
		// The a after \xe2\x82 makes the first character invalid: deciding slot 0 reads it.
		{"\xe2\x82a", false, 1, examinedref.Read{Delivered: 3}, true},
		{"\xe2\x82a", false, 3, examinedref.Read{Delivered: 3}, true},
		// The input ends inside a valid prefix: deciding slot 0 reads the end.
		{"\xe2\x82", false, 1, examinedref.Read{Delivered: 2, EOF: true}, true},
		{"\xe2\x82", false, 3, examinedref.Read{Delivered: 2, EOF: true}, true},
		{"\xffa", false, 1, examinedref.Read{Delivered: 1}, true},
		{"a😀b", false, 2, examinedref.Read{Delivered: 5}, true},
	}
	for _, tc := range tests {
		need, ok := examinedref.Needed(tc.in, tc.bytes, tc.e)
		if need != tc.need || ok != tc.ok {
			t.Errorf("Needed(%q, bytes=%v, %d) = %+v, %v; want %+v, %v", tc.in, tc.bytes, tc.e, need, ok, tc.need, tc.ok)
		}
	}
}

// TestCheckHighWater checks the high-water comparison on hand-made stream traces of "ab;" in bytes.
func TestCheckHighWater(t *testing.T) {
	t.Parallel()
	read := func(n int, eof bool) examinedref.Read { return examinedref.Read{Delivered: n, EOF: eof} }
	// main = item* #stream $$ reads ab;, matches item [0, 3) and reaches the end.
	trace := []examinedref.Event{lockEnter("main", 0, 1), lockEnter("item", 0, 2), lockExit("item", 0, 2, true, 3, 3), lockExit("main", 0, 1, true, 3, 4)}
	reads := []examinedref.Read{read(0, false), read(0, false), read(3, false), read(3, true)}
	tests := []struct {
		name       string
		trace      []examinedref.Event
		reads      []examinedref.Read
		final      examinedref.Read
		violations []string
	}{
		{"exact", trace, reads, read(3, true), nil},
		{"a read of the end that no call examined",
			[]examinedref.Event{lockEnter("main", 0, 1), lockExit("main", 0, 1, false, 0, 3)}, []examinedref.Read{read(0, false), read(3, false)}, read(3, true),
			[]string{"the parse read {Delivered:3 EOF:true}"}},
		{"an exit that examines input not yet delivered", trace, []examinedref.Read{read(0, false), read(0, false), read(2, false), read(3, true)}, read(3, true),
			[]string{"exit item@0"}},
		{"a start rule that examined less than the parse read",
			[]examinedref.Event{lockEnter("main", 0, 1), lockExit("main", 0, 1, false, 0, 2)}, []examinedref.Read{read(0, false), read(3, false)}, read(3, false),
			[]string{"the parse read {Delivered:3 EOF:false}"}},
		{"a start rule that examined more than the parse read",
			[]examinedref.Event{lockEnter("main", 0, 1), lockExit("main", 0, 1, false, 0, 4)}, []examinedref.Read{read(0, false), read(3, false)}, read(3, false),
			[]string{"exit main@0", "the parse read {Delivered:3 EOF:false}"}},
		{"the whole-input check after a match that examined only its text",
			[]examinedref.Event{lockEnter("main", 0, 1), lockExit("main", 0, 1, true, 2, 2)}, []examinedref.Read{read(0, false), read(2, false)}, read(3, false),
			nil},
		{"no read for the whole-input check",
			[]examinedref.Event{lockEnter("main", 0, 1), lockExit("main", 0, 1, true, 2, 2)}, []examinedref.Read{read(0, false), read(2, false)}, read(2, false),
			[]string{"examined to 3"}},
		{"two start rules", append(append([]examinedref.Event(nil), trace...), lockEnter("main", 4, 1), lockExit("main", 4, 1, false, 4, 4)),
			append(append([]examinedref.Read(nil), reads...), read(3, true), read(3, true)), read(3, true),
			[]string{"2 exits of the start rule"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := examinedref.CheckHighWater("ab;", true, tc.trace, tc.reads, tc.final)
			if len(got) != len(tc.violations) {
				t.Fatalf("violations %q, want %d", got, len(tc.violations))
			}
			for i, v := range tc.violations {
				if !strings.Contains(got[i], v) {
					t.Errorf("violation %q does not contain %q", got[i], v)
				}
			}
		})
	}
}
