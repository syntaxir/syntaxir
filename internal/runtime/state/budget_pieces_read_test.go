//go:build !sxir_budget_controls_reference

package state

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// Decoding at a position of a Document's pieces counts against the default limit what decoding there
// in a flat input counts: one unit for a byte that is a character by itself, and one more for each
// byte available to a character that is not. The other bytes the pieces read for a decoding are work
// of the piece representation, and only they are Document units.
func TestPieceDecodeCountsWhatAFlatDecodeCounts(t *testing.T) {
	if !DocumentPieceInput {
		t.Skip("piece input is off in this build")
	}
	text := strings.Repeat("abé€\U0001F600\n", 800)
	control := func() *BudgetControl {
		return &BudgetControl{Limits: runtime.Limits{Compute: 1 << 40}, DefaultCompute: true}
	}
	flat := NewTextInput(text, input.Bytes, false)
	flat.Control = control()
	pieces := Input{Unit: input.Bytes, Eof: true, Control: control(), Pieces: NewDocumentPieces(text, input.Bytes)}
	for i := 0; i < len(text); i++ {
		fu, fd := flat.Control.Used, flat.Control.Doc
		fr, fs, fe, fok := flat.Decode(i)
		pu, pd := pieces.Control.Used, pieces.Control.Doc
		pr, ps, pe, pok := pieces.Decode(i)
		if fr != pr || fs != ps || fe != pe || fok != pok {
			t.Fatalf("at %d: pieces decode %q %d %d %v, flat %q %d %d %v", i, pr, ps, pe, pok, fr, fs, fe, fok)
		}
		flatCounted := (flat.Control.Used - fu) - (flat.Control.Doc - fd)
		pieceCounted := (pieces.Control.Used - pu) - (pieces.Control.Doc - pd)
		read := uint64(min(4, len(text)-i))
		want, doc := uint64(1), read-1
		if text[i] >= 0x80 {
			want, doc = 1+read, 0
		}
		if pieceCounted != flatCounted || flatCounted != want {
			t.Fatalf("at %d: the pieces count %d units, the flat input %d, want %d", i, pieceCounted, flatCounted, want)
		}
		if pieces.Control.Doc-pd != doc {
			t.Fatalf("at %d: %d Document units for %d bytes read, want %d", i, pieces.Control.Doc-pd, read, doc)
		}
	}
}
