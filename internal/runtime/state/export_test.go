package state

import "syntaxir.dev/sxir/internal/runtime/program"

// The exports of the internal test helpers to the tests of package state_test, which compile action expressions
// with the compiler side (actionlower) that imports this package.
var (
	Element           = element
	EngineMemberCanon = engineMemberCanon
)

// InternBytes and PresentWide export internBytes and presentWide to TestContextPresentMemory.
const (
	InternBytes = internBytes
	PresentWide = presentWide
)

// NamePresent names twice, as a call in a context does, the present set of a context of a growth
// of each of the leaders at one position, and returns the set and the memory used after each time.
func NamePresent(leaders []*program.Rule) (sets, mem [2]uint64) {
	p := &Parser{}
	for _, r := range leaders {
		p.gamma = append(p.gamma, growRec{key: MemoKey{Rule: r.ID}, bit: r.LeaderBit, idx: int32(r.LeaderIndex), cycle: int32(r.Cycle)})
	}
	for i := range sets {
		sets[i], mem[i] = p.present(0), p.MemUsed
	}
	return sets, mem
}
