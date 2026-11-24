//go:build !sxir_budget_controls_reference

package state_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/engine"
	value "syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

func undoFixture(p *state.Parser) (*state.Frame, []*sxirvalue.Node) {
	old0 := value.Ptr(value.Data{Text: "old-zero", Fields: valueimpl.Fields{{Name: "keep", Value: "zero"}}})
	first0 := value.Ptr(value.Data{Text: "first-zero"})
	latest0 := value.Ptr(value.Data{Text: "latest-zero"})
	old1 := value.Ptr(value.Data{Text: "old-one"})
	last1 := value.Ptr(value.Data{Text: "last-one"})
	f := &state.Frame{Vals: []*sxirvalue.Node{latest0, old1}}
	p.Trail = []state.Undo{{F: f, Slot: 0, Old: old0}, {F: f, Slot: 0, Old: first0}, {F: f, Slot: 1, Old: old1}, {F: f, Slot: 1, Old: last1}}
	return f, []*sxirvalue.Node{old0, first0, old1, last1, latest0}
}

func assertUndoRestored(t *testing.T, f *state.Frame, nodes []*sxirvalue.Node) {
	t.Helper()
	if f.Vals[0] != nodes[0] || f.Vals[1] != nodes[2] {
		t.Fatalf("restored slots = (%p,%p), want (%p,%p)", f.Vals[0], f.Vals[1], nodes[0], nodes[2])
	}
	if value.D(nodes[0]).Text != "old-zero" || value.D(nodes[0]).Fields[0].Value != "zero" || value.D(nodes[1]).Text != "first-zero" || value.D(nodes[2]).Text != "old-one" || value.D(nodes[3]).Text != "last-one" {
		t.Fatal("undo changed a retained Node pointee")
	}
}

func TestBudgetResetRestoresRepeatedCaptureWritesInReverse(t *testing.T) {
	t.Parallel()
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 4})
	f, nodes := undoFixture(p)
	p.Pos = 71
	p.Frame = f
	p.SetVars([]any{1})
	p.Recovered = []state.RecEntry{state.ErrEntry(&sxirvalue.SyntaxError{Messages: []string{"temporary"}})}
	m := state.Mark{Pos: 4, Env: nil, Trail: 0, Recovered: 0}

	p.Reset(m)
	assertUndoRestored(t, f, nodes)
	if p.Pos != 4 || p.Env != nil || len(p.Recovered) != 0 || len(p.Trail) != 0 {
		t.Fatalf("reset state: pos=%d env=%p recovered=%d trail=%d", p.Pos, p.Env, len(p.Recovered), len(p.Trail))
	}
}

// TestBudgetResetUndoChargesNothing checks that undoing the capture writes of a reset charges nothing
// (captures are not an item of the cost, spec/runtime/parsing.md#cost): a reset that undoes 257 writes
// under a context that cancels at its first poll neither polls nor fails, and restores every slot.
func TestBudgetResetUndoChargesNothing(t *testing.T) {
	t.Parallel()
	p := statetest.NewBudgetParser(runtime.Limits{})
	f := &state.Frame{Vals: make([]*sxirvalue.Node, 257)}
	for i := range f.Vals {
		old := value.Ptr(value.Data{Text: "old"})
		f.Vals[i] = value.Ptr(value.Data{Text: "new"})
		p.Trail = append(p.Trail, state.Undo{F: f, Slot: i, Old: old})
	}
	ctx := statetest.NewPollingContext(1)
	p.Control = state.NewBudgetControl(ctx, runtime.Limits{})
	if err := statetest.CatchFatal(t, func() { p.Reset(state.Mark{Trail: 0}) }); err != nil {
		t.Fatalf("undoing 257 writes failed: %v", err)
	}
	if ctx.Checks != 0 || p.Control.Used != 0 {
		t.Fatalf("undoing 257 writes polled %d times and charged %d units, want none", ctx.Checks, p.Control.Used)
	}
	if len(p.Trail) != 0 {
		t.Fatalf("reset left %d trail entries", len(p.Trail))
	}
	for i := range f.Vals {
		if value.D(f.Vals[i]).Text != "old" {
			t.Fatalf("slot %d not restored: %#v", i, f.Vals[i])
		}
	}
}

func TestBudgetAttachCapturesNilFrameAndFieldOrder(t *testing.T) {
	t.Parallel()
	retained := value.Ptr(value.Data{Text: "base", Fields: valueimpl.Fields{{Name: "existing", Value: "kept"}}})
	allNil := &state.Frame{Vals: make([]*sxirvalue.Node, 320)}
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 100_000})
	got := p.AttachCaptures(retained, &program.Scope{Names: make([]string, len(allNil.Vals))}, allNil, 2, 9)
	if got != retained || len(value.D(retained).Fields) != 1 || value.D(retained).Fields[0].Value != "kept" {
		t.Fatal("all-nil capture frame changed the base node")
	}

	fields := []string{"first", "duplicate", "third", "duplicate", "late_" + strings.Repeat("p", 80)}
	values := []*sxirvalue.Node{
		value.Ptr(value.Data{Text: "first-value"}), value.Ptr(value.Data{Text: "old-duplicate"}), value.Ptr(value.Data{Text: "third-value"}), value.Ptr(value.Data{Text: "new-duplicate"}), value.Ptr(value.Data{Text: "late-value"}),
	}
	base := value.Ptr(value.Data{Text: "fresh-body", Fresh: true})
	result := p.AttachCaptures(base, &program.Scope{Names: fields}, &state.Frame{Vals: values}, 0, 5)
	if result != base {
		t.Fatal("fresh body was unexpectedly wrapped")
	}
	wantNames := []string{"first", "duplicate", "third", fields[4]}
	if len(value.D(result).Fields) != len(wantNames) {
		t.Fatalf("capture field count = %d, want %d", len(value.D(result).Fields), len(wantNames))
	}
	for i, name := range wantNames {
		if value.D(result).Fields[i].Name != name {
			t.Fatalf("field %d name = %q, want %q", i, value.D(result).Fields[i].Name, name)
		}
	}
	if value.D(result).Fields[1].Value != values[3] || value.D(result).Fields[3].Value != values[4] {
		t.Fatal("capture overwrite/order semantics changed")
	}
}

func TestBudgetAttachCapturesWrapsNonfreshAndSelfCapture(t *testing.T) {
	t.Parallel()
	child := value.Ptr(value.Data{Text: "retained-child"})
	prior := valueimpl.Fields{{Name: "prior", Value: "unchanged"}}
	shared := value.Ptr(value.Data{Text: "shared", Fields: append(valueimpl.Fields(nil), prior...)})
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 100_000})
	wrapped := p.AttachCaptures(shared, &program.Scope{Names: []string{"child"}}, &state.Frame{Vals: []*sxirvalue.Node{child}}, 4, 7)
	if wrapped == shared || len(value.D(wrapped).Children) != 1 || value.D(wrapped).Children[0] != shared {
		t.Fatal("nonfresh node was not wrapped before capture attachment")
	}
	if got, ok := value.D(wrapped).Fields.Get("child"); !ok || got != child {
		t.Fatalf("wrapped child capture = %v, %t", got, ok)
	}
	if !reflect.DeepEqual(value.D(shared).Fields, prior) || value.D(shared).Text != "shared" || value.D(child).Text != "retained-child" {
		t.Fatal("capture attachment modified a retained node")
	}

	self := value.Ptr(value.Data{Text: "self", Fresh: true})
	got := p.AttachCaptures(self, &program.Scope{Names: []string{"self"}}, &state.Frame{Vals: []*sxirvalue.Node{self}}, 1, 2)
	if got == self || len(value.D(got).Children) != 1 || value.D(got).Children[0] != self {
		t.Fatal("self-capture did not wrap the body node")
	}
	if val, ok := value.D(got).Fields.Get("self"); !ok || val != self || len(value.D(self).Fields) != 0 {
		t.Fatal("self-capture fields or source node changed")
	}
}

// TestBudgetAttachCapturesWideScopesChargeNothing checks that attaching captures charges nothing however
// wide the scope (spec/runtime/parsing.md#cost: captures are not an item of the cost): neither the scan
// of a scope whose slots are all empty nor the field walk of many long names, each under a limit smaller
// than the scope; and that attaching to a node that is not fresh leaves that node unchanged.
func TestBudgetAttachCapturesWideScopesChargeNothing(t *testing.T) {
	t.Parallel()
	base := value.Ptr(value.Data{Text: "retained", Fields: valueimpl.Fields{{Name: "prior", Value: "kept"}}})
	wide := &state.Frame{Vals: make([]*sxirvalue.Node, 320)}
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 128})
	if err := statetest.CatchFatal(t, func() { _ = p.AttachCaptures(base, &program.Scope{Names: make([]string, 320)}, wide, 0, 1) }); err != nil {
		t.Fatalf("all-nil scan = %v, want no error", err)
	}
	if p.Control.Used != 0 {
		t.Fatalf("all-nil scan charged %d units, want 0", p.Control.Used)
	}
	if len(value.D(base).Fields) != 1 || value.D(base).Fields[0].Value != "kept" {
		t.Fatal("the all-nil scan changed the retained node")
	}

	const n = 48
	names := make([]string, n)
	vals := make([]*sxirvalue.Node, n)
	for i := range names {
		names[i] = fmt.Sprintf("capture_%03d_%s", i, strings.Repeat("common_prefix_", 12))
		vals[i] = value.Ptr(value.Data{Text: fmt.Sprintf("value-%d", i)})
	}
	retained := value.Ptr(value.Data{Text: "shared-retained", Fields: valueimpl.Fields{{Name: "existing", Value: "stable"}}})
	p = statetest.NewBudgetParser(runtime.Limits{Compute: 32})
	var got *sxirvalue.Node
	if err := statetest.CatchFatal(t, func() { got = p.AttachCaptures(retained, &program.Scope{Names: names}, &state.Frame{Vals: vals}, 3, 9) }); err != nil {
		t.Fatalf("capture field walk = %v, want no error", err)
	}
	if p.Control.Used != 0 {
		t.Fatalf("capture field walk charged %d units, want 0", p.Control.Used)
	}
	if got == retained || len(value.D(got).Fields) != n {
		t.Fatalf("attachment returned the retained node or %d fields, want a new node with %d", len(value.D(got).Fields), n)
	}
	if len(value.D(retained).Fields) != 1 || value.D(retained).Fields[0].Name != "existing" || value.D(retained).Fields[0].Value != "stable" || value.D(retained).Text != "shared-retained" {
		t.Fatal("capture assembly modified the retained nonfresh node")
	}
}

// TestBudgetStateBatchRollbackAndPoolRetry rolls back 320 capture writes, which charges nothing, and
// interrupts the parse right after (one unit past the match of arm), whether before or at the call of
// fallback; the interrupted parser leaves no scratch or control behind, and a retry from the pool parses.
func TestBudgetStateBatchRollbackAndPoolRetry(t *testing.T) {
	t.Parallel()
	var body strings.Builder
	body.WriteString(`c0:"a" `)
	for i := 1; i < 320; i++ {
		fmt.Fprintf(&body, "c%d:_ ", i)
	}
	var predicate strings.Builder
	predicate.WriteString(`text($c0)=="a"`)
	for i := 1; i < 320; i++ {
		fmt.Fprintf(&predicate, ` && text($c%d)==""`, i)
	}
	src := `rule main = (` + body.String() + ` {` + predicate.String() + `} arm "!" / fallback) #recover(skip=.*) $$
rule fallback = .* #atomic
rule arm = _`
	prog := enginetest.MustCompile(t, src)
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			for _, recognize := range []bool{false, true} {
				armed := false
				fallback := 0
				var observed *state.Parser
				n, err := prog.ParseWith("main", "a", engine.ParseOptions{Backend: b, Unit: u, Recognize: recognize, Limits: runtime.Limits{Compute: 1 << 50}, Trace: func(e engine.TraceEvent) {
					observed = state.TraceParser(e)
					if e.Kind == engine.TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if e.Kind == engine.TraceExit && e.Rule == "arm" && e.Matched {
						if len(state.TraceParser(e).Trail) < 320 {
							t.Fatalf("%v/%v recognize=%t: outer capture trail=%d", b, u, recognize, len(state.TraceParser(e).Trail))
						}
						armed = true
						state.TraceParser(e).Control.Limits.Compute = state.TraceParser(e).Control.Used + 1
					}
				}})
				if !armed || n != nil || !errors.Is(err, runtime.ErrLimit) || fallback > 1 || observed == nil || observed.Scratch != nil || observed.Control != nil {
					t.Fatalf("%v/%v recognize=%t: armed=%t returned=%t err=%v fallback=%d", b, u, recognize, armed, n != nil, err, fallback)
				}
				n, err = prog.ParseWith("main", "a", engine.ParseOptions{Backend: b, Unit: u, Recognize: recognize})
				if err != nil || (recognize && n != nil) || (!recognize && n == nil) {
					t.Fatalf("%v/%v retry: %v", b, u, err)
				}
			}
		}
	}
}

func TestBudgetStateProjectedFrameClear(t *testing.T) {
	t.Parallel()
	const count = 320
	var items []ir.Node
	var slots []ir.Slot
	for i := range count {
		name := fmt.Sprintf("f%d", i)
		text := &ir.Text{Body: &ir.Succeed{}}
		text.Attrs().Want = true
		items = append(items, &ir.Capture{Slot: i, Name: name, Body: text})
		slots = append(slots, ir.Slot{Name: name})
	}
	rep := &ir.Repeat{Min: 0, Max: 1, Body: &ir.Seq{Items: items}, Scope: slots, Project: "f0"}
	empty := frontend.NewResult(check.NewModel(nil, nil), nil, check.Sites{})
	m, err := closure.Matcher(empty.Runtime(), empty.Checker(), &ir.Module{}, &ir.Capture{Name: "fs", Body: rep, Project: "f0"}, []ir.Slot{{Name: "fs"}})
	if err != nil {
		t.Fatal(err)
	}
	// Clearing the frame of the projected captures is no event, nor are the values of the empty
	// texts, so the parse ends under a limit below the number of captures.
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 64})
	control := p.Control
	p.Input = state.NewTextInput("", input.Bytes, false)
	p.Control = control
	p.Frame = p.NewFrame(1)
	// Each capture writes once, the count captures and fs in the caller's frame; the clear writes none.
	if err := statetest.CatchFatal(t, func() { m(p) }); err != nil || p.Control.Used != 0 || len(p.Trail) != count+1 {
		t.Fatalf("projected clear err=%v units=%d capture writes=%d", err, p.Control.Used, len(p.Trail))
	}
}
