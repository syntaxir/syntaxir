//go:build !sxir_budget_controls_reference

package state_test

import (
	"errors"
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestBudgetValueTakeOffChargesNothing checks that taking gathered values off the stack charges nothing
// (spec/runtime/parsing.md#cost: values the parse builds are not an item of the cost), even past a limit
// smaller than the number of values, and that it keeps each value's identity and leaves the values unchanged.
func TestBudgetValueTakeOffChargesNothing(t *testing.T) {
	t.Parallel()
	retained := nodes.Ptr(nodes.Data{Text: "retained", Start: 7, End: 9, Fields: valueimpl.Fields{{Name: "value", Value: "unchanged"}}})
	p := &state.Parser{KidStack: make([]*value.Node, 257)}
	for i := range p.KidStack {
		p.KidStack[i] = retained
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 128})
	var kids []*value.Node
	if err := statetest.CatchFatal(t, func() { kids = p.Kids(0) }); err != nil {
		t.Fatalf("take-off of 257 values under a limit of 128 = %v, want no error", err)
	}
	if p.Control.Used != 0 {
		t.Fatalf("take-off charged %d units, want 0", p.Control.Used)
	}
	if len(kids) != 257 || len(p.KidStack) != 0 {
		t.Fatalf("lengths = %d, %d", len(kids), len(p.KidStack))
	}
	for i, n := range kids {
		if n != retained {
			t.Fatalf("child %d lost its identity", i)
		}
	}
	if nodes.D(retained).Text != "retained" || nodes.D(retained).Start != 7 || nodes.D(retained).End != 9 || nodes.D(retained).Fields[0].Value != "unchanged" {
		t.Fatal("clearing tracking references modified the retained node")
	}
}

func TestBudgetValueFieldAndEnvironmentWalks(t *testing.T) {
	t.Parallel()
	fields := make(valueimpl.Fields, 300)
	st := &program.Type{}
	values := make([]any, len(fields))
	for i := range fields {
		name := fmt.Sprintf("v%d", i)
		fields[i] = valueimpl.NodeField{Name: name, Value: i}
		st.Fields = append(st.Fields, name)
		values[i] = i
	}
	for _, run := range []struct {
		name string
		fn   func(*state.Parser)
	}{
		{"get", func(p *state.Parser) { p.ValueField(fields, "absent") }},
		{"set", func(p *state.Parser) { p.SetValueField(&fields, "absent", 42) }},
		{"type fields", func(p *state.Parser) { p.HasValueField(st, "absent") }},
		{"environment", func(p *state.Parser) {
			for range 300 {
				p.VarValue(299)
			}
		}},
	} {
		t.Run(run.name, func(t *testing.T) {
			p := &state.Parser{}
			p.SetVars(values)
			p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 128})
			if err := statetest.CatchFatal(t, func() { run.fn(p) }); !errors.Is(err, runtime.ErrLimit) {
				t.Fatalf("walk interruption = %v", err)
			}
			if len(fields) != 300 || fields[299].Value != 299 {
				t.Fatal("interrupted field walk modified existing values")
			}
		})
	}
}

func TestBudgetValueTrackingCleanupAfterInterruption(t *testing.T) {
	t.Parallel()
	retained := nodes.Ptr(nodes.Data{Text: "retained"})
	p := &state.Parser{Created: make([]*value.Node, 258)}
	for i := range p.Created {
		p.Created[i] = retained
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 128})
	if err := statetest.CatchFatal(t, func() { p.DropCreated(1) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("tracking cleanup interruption = %v", err)
	}
	// The interruption boundary must disable controls before mandatory cleanup.
	p.ReleaseScratch()
	p.DropCreated(1)
	if p.Control != nil || len(p.Created) != 1 || p.Created[0] != retained || nodes.D(retained).Text != "retained" {
		t.Fatal("cleanup changed the tracking prefix or retained value")
	}
	for _, n := range p.Created[1:cap(p.Created)] {
		if n != nil {
			t.Fatal("mandatory cleanup left a tracking reference")
		}
	}
}
