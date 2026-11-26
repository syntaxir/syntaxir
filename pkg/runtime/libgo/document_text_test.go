package libgo

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestDocumentEditSplicesText applies random edits with multi-byte characters and invalid UTF-8 to
// CodePoints Documents, short and longer than a few chunks, and checks after each the text that the
// Document keeps (document_text.go) against the edited text: its code points, the chunks that hold
// it, the token text of random ranges and the whole text.
func TestDocumentEditSplicesText(t *testing.T) {
	pieces := []string{"", "a", "é", "世", "😀", "\xff", "ab\xe2\x82", "\x82c", "xyz", "é世😀a", strings.Repeat("q", 3*docChunkTarget)}
	for _, initial := range []string{"", "aé世", strings.Repeat("aé世", docChunkTarget), strings.Repeat("ab", 3*docChunkTarget)} {
		r := rand.New(rand.NewPCG(3, uint64(len(initial))))
		d := testDocumentState(&Rule{Name: "main"}, initial, sxirinput.CodePoints)
		want := []rune(initial)
		checkDocumentText(t, d, want)
		for step := range 300 {
			// Edits that grow the text alternate with edits that shrink it.
			start := r.IntN(len(want) + 1)
			end, text := start+r.IntN(min(2, len(want)-start)+1), pieces[r.IntN(len(pieces))]
			if (step/30)%2 == 1 {
				end, text = start+r.IntN(min(3*docChunkMin, len(want)-start)+1), ""
			}
			if err := d.edit(start, end, text); err != nil {
				t.Fatalf("edit %d: %v", step, err)
			}
			// Each invalid byte converts to one replacement character.
			want = slices.Concat(want[:start], []rune(text), want[end:])
			checkDocumentText(t, d, want)
			p := &Parser{Document: d}
			p.useDocumentInput(d)
			if p.n != len(want) {
				t.Fatalf("edit %d: a parse reads %d code points, want %d", step, p.n, len(want))
			}
			for range 8 {
				a := r.IntN(len(want) + 1)
				b := a + r.IntN(min(2*docChunkTarget, len(want)-a)+1)
				if got := p.Text(a, b); got != string(want[a:b]) {
					t.Fatalf("edit %d: text [%d,%d) = %q, want %q", step, a, b, got, string(want[a:b]))
				}
			}
			if d.currentText() != string(want) {
				t.Fatalf("edit %d: the text differs from the edited text", step)
			}
		}
	}
}

// checkDocumentText checks the code points and the chunks of d against the text want: the chunks hold
// the text in order, each with its position, length and offsets, and none is empty or longer than
// twice the target length and the length that an edit joins to a neighbor.
func checkDocumentText(t *testing.T, d *documentState, want []rune) {
	t.Helper()
	if !slices.Equal(d.in, want) {
		t.Fatalf("the code points differ from the edited text")
	}
	at, offs := 0, 0
	var text strings.Builder
	for i, c := range d.chunks {
		if c.start != at || c.n != utf8.RuneCountInString(c.s) || c.n == 0 || c.n > 2*docChunkTarget+docChunkMin {
			t.Fatalf("chunk %d of %d: start %d (want %d), %d code points, %d bytes", i, len(d.chunks), c.start, at, c.n, len(c.s))
		}
		if (c.offs == nil) != (len(c.s) == c.n) {
			t.Fatalf("chunk %d: offsets %v for %d bytes of %d code points", i, c.offs != nil, len(c.s), c.n)
		}
		rs := []rune(c.s)
		for j := 0; j <= c.n; j++ {
			if got, want := c.byteAt(j), len(string(rs[:j])); got != want {
				t.Fatalf("chunk %d: code point %d at byte %d, want %d", i, j, got, want)
			}
		}
		at += c.n
		offs += len(c.offs)
		text.WriteString(c.s)
	}
	if at != len(want) || text.String() != string(want) {
		t.Fatalf("the chunks hold %d code points, want %d", at, len(want))
	}
	if offs != d.offsLen {
		t.Fatalf("offsLen %d, want %d", d.offsLen, offs)
	}
}

// TestDocumentEditTextEdges checks the edits that the random edits of TestDocumentEditSplicesText
// reach only by chance: a span across a chunk boundary, edits at the end of the text, an empty edit,
// the offsets of a chunk rebuilt when it gains or loses its only non-ASCII code point, and the join of
// a chunk shorter than docChunkMin to its next or, for the last chunk, its previous neighbor.
func TestDocumentEditTextEdges(t *testing.T) {
	ascii := strings.Repeat("ab", 3*docChunkTarget/2)
	type edit struct {
		start, end int
		text       string
	}
	for _, tc := range []struct {
		name    string
		initial string
		// edits are applied in order; chunks is the number of chunks after the last one, or -1.
		edits  []edit
		chunks int
	}{
		{"a span across a chunk boundary", ascii, []edit{{docChunkTarget - 2, docChunkTarget + 2, "é世"}}, 2},
		{"a span of whole chunks", ascii, []edit{{1, 3*docChunkTarget - 1, "x"}}, 1},
		{"an insertion at the end", ascii, []edit{{3 * docChunkTarget, 3 * docChunkTarget, "世😀"}}, 3},
		{"a deletion at the end", ascii, []edit{{3*docChunkTarget - 5, 3 * docChunkTarget, ""}}, 3},
		{"an insertion into an empty text", "", []edit{{0, 0, "é"}}, 1},
		{"a deletion of the whole text", ascii, []edit{{0, 3 * docChunkTarget, ""}}, 0},
		{"an empty edit", ascii, []edit{{docChunkTarget, docChunkTarget, ""}}, 3},
		{"an empty edit of an empty text", "", []edit{{0, 0, ""}}, 0},
		{"a chunk gains offsets", ascii, []edit{{10, 11, "é"}}, 3},
		{"a chunk loses its offsets", ascii, []edit{{10, 11, "é"}, {10, 11, "a"}}, 3},
		{"a non-ASCII chunk rebuilt", strings.Repeat("aé世", docChunkTarget), []edit{{5, 6, "q"}, {docChunkTarget, docChunkTarget + 1, "😀😀"}}, 3},
		{"a short chunk joins its next", ascii, []edit{{docChunkTarget + 1, 2*docChunkTarget - 1, ""}}, 2},
		{"a short last chunk joins its previous", ascii, []edit{{2*docChunkTarget + 1, 3 * docChunkTarget, ""}}, 2},
		{"a short single chunk stays", "abc", []edit{{1, 2, ""}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDocumentState(&Rule{Name: "main"}, tc.initial, sxirinput.CodePoints)
			want := []rune(tc.initial)
			for i, e := range tc.edits {
				before := len(d.chunks)
				if err := d.edit(e.start, e.end, e.text); err != nil {
					t.Fatalf("edit %d: %v", i, err)
				}
				want = slices.Concat(want[:e.start], []rune(e.text), want[e.end:])
				checkDocumentText(t, d, want)
				if e.start == e.end && e.text == "" && len(d.chunks) != before {
					t.Fatalf("edit %d: an empty edit changed the chunks from %d to %d", i, before, len(d.chunks))
				}
			}
			if tc.chunks >= 0 && len(d.chunks) != tc.chunks {
				t.Fatalf("%d chunks, want %d", len(d.chunks), tc.chunks)
			}
			if d.currentText() != string(want) {
				t.Fatalf("the text differs from the edited text")
			}
		})
	}
}
