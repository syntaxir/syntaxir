package libgo

import "testing"

// TestContextPresentMemory checks the width of present sets on a cycle of 33 leaders, those of the
// cycle of the engine's TestContextPresentMemory: the first 31 have a bit, as on every backend,
// and the present set of a context of all of them is an identity, interned once, that charges one
// entry of intern with a key of one word of bits, as in the engine.
func TestContextPresentMemory(t *testing.T) {
	rules := make([]*Rule, 33)
	for i := range rules {
		rules[i] = &Rule{Id: i, Cycle: 1, Leader: true}
	}
	numberLeaders(rules)
	name := func(rules []*Rule) (sets [2]presentSet, mem [2]uint64) {
		p := &Parser{}
		for _, r := range rules {
			p.gamma = append(p.gamma, growRec{rule: int32(r.Id), cycle: r.Cycle, idx: r.leaderIndex})
		}
		for i := range sets {
			sets[i], mem[i] = p.present(0), p.memUsed
		}
		return sets, mem
	}
	if maxLeaderBits != 31 {
		t.Fatalf("%d leaders with a bit", maxLeaderBits)
	}
	sets, mem := name(rules[:31])
	if want := presentSet(1)<<31 - 1; sets != [2]presentSet{want, want} || mem != [2]uint64{} {
		t.Errorf("31 leaders: present sets %#x, memory %v; want %#x without memory", sets, mem, want)
	}
	sets, mem = name(rules)
	key := uint64(1 + 8) // the kind of the key and one word of bits
	if sets[0]&presentWide == 0 || sets[1] != sets[0] || mem != [2]uint64{internBytes + key, internBytes + key} {
		t.Errorf("33 leaders: present sets %#x, memory %v; want one identity tagged %#x, interned for %d bytes",
			sets, mem, presentWide, internBytes+key)
	}
}
