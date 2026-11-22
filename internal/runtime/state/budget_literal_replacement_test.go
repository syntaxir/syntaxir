//go:build !sxir_budget_controls_reference

package state_test

import (
	"bufio"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func TestBudgetLiteralReplacementMismatchBudget(t *testing.T) {
	t.Parallel()
	for _, streaming := range []bool{false, true} {
		for _, suffix := range []string{"", string(utf8.RuneError)} {
			literal := strings.Repeat("a", 4096) + suffix
			c := state.NewBudgetControl(nil, runtime.Limits{Compute: 32})
			p := &state.Parser{Input: state.Input{Unit: input.Bytes, Bs: []byte("b"), Eof: !streaming, Control: c}}
			if streaming {
				p.Reader = bufio.NewReader(strings.NewReader(""))
			}
			// A literal that does not match advances over nothing and counts nothing
			// (spec/runtime/parsing.md#compute-units): the scan of the literal for the replacement
			// character and the inspection of the first byte are no events.
			var matched bool
			err := statetest.CatchFatal(t, func() { matched = p.MatchLiteral([]rune(literal), []byte(literal)) })
			if err != nil || matched {
				t.Fatalf("streaming=%v replacement=%v error=%v matched=%v, want a mismatch", streaming, suffix != "", err, matched)
			}
			if c.Used != 0 || p.Pos != 0 {
				t.Fatalf("streaming=%v spent=%d pos=%d, want nothing counted and the initial byte mismatch", streaming, c.Used, p.Pos)
			}
		}
	}
}

func TestBudgetLiteralReplacementPublicLookahead(t *testing.T) {
	t.Parallel()
	literal := strings.Repeat("a", 4096) + string(utf8.RuneError)
	prog := enginetest.MustCompile(t, "rule main = !"+strconv.Quote(literal)+" \"b\" $$")
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			opts := engine.ParseOptions{Backend: backend, Unit: input.Bytes, Recognize: recognize}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("baseline %v/%v: %v", backend, recognize, err)
			}
			// The literal of the lookahead fails and counts nothing, so the parse fits a limit far
			// below the literal's length.
			opts.Limits = runtime.Limits{Compute: 256}
			n, err := prog.ParseWith("main", "b", opts)
			if (n == nil) != recognize || err != nil {
				t.Fatalf("%v/recognize=%v node=%v error=%v, want the parse to fit the limit", backend, recognize, n, err)
			}
			opts.Limits = runtime.Limits{Compute: 1 << 20}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("retry %v/%v: %v", backend, recognize, err)
			}
		}
	}
}

func TestBudgetLiteralReplacementSentinelBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		literal string
		visits  uint64
		found   bool
	}{
		{"first", string(utf8.RuneError) + strings.Repeat("a", 4096), 1, true},
		{"last", "aaaa" + string(utf8.RuneError), 5, true},
		{"absent", "aaaaa", 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := state.NewBudgetControl(nil, runtime.Limits{Compute: tc.visits})
			p := &state.Parser{Input: state.Input{Control: c}}
			// The scan is no event: it counts nothing, whatever the limit.
			got := p.LiteralHasReplacement([]rune(tc.literal))
			if got != tc.found || c.Used != 0 {
				t.Fatalf("found=%v spent=%d, want %v/0 (%d visits)", got, c.Used, tc.found, tc.visits)
			}
			if tc.visits > 1 {
				c = state.NewBudgetControl(nil, runtime.Limits{Compute: tc.visits - 1})
				p.Control = c
				err := statetest.CatchFatal(t, func() { p.LiteralHasReplacement([]rune(tc.literal)) })
				if err != nil || c.Used != 0 {
					t.Fatalf("one-short error=%v spent=%d", err, c.Used)
				}
			}
			p.Control = nil
			if got := p.LiteralHasReplacement([]rune(tc.literal)); got != tc.found {
				t.Fatalf("inactive found=%v", got)
			}
		})
	}
}

func TestBudgetLiteralReplacementSentinelCancellation(t *testing.T) {
	t.Parallel()
	ctx := statetest.NewPollingContext(1)
	c := state.NewBudgetControl(ctx, runtime.Limits{Compute: 1 << 20})
	p := &state.Parser{Input: state.Input{Control: c}}
	err := statetest.CatchFatal(t, func() { p.LiteralHasReplacement([]rune(strings.Repeat("a", 4096) + string(utf8.RuneError))) })
	// The scan counts nothing, so it reaches no poll and does not observe the context.
	if err != nil || c.Used != 0 {
		t.Fatalf("error=%v spent=%d, want neither", err, c.Used)
	}
}
