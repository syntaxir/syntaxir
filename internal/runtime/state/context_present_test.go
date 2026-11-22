package state_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// TestContextPresentMemory checks the width of present sets on the leaders of the one cycle of
// codegentest.ContextChain(31, 1, false), 33 of them (L1 to L31, L and G): the first 31 have a
// bit, as on every backend, and the present set of a context of all of them is an identity,
// interned once, that charges one entry of intern with a key of one word of bits. The Go library's
// TestContextPresentMemory checks the same charge for a cycle of as many leaders.
func TestContextPresentMemory(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, "@start\nrule main = "+codegentest.ContextChain(31, 1, false))
	var all, narrow []*program.Rule
	for _, r := range prog.RuleList {
		if !r.Leader {
			continue
		}
		if r.Cycle != prog.ByName["L"].Cycle {
			t.Fatalf("leader %s in cycle %d, L in cycle %d", r.Name, r.Cycle, prog.ByName["L"].Cycle)
		}
		all = append(all, r)
		if r.LeaderIndex < 31 {
			narrow = append(narrow, r)
		}
	}
	if len(all) != 33 || len(narrow) != 31 {
		t.Fatalf("%d leaders, %d of index below 31", len(all), len(narrow))
	}
	sets, mem := state.NamePresent(narrow)
	if want := uint64(1)<<31 - 1; sets != [2]uint64{want, want} || mem != [2]uint64{} {
		t.Errorf("31 leaders: present sets %#x, memory %v; want %#x without memory", sets, mem, want)
	}
	sets, mem = state.NamePresent(all)
	key := uint64(1 + 8) // the kind of the key and one word of bits
	if sets[0]&state.PresentWide == 0 || sets[1] != sets[0] || mem != [2]uint64{state.InternBytes + key, state.InternBytes + key} {
		t.Errorf("33 leaders: present sets %#x, memory %v; want one identity tagged %#x, interned for %d bytes",
			sets, mem, uint64(state.PresentWide), state.InternBytes+key)
	}
}
