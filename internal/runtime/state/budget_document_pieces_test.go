//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func TestBudgetDocumentPieceConstruction(t *testing.T) {
	t.Parallel()
	if !state.DocumentPieceInput {
		t.Skip("piece input is disabled")
	}
	for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
		for _, text := range []string{strings.Repeat("é😀x", 2048), strings.Repeat("x\xff", 4096)} {
			c := state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})
			in := state.NewControlledDocumentInput(text, unit, c)
			want := state.NewDocumentPieces(text, unit)
			if in.Pieces == nil || !reflect.DeepEqual(in.Pieces, want) {
				t.Fatalf("%s: controlled preparation changed piece contents", unit)
			}
			// Preparing the input is no event: neither retaining a byte string nor decoding code
			// points counts a unit.
			if c.Used != 0 {
				t.Fatalf("%s: preparing the pieces counted %d units", unit, c.Used)
			}
		}
	}
	c := state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
	if err := statetest.CatchFatal(t, func() { state.NewControlledDocumentInput(strings.Repeat("a", 8192), input.CodePoints, c) }); err != nil || c.Used != 0 {
		t.Fatalf("piece preparation under a limit of 1: %v, counted %d units", err, c.Used)
	}
}

// TestBudgetDocumentPieceTextPublication pins "Input caches are published complete" of
// "Checks and Interruptions" in spec/runtime/parsing.md.
func TestBudgetDocumentPieceTextPublication(t *testing.T) {
	t.Parallel()
	if !state.DocumentPieceInput {
		t.Skip("piece input is disabled")
	}
	for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
		pieces := state.NewDocumentPieces(strings.Repeat("x", 8192), unit)
		pieces.Replace(4096, 4097, "é")
		// Materializing the text is Document work: it counts against no limit, but its steps keep
		// the polls, so a cancellation interrupts it before it publishes anything.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c := state.NewBudgetControl(ctx, runtime.Limits{Compute: 1})
		in := state.Input{Unit: unit, Eof: true, Pieces: pieces, Control: c}
		if err := statetest.CatchFatal(t, func() { in.Text(0, pieces.N) }); !errors.Is(err, context.Canceled) || c.Used != c.Doc {
			t.Fatalf("%s: piece materialization was not interrupted by the cancellation alone: %v, counted %d units", unit, err, c.Used-c.Doc)
		}
		if pieces.FullOK || pieces.Full != "" {
			t.Fatal("interrupted materialization published a cache")
		}
		in.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})
		got := in.Text(0, pieces.N)
		if want := strings.Repeat("x", 4096) + "é" + strings.Repeat("x", 4095); got != want || !pieces.FullOK {
			t.Fatalf("%s: retry materialization differs", unit)
		}
	}
}
