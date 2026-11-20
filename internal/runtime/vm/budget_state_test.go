//go:build !zp_budget_controls_reference

package vm

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	value "zeroparse.dev/zp/internal/runtime/nodes"
	"zeroparse.dev/zp/internal/runtime/program"
	"zeroparse.dev/zp/internal/runtime/state"
	"zeroparse.dev/zp/pkg/bytecode"
	"zeroparse.dev/zp/pkg/grammar/ast"
	"zeroparse.dev/zp/pkg/runtime"
	zpvalue "zeroparse.dev/zp/pkg/runtime/value"
	"zeroparse.dev/zp/pkg/runtime/value/impl"
)

// TestBudgetStateVMChildAssembly pins that the match code assembles the children of a sequence and of a
// repetition's list without a charge (spec/runtime/parsing.md#cost): under the least limit the assembly of
// 320 children completes, charges nothing and leaves the nodes it takes unchanged.
func TestBudgetStateVMChildAssembly(t *testing.T) {
	for _, op := range []bytecode.Op{bytecode.OpSeq, bytecode.OpEndRepeat} {
		for _, lim := range []runtime.Limits{{Compute: 1}, {Compute: 10000}} {
			p := stateBudgetParser(lim)
			start := 3
			retained := value.Ptr(value.Data{Text: "retained", Fields: valueimpl.Fields{{Name: "keep", Value: "unchanged"}}})
			machineOf(p).vals = append(machineOf(p).vals, &start)
			want := make([]*zpvalue.Node, 320)
			for i := range want {
				if i%3 != 0 {
					want[i] = retained
				}
				machineOf(p).vals = append(machineOf(p).vals, state.NodeOrNil(want[i]))
			}
			instruction := bytecode.Instr{Op: op, A: 320}
			if op == bytecode.OpEndRepeat {
				instruction.A = 1
				machineOf(p).reps = []repState{{base: 1, count: 320}}
			}
			vm := &Program{m: &bytecode.Module{Code: []bytecode.Instr{instruction, {Op: bytecode.OpEnd}}}}
			body := vmBody{}
			var result *zpvalue.Node
			err := catchFatal(t, func() { _, result, _ = machineOf(p).step(vm, &body, false, false, nil) })
			if err != nil || result == nil || !reflect.DeepEqual(value.D(result).Children, want) || value.D(result).Start != 3 {
				t.Fatalf("%v assembled children/position differs: %v", op, err)
			}
			if p.Control.Used != 0 {
				t.Fatalf("%v assembly charged %d units, want none", op, p.Control.Used)
			}
			if value.D(retained).Text != "retained" || value.D(retained).Fields[0].Value != "unchanged" {
				t.Fatal("assembly changed retained node")
			}
		}
	}
}

// TestBudgetStateVMCutAndFailureUnwind pins that a cut charges a step for each entry it passes, so that a
// long cut stops at the budget and at a cancellation, while the unwinding of a failure charges nothing: it
// pops every entry without a step, as flow.reset returns to a mark (spec/flow-ir/operations.md), since the
// work that each entry undoes was charged when it was done.
func TestBudgetStateVMCutAndFailureUnwind(t *testing.T) {
	newEntries := func(p *state.Parser, kind uint8) {
		for range 320 {
			machineOf(p).ents = append(machineOf(p).ents, vmEntry{kind: kind, cut: true})
		}
	}
	vm := &Program{m: &bytecode.Module{Code: []bytecode.Instr{{Op: bytecode.OpCut}, {Op: bytecode.OpEnd}}}}
	cut := func(p *state.Parser) {
		body := vmBody{}
		machineOf(p).step(vm, &body, false, false, nil)
	}

	p := stateBudgetParser(runtime.Limits{Compute: 64})
	newEntries(p, eLabel)
	machineOf(p).ents[0].kind = eChoice
	machineOf(p).ents[0].cut = false
	if err := catchFatal(t, func() { cut(p) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("cut err=%v", err)
	}
	if machineOf(p).ents[0].cut {
		t.Fatal("cut reached its target after budget exhaustion")
	}
	p.Control = state.NewBudgetControl(newPollingContext(1), runtime.Limits{})
	if err := catchFatal(t, func() { cut(p) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cut cancellation=%v", err)
	}

	for _, kind := range []uint8{eChoice, eIter} {
		p := stateBudgetParser(runtime.Limits{Compute: 64})
		newEntries(p, kind)
		before := p.Control.Used
		if err := catchFatal(t, func() { machineOf(p).unwind(vm.m, 0) }); err != nil {
			t.Fatalf("unwinding entries of kind %d: %v", kind, err)
		}
		if n := len(machineOf(p).ents); n != 0 {
			t.Fatalf("unwinding entries of kind %d left %d, want 0", kind, n)
		}
		if p.Control.Used != before {
			t.Fatalf("unwinding entries of kind %d charged %d units, want none", kind, p.Control.Used-before)
		}
	}
}

// TestBudgetStateOperatorsRuleFrameSnapshotAndRestoration checks that the snapshot and restoration of
// a frame of 320 slots around an operator try charge nothing (frame.save and frame.restore,
// spec/exec-ir/operations.md): under any limit that admits the operator, the try keeps its captures,
// and only the recursive form's charge for the operator it tries is used. The try is reset all the
// same: its position is the one before it, and its capture writes are undone, the write to an outer
// frame that the snapshot does not restore included.
func TestBudgetStateOperatorsRuleFrameSnapshotAndRestoration(t *testing.T) {
	for _, iterative := range []bool{false, true} {
		for _, limit := range []uint64{128, 449, 10000} {
			// The recursive form charges the operator it tries before the copies under test.
			compute, operator := limit, uint64(0)
			if !iterative {
				compute, operator = compute+1, 1
			}
			p := stateBudgetParser(runtime.Limits{Compute: compute})
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
					op.Line.M = func(p *state.Parser) (*zpvalue.Node, bool) { setup(p.Frame); return retained, true }
					best = p.Longest([]*state.OperatorsRuleOp{op})
				}
			})
			if err != nil || best.Op != op || best.Frame.Vals[0] != retained {
				t.Fatalf("iterative=%t limit=%d: winning captures lost: %v", iterative, limit, err)
			}
			if p.Pos != 0 || len(p.Trail) != 0 || outer.Vals[0] != old {
				t.Fatalf("iterative=%t limit=%d: the try left position %d, %d trail entries and the outer capture %v, want 0, none and the old value", iterative, limit, p.Pos, len(p.Trail), outer.Vals[0])
			}
			if p.Control.Used != operator {
				t.Fatalf("iterative=%t limit=%d: snapshot and restore used %d units, want %d (the operator)", iterative, limit, p.Control.Used, operator)
			}
			after, _ := json.Marshal(retained)
			if string(before) != string(after) {
				t.Fatal("OperatorsRule frame copy changed public node")
			}
		}
	}
}

// TestBudgetResetAndVMRestoreChargeNothing checks that a reset and the VM's restore undo every capture
// write of the trail, in reverse order, without a charge: under a limit of two units they undo all
// four writes and use none.
func TestBudgetResetAndVMRestoreChargeNothing(t *testing.T) {
	for _, name := range []string{"reset", "restore"} {
		t.Run(name, func(t *testing.T) {
			p := stateBudgetParser(runtime.Limits{Compute: 2})
			f, nodes := undoFixture(p)
			p.Frame = f
			p.Pos = 99
			p.SetVars([]any{1})
			p.Recovered = []state.RecEntry{state.ErrEntry(&zpvalue.SyntaxError{Messages: []string{"temporary"}})}
			if name == "reset" {
				m := state.Mark{Pos: 4, Trail: 0}
				if err := catchFatal(t, func() { p.Reset(m) }); err != nil {
					t.Fatalf("reset = %v, want no interruption", err)
				}
				if p.Pos != 4 || len(p.Trail) != 0 {
					t.Fatalf("reset state: pos=%d trail=%d", p.Pos, len(p.Trail))
				}
			} else {
				machineOf(p).vals = []any{"keep", "discard-one", "discard-two"}
				machineOf(p).reps = []repState{{count: 1}, {count: 2}}
				save := vmSave{pos: 4, vals: 1, reps: 1, trail: 0, recovered: 0, env: nil, frame: f}
				if err := catchFatal(t, func() { machineOf(p).restore(&save) }); err != nil {
					t.Fatalf("VM restore = %v, want no interruption", err)
				}
				if p.Pos != 4 || len(machineOf(p).vals) != 1 || len(machineOf(p).reps) != 1 || len(p.Trail) != 0 {
					t.Fatalf("restore state: pos=%d vals=%d reps=%d trail=%d", p.Pos, len(machineOf(p).vals), len(machineOf(p).reps), len(p.Trail))
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

func TestBudgetDocumentRunStateClearReservation(t *testing.T) {
	marker := value.Ptr(value.Data{Text: "retained"})
	r := &state.RunState{Old: []state.RunElem{{V: marker}}, Mid: make([]state.RunElem, 129), Post: []state.RunElem{{V: marker}}}
	for i := range r.Mid {
		r.Mid[i].V = marker
	}
	p := stateBudgetParser(runtime.Limits{Compute: 128})
	mc := machineOf(p)
	if err := catchFatal(t, func() { mc.freeRunState(r) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("clear interruption = %v", err)
	}
	if len(mc.freeRuns) != 0 || r.Old != nil || r.Mid[128].V != marker || r.Post[0].V != marker || value.D(marker).Text != "retained" {
		t.Fatal("incomplete clear was published or changed pointees")
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 130})
	mc.freeRunState(r)
	if len(mc.freeRuns) != 1 || r.Mid[128].V != nil || r.Post[0].V != nil {
		t.Fatal("completed clear did not release references")
	}
}
