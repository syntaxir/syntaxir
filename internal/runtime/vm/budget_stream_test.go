//go:build !sxir_budget_controls_reference

package vm

import (
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func TestBudgetStreamDiscardAndStackClear(t *testing.T) {
	for _, u := range []input.PositionUnit{input.Bytes, input.CodePoints} {
		t.Run(u.String(), func(t *testing.T) {
			in := state.Input{Unit: u, Eof: true, BaseLine: 1, BaseCol: 1, Lines: []int{0}, Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 128})}
			if u == input.Bytes {
				in.Bs = []byte(strings.Repeat("a", 258))
			} else {
				in.In = []rune(strings.Repeat("a", 258))
			}
			got := catchStreamPanic(func() { in.Discard(129) })
			if _, ok := got.(state.Fatal); !ok || in.Base != 0 || in.Control.Used != 128 {
				t.Fatalf("discard interruption=%v base=%d steps=%d", got, in.Base, in.Control.Used)
			}
		})
	}
	t.Run("trail", func(t *testing.T) {
		// The commit interrupts the cleanup of the trail, which its reference build leaves out.
		if !state.StreamUndoCleanup {
			t.Skip("the build does not clear the inactive capacity of the trail")
		}
		node := nodes.Ptr(nodes.Data{Text: "owned"})
		p := &state.Parser{Input: state.Input{Unit: input.Bytes, Eof: true, Bs: []byte(strings.Repeat("a", 3000)), BaseLine: 1, BaseCol: 1, Lines: []int{0}, Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 953 + 128})}, Memo: state.NewMemoTable()}
		p.Trail = make([]state.Undo, 0, 129)
		got := catchStreamPanic(func() { p.Commit(2048) })
		if _, ok := got.(state.Fatal); !ok || p.Base != 2047 || p.Memo.Base != 0 || nodes.D(node).Text != "owned" {
			t.Fatalf("commit trail interruption=%v base=%d pruned=%d", got, p.Base, p.Memo.Base)
		}
	})
}

// TestBudgetStreamRegisterClear checks that a stream commit clears the registers above the files of
// the running Flow bodies, those of chunks above the chunk in use among them, and keeps the files in
// use, without a charge: the registers are no item of the cost.
func TestBudgetStreamRegisterClear(t *testing.T) {
	node := nodes.Ptr(nodes.Data{Text: "owned"})
	p := &state.Parser{Input: state.Input{Unit: input.Bytes, Eof: true, Bs: []byte("aaaa"), BaseLine: 1, BaseCol: 1, Lines: []int{0}, Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 1})}, Memo: state.NewMemoTable()}
	regs := &machineOf(p).regs
	live := regs.push(2)
	mark := regs.mark()
	for _, file := range [][]any{regs.push(MinRegChunk - 2), regs.push(3)} { // the second takes a chunk of its own
		for i := range file {
			file[i] = node
		}
	}
	regs.release(mark)
	live[0], live[1] = node, node
	before := p.Control.Used
	machineOf(p).ClearInactive()
	if p.Control.Used != before {
		t.Fatalf("clearing the registers charged %d units", p.Control.Used-before)
	}
	if live[0] != node || live[1] != node {
		t.Fatal("the clear dropped registers in use")
	}
	if len(regs.chunks) != 1 || slices.ContainsFunc(regs.chunks[0][2:], func(v any) bool { return v != nil }) {
		t.Fatalf("%d chunks remain, or the chunk in use holds registers of finished files", len(regs.chunks))
	}
}
