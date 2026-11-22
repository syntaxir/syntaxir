//go:build !sxir_budget_controls_reference

package vm

import (
	"context"
	"encoding/json"
	"testing"

	value "syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestBudgetStateOperatorsRuleFrameSnapshotAndRestoration checks that the snapshot and restoration of
// a frame of 320 slots around an operator try charge nothing (frame.save and frame.restore,
// spec/exec-ir/operations.md): under any limit that admits the operator, the try keeps its captures,
// and only the recursive form's charge for the operator it tries is used. The try is reset all the
// same: its position is the one before it, and its capture writes are undone, the write to an outer
// frame that the snapshot does not restore included.
func TestBudgetStateOperatorsRuleFrameSnapshotAndRestoration(t *testing.T) {
	for _, iterative := range []bool{false, true} {
		for _, limit := range []uint64{128, 449, 10000} {
			// Neither form counts the operator it tries: an operator is no event
			// (spec/runtime/parsing.md#compute-units).
			p := stateBudgetParser(runtime.Limits{Compute: limit})
			retained := value.Ptr(value.Data{Text: "retained", Fields: valueimpl.Fields{{Name: "keep", Value: "unchanged"}}})
			before, _ := json.Marshal(retained)
			op := &state.OperatorsRuleOp{Kind: ast.Prefix, Line: &state.OperatorsRuleLine{Scope: &program.Scope{Names: make([]string, 320)}}}
			outer, old := p.NewFrame(1), value.Ptr(value.Data{Text: "old"})
			setup := func(f *state.Frame) {
				for i := range f.Vals {
					f.Vals[i] = retained
				}
				outer.Vals[0] = retained
				p.Trail = []state.Undo{{F: f, Slot: 0, Old: nil}, {F: outer, Slot: 0, Old: old}}
				p.Pos = 1
			}
			var best state.OperatorsRuleAttempt
			err := catchFatal(t, func() {
				if iterative {
					f := &longestFrame{started: true, st: state.LongestState{L: []*state.OperatorsRuleOp{op}, F: p.NewFrame(320)}}
					setup(f.st.F)
					f.next(ivmMachineOf(p), nil, iresult{ok: true, v: retained})
					best = ivmMachineOf(p).attempt
				} else {
					op.Line.M = func(p *state.Parser) (*sxirvalue.Node, bool) { setup(p.Frame); return retained, true }
					best = p.Longest([]*state.OperatorsRuleOp{op})
				}
			})
			if err != nil || best.Op != op || best.Frame.Vals[0] != retained {
				t.Fatalf("iterative=%t limit=%d: winning captures lost: %v", iterative, limit, err)
			}
			if p.Pos != 0 || len(p.Trail) != 0 || outer.Vals[0] != old {
				t.Fatalf("iterative=%t limit=%d: the try left position %d, %d trail entries and the outer capture %v, want 0, none and the old value", iterative, limit, p.Pos, len(p.Trail), outer.Vals[0])
			}
			if p.Control.Used != 0 {
				t.Fatalf("iterative=%t limit=%d: snapshot and restore used %d units, want none", iterative, limit, p.Control.Used)
			}
			after, _ := json.Marshal(retained)
			if string(before) != string(after) {
				t.Fatal("OperatorsRule frame copy changed public node")
			}
		}
	}
}

// TestBudgetResetAndVMRestoreChargeNothing checks that a reset and the VM's restore of a Flow body
// (RESET of a mark, then RESTORE of the position and the recovered errors, as a failed alternative
// returns to its start) undo every capture write of the trail, in reverse order, without a charge:
// under a limit of two units they undo all four writes and use none.
func TestBudgetResetAndVMRestoreChargeNothing(t *testing.T) {
	for _, name := range []string{"reset", "restore"} {
		t.Run(name, func(t *testing.T) {
			p := stateBudgetParser(runtime.Limits{Compute: 2})
			f, nodes := undoFixture(p)
			p.Frame = f
			p.Pos = 99
			p.SetVars([]any{1})
			p.Recovered = []state.RecEntry{state.ErrEntry(&sxirvalue.SyntaxError{Messages: []string{"temporary"}})}
			m := state.Mark{Pos: 4, Trail: 0}
			if name == "reset" {
				if err := catchFatal(t, func() { p.Reset(m) }); err != nil {
					t.Fatalf("reset = %v, want no interruption", err)
				}
				if p.Pos != 4 || len(p.Trail) != 0 {
					t.Fatalf("reset state: pos=%d trail=%d", p.Pos, len(p.Trail))
				}
			} else {
				err := catchFatal(t, func() {
					runFlowBody(t, p, []any{m, 4, 0, nil, true},
						bytecode.FlowInstr{Op: bytecode.FlowReset, Args: []int{0}},
						bytecode.FlowInstr{Op: bytecode.FlowRestore, Args: []int{1, 2}},
						bytecode.FlowInstr{Op: bytecode.FlowReturn, Args: []int{3, 4}},
					)
				})
				if err != nil {
					t.Fatalf("VM restore = %v, want no interruption", err)
				}
				if p.Pos != 4 || len(p.Recovered) != 0 || len(p.Trail) != 0 {
					t.Fatalf("restore state: pos=%d recovered=%d trail=%d", p.Pos, len(p.Recovered), len(p.Trail))
				}
			}
			if p.Control.Used != 0 {
				t.Fatalf("%s charged %d units, want none", name, p.Control.Used)
			}
			// The oldest write of each slot is restored last.
			if f.Vals[0] != nodes[0] || f.Vals[1] != nodes[2] {
				t.Fatalf("restored slots=(%p,%p), want (%p,%p)", f.Vals[0], f.Vals[1], nodes[0], nodes[2])
			}
			if value.D(nodes[0]).Text != "old-zero" || value.D(nodes[0]).Fields[0].Value != "zero" {
				t.Fatal("rollback mutated retained node data")
			}
		})
	}
}

// interruptAt makes the k-th reservation of c the one that observes a canceled context: the work of a
// Document counts under no Compute limit, but it keeps advancing the poll counter.
func interruptAt(c *state.BudgetControl, k uint64) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Ctx = ctx
	c.SinceCheck = state.BudgetCheckInterval - k
}

// TestBudgetDocumentRunStateClearCountsNothing checks that letting go of a finished run, work of the
// Document, counts no unit and observes no context, since it is no event
// (spec/runtime/parsing.md#compute-units). A Flow body holds its run (RESUMESTART's state) only in a
// register, which the machine that a Document keeps between parses clears (Machine.Keep): under a
// limit of one unit and a canceled context that the next reservation would observe, the clear
// completes and releases the reference to the run without changing the run's lists or their pointees.
func TestBudgetDocumentRunStateClearCountsNothing(t *testing.T) {
	marker := value.Ptr(value.Data{Text: "retained"})
	r := &state.RunState{Old: []state.RunElem{{V: marker}}, Mid: make([]state.RunElem, 129), Post: []state.RunElem{{V: marker}}}
	for i := range r.Mid {
		r.Mid[i].V = marker
	}
	p := stateBudgetParser(runtime.Limits{Compute: 1})
	interruptAt(p.Control, 1)
	mc := machineOf(p)
	at := mc.regs.mark()
	mc.regs.push(1)[0] = r // the register file of a body that finished the run
	mc.regs.release(at)
	if err := catchFatal(t, func() { mc.Keep(0, false) }); err != nil {
		t.Fatalf("clear = %v, want no interruption", err)
	}
	held := false
	for _, c := range mc.regs.chunks {
		for _, v := range c {
			held = held || v == any(r)
		}
	}
	if p.Control.Used != 0 || held || r.Mid[128].V != marker || r.Post[0].V != marker || value.D(marker).Text != "retained" {
		t.Fatalf("clear used %d units, kept the run %t or changed its lists", p.Control.Used, held)
	}
}
