package vm

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// Completed calls can leave references beyond slice length. Stream commits
// must release them at the next prune without disturbing active root values.
func TestStreamStorageCommitReferences(t *testing.T) {
	prog := &state.SharedProgram{}
	state.InitSharedProgram(prog, compiletest.Program(t, `rule main = "a"`, check.Options{SourceInfo: true}).Runtime())
	p := state.NewParser(prog, strings.Repeat("a", 4096), input.CodePoints, false)
	live := nodes.Ptr(nodes.Data{Text: "live"})
	dead := nodes.Ptr(nodes.Data{Text: "dead"})
	root := p.NewFrame(1)
	root.Vals[0] = live
	p.Frame = root
	p.Trail = make([]state.Undo, 3)
	p.Trail[0] = state.Undo{F: root, Old: live}
	p.Trail[2] = state.Undo{F: &state.Frame{Vals: []*value.Node{dead}}, Old: dead}
	p.Trail = p.Trail[:1]
	regs := &machineOf(p).regs
	active := regs.push(1)
	active[0] = live
	mark := regs.mark()
	copy(regs.push(2), []any{dead, dead}) // the registers of a finished call
	regs.release(mark)
	p.Commit(1023)
	if len(p.Trail) != 0 || p.Memo.Base != 0 || active[0] != live || root.Vals[0] != live {
		t.Fatal("early commit damaged live state or pruned too soon")
	}
	p.Commit(1024)
	if p.Memo.Base != 1024 || active[0] != live || root.Vals[0] != live {
		t.Fatal("pruning damaged live root or active VM register")
	}
	if state.StreamUndoCleanup {
		for _, u := range p.Trail[:cap(p.Trail)] {
			if u.F != nil || u.Old != nil {
				t.Fatal("inactive undo entry retained a completed value")
			}
		}
	}
	if state.StreamStackCleanup {
		for _, v := range regs.chunks[0][1:] {
			if v != nil {
				t.Fatal("inactive VM register retained a completed value")
			}
		}
	}
}
