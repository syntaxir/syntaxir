//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

func TestBudgetMemoEnvValuesDeepLookup(t *testing.T) {
	t.Parallel()
	const count = 320 // three levels: two inner nodes and a leaf on every path
	p := statetest.MemoParser(count)
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1_000_000})
	vals := p.EnvValues([]int{count - 1, -1})
	if len(vals) != 2 || vals[0] != count-1 || vals[1] != nil {
		t.Fatalf("envValues = %#v, want [%d nil]", vals, count-1)
	}
	// Looking up the values of a key is no event: the call counts the key (ChargeKey), whatever the
	// depth of the environment.
	if p.Control.Used != 0 {
		t.Fatalf("lookups used %d steps, want 0", p.Control.Used)
	}

	limited := statetest.MemoParser(count)
	limited.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 3})
	if err := statetest.CatchFatal(t, func() { limited.EnvValues([]int{0}) }); err != nil || limited.Control.Used != 0 {
		t.Fatalf("deep envValues error = %v, counted %d units, want neither", err, limited.Control.Used)
	}

	// The nil-environment key remains a shared all-nil slice and resolves missing variables.
	nilParser := &state.Parser{Input: state.Input{Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 8})}}
	got := nilParser.EnvValues([]int{-1, -1})
	if len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Fatalf("nil envValues = %#v, want two nil values", got)
	}
}

func TestBudgetMemoBindPreservesSnapshotsOnInterruption(t *testing.T) {
	t.Parallel()
	const count = 48 // two levels: an inner node and a leaf on every path
	// A rebind is no event: it counts nothing, copies the path and leaves the saved snapshot intact.
	p := statetest.MemoParser(count)
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 2})
	before := p.Env
	if err := statetest.CatchFatal(t, func() { p.Bind(0, "replacement") }); err != nil || p.Control.Used != 0 {
		t.Fatalf("replacement error = %v, counted %d units, want neither", err, p.Control.Used)
	}
	if p.Env == before || p.Env.Get(0, p.EnvDepth()) != "replacement" {
		t.Fatal("the bind did not publish the new environment")
	}
	for i := 0; i < count; i++ {
		if got := before.Get(i, p.EnvDepth()); got != i {
			t.Fatalf("saved environment changed at slot %d: %v", i, got)
		}
	}

	// Equal bindings keep the persistent snapshot identity.
	stable := statetest.MemoParser(count)
	stable.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1_000_000})
	head := stable.Env
	stable.Bind(count-1, count-1)
	stable.Bind(0, 0)
	if stable.Env != head {
		t.Fatal("equal bind changed environment identity")
	}

	// A completed update copies only the path and leaves the old snapshot intact.
	stable.Bind(0, "replacement")
	if stable.Env == head {
		t.Fatal("changed bind reused the old environment")
	}
	if got := stable.Env.Get(0, stable.EnvDepth()); got != "replacement" {
		t.Fatalf("replacement lookup = %v", got)
	}
	if got := head.Get(0, stable.EnvDepth()); got != 0 {
		t.Fatalf("saved lookup = %v; want 0", got)
	}
	if got := stable.Env.Get(count-1, stable.EnvDepth()); got != count-1 {
		t.Fatalf("replacement lost slot %d: %v", count-1, got)
	}
}

func TestBudgetMemoKeyEqualityAndLongChains(t *testing.T) {
	t.Parallel()
	table := &state.MemoTable{Slots: make([]*state.MemoEntry, 1)}
	p := &state.Parser{Memo: table}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})
	long := strings.Repeat("common-prefix-", 96)
	var head *state.MemoEntry
	for i := 0; i < 320; i++ {
		env := []any{i, long + fmt.Sprint(i)}
		e := &state.MemoEntry{Rule: int32(i), Min: int32(i % 3), Pos: 0, Env: env, Next: head}
		head = e
	}
	table.Slots[0] = head

	key := state.MemoKey{Rule: 0, Pos: 0, Min: 0, Env: []any{0, long + "0"}}
	if got, ok := p.MemoGet(key); !ok || got.Rule != 0 {
		t.Fatalf("late memo hit = %v, %t", got, ok)
	}
	key.Env = []any{0, long + "different"}
	if got, ok := p.MemoGet(key); ok || got != nil {
		t.Fatalf("memo miss = %v, %t; want nil, false", got, ok)
	}

	// slices.Equal semantics distinguish a typed nil from a nil interface and equate nil/empty slices.
	var typedNil *value.Node
	table.Slots[0] = &state.MemoEntry{Rule: 9, Pos: 0, Env: []any{typedNil}}
	if got, ok := p.MemoGet(state.MemoKey{Rule: 9, Pos: 0, Env: []any{nil}}); ok || got != nil {
		t.Fatalf("typed-nil key matched nil interface: %v, %t", got, ok)
	}
	table.Slots[0] = &state.MemoEntry{Rule: 9, Pos: 0}
	if got, ok := p.MemoGet(state.MemoKey{Rule: 9, Pos: 0, Env: []any{}}); !ok || got == nil {
		t.Fatalf("nil/empty key equality = %v, %t; want a hit", got, ok)
	}
}

func budgetMemoVariableGrammar() string {
	const count = 280
	var src strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&src, "var memo_key_with_common_prefix_%d int\n", i)
	}
	src.WriteString("rule main = ")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&src, "{memo_key_with_common_prefix_%d := %d} ", i, i)
	}
	src.WriteString("(r \"x\" / r) $$\nrule marker = ^^\nrule r = marker ")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&src, "{memo_key_with_common_prefix_%d == %d} ", i, i)
	}
	src.WriteString("\"a\"")
	return src.String()
}

func TestBudgetMemoParseBudgetPoolRetryAndRetainedResult(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, budgetMemoVariableGrammar())
	input := "a"
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints} {
			// The memo keys of 280 variables cost more than the default budget of a one-character
			// input: the fixture's cost comes from the grammar, so the setup parse disables the
			// budget. The limit below falls inside the key of the first call of r, which reads
			// every variable (four units each).
			opts := engine.ParseOptions{Backend: backend, Unit: unit, Limits: runtime.Limits{Compute: runtime.Unlimited}}
			retained, err := prog.ParseWith("main", input, opts)
			if err != nil || retained == nil {
				t.Fatalf("%v/%v setup parse: node=%v err=%v", backend, unit, retained, err)
			}
			before, jsonErr := json.Marshal(retained)
			if jsonErr != nil {
				t.Fatal(jsonErr)
			}

			armed := false
			markerCalls := 0
			limited := opts
			limited.Limits = runtime.Limits{Compute: 1 << 50}
			limited.Trace = func(e engine.TraceEvent) {
				if e.Kind == engine.TraceEnter && e.Rule == "marker" {
					markerCalls++
				}
				if !armed && e.Kind == engine.TraceEnter && e.Rule == "r" {
					armed = true
					state.TraceParser(e).DeferMemo = false
					state.TraceParser(e).Control.Limits.Compute = state.TraceParser(e).Control.Used + 16
				}
			}
			if node, parseErr := prog.ParseWith("main", input, limited); node != nil || !errors.Is(parseErr, runtime.ErrLimit) {
				t.Fatalf("%v/%v limited parse: node=%v err=%v", backend, unit, node, parseErr)
			}
			if markerCalls != 0 {
				t.Fatalf("memo-key interruption entered rule body %d times", markerCalls)
			}
			if !armed {
				t.Fatalf("%v/%v limited parse never reached the memoized rule", backend, unit)
			}
			retry, err := prog.ParseWith("main", input, opts)
			if err != nil || retry == nil {
				t.Fatalf("%v/%v retry parse: node=%v err=%v", backend, unit, retry, err)
			}
			if after, jsonErr := json.Marshal(retained); jsonErr != nil || string(after) != string(before) {
				t.Fatalf("%v/%v retained result changed after pool reuse", backend, unit)
			}
		}
	}
}

func TestBudgetMemoBindCopyAndCancellation(t *testing.T) {
	t.Parallel()
	// An equal binding performs the lookup but does not copy a path.
	probe := statetest.MemoParser(320)
	root := probe.Env
	probe.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})
	probe.Bind(0, 0)
	searchSteps := probe.Control.Used
	p := statetest.MemoParser(320)
	root = p.Env
	// Neither the lookup nor the copy of the path counts a unit.
	if searchSteps != 0 {
		t.Fatalf("an equal binding counted %d units", searchSteps)
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: searchSteps + 2})
	if err := statetest.CatchFatal(t, func() { p.Bind(0, 999) }); err != nil || p.Control.Used != 0 {
		t.Fatalf("path-copy error=%v, counted %d units", err, p.Control.Used)
	}
	if p.Env == root {
		t.Fatal("the bind did not publish the copied path")
	}
	p.Control = nil
	p.Bind(0, 999)
	if got := p.Env.Get(0, p.EnvDepth()); got != 999 {
		t.Fatal("retry lost changed binding")
	}
	if got := root.Get(0, p.EnvDepth()); got != 0 {
		t.Fatal("path copy changed saved snapshot")
	}
	for _, run := range []func(*state.Parser){func(p *state.Parser) { p.EnvValues([]int{0}) }, func(p *state.Parser) { p.Bind(0, 999) }} {
		p := statetest.MemoParser(320)
		root := p.Env
		p.Control = state.NewBudgetControl(statetest.NewPollingContext(1), runtime.Limits{})
		p.Control.Charge(state.BudgetCheckInterval - 2) // a count of two more units would reach the poll
		// The environment operations count nothing, so they reach no poll and observe no context.
		if err := statetest.CatchFatal(t, func() { run(p) }); err != nil || p.Control.Used != state.BudgetCheckInterval-2 {
			t.Fatalf("environment operation=%v, counted %d units", err, p.Control.Used-(state.BudgetCheckInterval-2))
		}
		_ = root
	}
}

// TestBudgetMemoChainAndStringKeysChargeNothing pins that walking a memo chain and comparing string
// keys charge nothing (spec/runtime/parsing.md#cost: the call paid for its key): a lookup and a
// store over a chain of 320 entries, and a lookup that compares a key of 1024 bytes, succeed under a
// limit of one unit, charge none of it and reach no poll of the context, and a store still retires
// the entry it replaces without changing its public graph.
func TestBudgetMemoChainAndStringKeysChargeNothing(t *testing.T) {
	t.Parallel()
	table := &state.MemoTable{Slots: make([]*state.MemoEntry, 1)}
	for i := 0; i < 320; i++ {
		table.Slots[0] = &state.MemoEntry{Rule: int32(i), Next: table.Slots[0]}
	}
	replacement := &state.MemoEntry{Node: nodes.Ptr(nodes.Data{Text: "replacement"})}
	p := &state.Parser{Memo: table}
	free := func(what string, run func()) {
		t.Helper()
		p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
		if err := statetest.CatchFatal(t, run); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if p.Control.Used != 0 {
			t.Fatalf("%s charged %d units, want none", what, p.Control.Used)
		}
		p.Control = state.NewBudgetControl(statetest.NewPollingContext(1), runtime.Limits{})
		p.Control.Charge(state.BudgetCheckInterval - 1) // a unit more reaches the poll
		if err := statetest.CatchFatal(t, run); err != nil {
			t.Fatalf("%s polled the context: %v", what, err)
		}
	}
	free("chain get", func() { p.MemoGet(state.MemoKey{Rule: 0}) })
	free("chain put", func() { p.MemoPut(state.MemoKey{Rule: 0}, replacement) })
	if got, ok := p.MemoGet(state.MemoKey{Rule: 0}); !ok || got != replacement {
		t.Fatal("replacement failed")
	}
	// Retiring bookkeeping must not clear the old public node or its descendants.
	retained := nodes.Ptr(nodes.Data{Text: "retained", Children: []*value.Node{nodes.Ptr(nodes.Data{Text: "child"})}, Fields: valueimpl.Fields{{Name: "value", Value: "unchanged"}}})
	table.Slots[0] = &state.MemoEntry{Rule: 1, Node: retained, Env: []any{strings.Repeat("x", 1024)}}
	before, _ := json.Marshal(retained)
	key := state.MemoKey{Rule: 1, Env: []any{strings.Repeat("x", 1024)}}
	free("string key comparison", func() {
		if _, ok := p.MemoGet(key); !ok {
			t.Fatal("equal string key missed")
		}
	})
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})
	p.MemoPut(key, &state.MemoEntry{})
	after, _ := json.Marshal(retained)
	if string(before) != string(after) {
		t.Fatal("entry retirement changed public graph")
	}
	// Unequal string lengths and key lengths remain rejections.
	free("key length mismatch", func() {
		if got, ok := p.MemoGet(state.MemoKey{Rule: 1, Env: nil}); ok || got != nil {
			t.Fatal("key length mismatch matched")
		}
	})
	free("string length mismatch", func() {
		if got, ok := p.MemoGet(state.MemoKey{Rule: 1, Env: []any{"short"}}); ok || got != nil {
			t.Fatal("string length mismatch matched")
		}
	})
}

func TestBudgetMemoBatchGrowthInterruption(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = expr #recover(skip=.*) $$

rule expr = expr atom / atom

rule atom = "a"`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, cancel := range []bool{false, true} {
					retained := nodes.Ptr(nodes.Data{Text: "retained", Children: []*value.Node{nodes.Ptr(nodes.Data{Text: "child"})}})
					before, _ := json.Marshal(retained)
					var observed *state.Parser
					var chunks [][]state.MemoEntry
					armed := false
					exprExits := 0
					opts := engine.ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, Limits: runtime.Limits{Compute: 1 << 40}, Trace: func(e engine.TraceEvent) {
						if e.Kind == engine.TraceExit && e.Rule == "expr" {
							exprExits++
						}
						if !armed && e.Kind == engine.TraceExit && e.Rule == "atom" {
							armed = true
							observed = state.TraceParser(e)
							statetest.MemoPrefix(state.TraceParser(e).Memo, retained)
							chunks = append([][]state.MemoEntry(nil), state.TraceParser(e).Memo.Chunks...)
							if cancel {
								state.TraceParser(e).Control.Ctx = statetest.NewPollingContext(2)
							} else {
								// Stop at the next charge, inside the growth of expr.
								state.TraceParser(e).Control.Limits.Compute = state.TraceParser(e).Control.Used + 1
							}
						}
					}}
					n, err := prog.ParseWith("main", "a", opts)
					want := runtime.ErrLimit
					if cancel {
						want = context.Canceled
					}
					if !armed || n != nil || !errors.Is(err, want) {
						t.Fatalf("%v/%v recognize=%v cancel=%v: armed=%v node=%v error=%v", backend, unit, recognize, cancel, armed, n, err)
					}
					if exprExits != 1 { // The initial recursive seed call exits before atom enters.
						t.Fatalf("%v/%v recognize=%v cancel=%v: unexpected completed expr calls: %d", backend, unit, recognize, cancel, exprExits)
					}
					if observed.Control != nil || observed.Scratch != nil {
						t.Fatal("batch interruption retained invocation state")
					}
					if state.ScratchPools {
						// The reference build (sxir_reference_scratch_pools) drops the memo with the
						// parser instead of clearing it for a later parse.
						statetest.RequireMemoCleared(t, chunks)
					}
					after, _ := json.Marshal(retained)
					if string(after) != string(before) {
						t.Fatal("batch cleanup changed retained graph")
					}
					retry, retryErr := prog.ParseWith("main", "a", engine.ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
					if retryErr != nil || (retry == nil) != recognize {
						t.Fatalf("retry=%v/%v", retry, retryErr)
					}
				}
			}
		}
	}
}
