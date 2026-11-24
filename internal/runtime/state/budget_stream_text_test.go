//go:build !sxir_budget_controls_reference

package state_test

import (
	"bufio"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func budgetStreamTextInput(u input.PositionUnit, text string) state.Input {
	in := state.Input{Unit: u, BaseLine: 1, BaseCol: 1, Reader: bufio.NewReader(strings.NewReader(""))}
	if u == input.Bytes {
		in.Bs = []byte(text)
	} else {
		in.In = []rune(text)
	}
	return in
}

// Materializing the text of a stream counts no compute unit: the event is the text value of a node
// (RangeText), which counts the position units of its result, not the snapshot that serves it. The
// tests below hold a limit of one unit, which a counted materialization would exceed.
func TestBudgetStreamTextByteSnapshotBoundaries(t *testing.T) {
	t.Parallel()
	if !state.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, size := range []int{63, 64, 1024, 1025, 4097} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			text := strings.Repeat("x", size)
			in := budgetStreamTextInput(input.Bytes, text)
			end := min(size, 1024)
			used := size
			if size >= 64 {
				used = min(size, state.StreamTextChunk)
			}
			in.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
			var val string
			err := statetest.CatchFatal(t, func() { val = in.Text(0, end) })
			if err != nil || val != text[:end] || in.Control.Used != 0 {
				t.Fatalf("materialization error=%v value=%q counted=%d, want the text and no unit", err, val, in.Control.Used)
			}
			if size < 64 && in.StreamText.Source != "" || size >= 64 && in.StreamText.End != used {
				t.Fatal("snapshot/fallback threshold changed")
			}
			if size >= 64 {
				used := in.Control.Used
				if in.Text(1, end) != text[1:end] || in.Control.Used != used {
					t.Fatal("cache hit performed new materialization")
				}
			}
		})
	}
}

func TestBudgetStreamTextFallbackAndNoReadAhead(t *testing.T) {
	t.Parallel()
	for _, u := range []input.PositionUnit{input.Bytes, input.CodePoints} {
		for _, size := range []int{1, 63, 1025, 4097} {
			t.Run(fmt.Sprintf("%s/%d", u, size), func(t *testing.T) {
				r := &statetest.OneByteReader{Source: strings.Repeat("x", size+100)}
				in := state.Input{Unit: u, Reader: bufio.NewReader(r)}
				in.Fill(size)
				reads := r.Calls
				in.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 20})
				if in.Text(0, size) != strings.Repeat("x", size) || r.Calls != reads {
					t.Fatal("controlled text read ahead or changed text")
				}
				if size < 64 || size > state.StreamTextChunk {
					if in.StreamText.Source != "" {
						t.Fatal("fallback published a snapshot")
					}
				}
				if in.Control.Used != 0 {
					t.Fatalf("stream text materialization counted %d units, want none", in.Control.Used)
				}
			})
		}
	}
}

func TestBudgetStreamTextCodePointPublication(t *testing.T) {
	t.Parallel()
	if !state.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	const size = 1024
	text := strings.Repeat("a", size)
	old := state.StreamTextSnapshot{Base: -10, End: -7, Source: "old", Offsets: []int32{0, 1, 2, 3}}
	// Each phase of building a snapshot (sizing, encoding and the offsets up to the last one) counts
	// no unit, so none of them stops under a limit of one unit, and a new snapshot keeps its offsets in
	// storage of its own rather than in the old cache.
	for _, phase := range []string{"sizing", "encoding", "offsets", "last-offset"} {
		t.Run(phase, func(t *testing.T) {
			in := budgetStreamTextInput(input.CodePoints, text)
			in.StreamText = old
			in.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
			before := append([]int32(nil), old.Offsets...)
			var got string
			err := statetest.CatchFatal(t, func() { got = in.Text(0, 8) })
			if err != nil || got != text[:8] || in.Control.Used != 0 || in.StreamText.End != size || !reflect.DeepEqual(old.Offsets, before) {
				t.Fatalf("%s error=%v text=%q counted=%d mutated old cache=%t", phase, err, got, in.Control.Used, !reflect.DeepEqual(old.Offsets, before))
			}
		})
	}
	in := budgetStreamTextInput(input.CodePoints, text)
	in.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
	if got := in.Text(0, 5); got != "aaaaa" || in.Control.Used != 0 || in.StreamText.End != size {
		t.Fatalf("exact spent=%d text=%q", in.Control.Used, got)
	}
	if in.Text(9, 15) != "aaaaaa" || in.Control.Used != 0 {
		t.Fatal("cached code-point offsets were rebuilt")
	}
}

func TestBudgetStreamTextMalformedAndRetainedCache(t *testing.T) {
	t.Parallel()
	if !state.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, u := range []input.PositionUnit{input.Bytes, input.CodePoints} {
		in := budgetStreamTextInput(u, strings.Repeat("aé😀\xff\xc0z", 500))
		in.Base = int(^uint(0)>>1) - in.Loaded() - 10
		in.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})
		first := in.Text(in.Base+1, in.Base+10)
		var want string
		if u == input.Bytes {
			want = string(in.Bs[1:10])
		} else {
			want = string(in.In[1:10])
		}
		if first != want {
			t.Fatalf("%s malformed text=%q want %q", u, first, want)
		}
		if got := in.Text(in.Base+1100, in.Base+1110); got == "" || first != want || in.StreamText.Base != in.Base+1100 {
			t.Fatal("new snapshot changed old immutable text or absolute positions")
		}
		if u == input.CodePoints {
			in.In[0], in.In[1], in.In[2] = -1, 0xd800, 0x110000
			in.StreamText = state.StreamTextSnapshot{}
			if got := in.Text(in.Base, in.Base+3); got != strings.Repeat(string(utf8.RuneError), 3) {
				t.Fatalf("invalid scalar replacement=%q", got)
			}
		}
	}
}

func TestBudgetStreamTextOffsetCancellation(t *testing.T) {
	t.Parallel()
	if !state.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	in := budgetStreamTextInput(input.CodePoints, strings.Repeat("a", 1024))
	old := state.StreamTextSnapshot{Base: -3, End: -1, Source: "ok", Offsets: []int32{0, 1, 2}}
	in.StreamText = old
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := false
	c := &budgetStreamTextPhaseContext{Context: ctx, observe: func() bool { observed = true; return true }}
	c.cancel = cancel
	in.Control = state.NewBudgetControl(c, runtime.Limits{})
	// Building the offsets of a snapshot counts no unit, so it reaches no check that observes the
	// context: its work is bounded by the size of a snapshot (StreamTextChunk).
	var got string
	err := statetest.CatchFatal(t, func() { got = in.Text(0, 10) })
	if err != nil || observed || got != strings.Repeat("a", 10) || in.Control.Used != 0 || in.StreamText.End != 1024 {
		t.Fatalf("offset building error=%v observed=%t counted=%d", err, observed, in.Control.Used)
	}
}

type budgetStreamTextPhaseContext struct {
	context.Context
	observe func() bool
	cancel  context.CancelFunc
}

func (c *budgetStreamTextPhaseContext) Err() error {
	if c.observe() {
		c.cancel()
	}
	return c.Context.Err()
}
