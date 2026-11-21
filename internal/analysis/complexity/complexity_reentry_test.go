package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/starttest"
)

// reentryWitness has a continuation all of whose paths re-enter the cycle: the later alternative a
// of the units "(" c ")" "y" evaluates the left call of the leader c before it examines any input,
// and only "+" follows that call.
const reentryWitness = "@start\nrule main = c $$\nrule a = c \"+\"\nrule c = \"n\" / \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n"

// reentryKept has the same continuation followed by a scan after the call, which is not bounded,
// so the continuation is kept and compared.
const reentryKept = "@start\nrule main = c $$\nrule a = c \"+\" [x]* \"!\"\nrule c = \"n\" / \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n"

// TestReentryLeavesOutWholeContinuation checks that withoutReentry leaves out a continuation all
// of whose paths re-enter the cycle, and only then: in reentryWitness the unit "(" c ")" "y" loses
// the continuation a, and the grammar is linear (its cost is measured in
// TestReentryWitnessScale); with the re-entry rule off (the mutant) the continuation is compared,
// and the grammar is not classified linear. In reentryKept the continuation is kept, and the
// grammar is not classified linear either.
func TestReentryLeavesOutWholeContinuation(t *testing.T) {
	defer func() { reentryOn = true }()
	for _, c := range []struct {
		name, src string
		dropped   bool
		class     ComplexityClass
		without   ComplexityClass
	}{
		{"every path re-enters", reentryWitness, true, ClassLinear, ClassUnknown},
		{"a scan follows the call", reentryKept, false, ClassUnknown, ClassUnknown},
	} {
		reentryOn = true
		g, err := parser.Parse(c.src)
		if err != nil {
			t.Fatal(err)
		}
		k, _, err := prepareClassifier(starttest.MarkAll(g), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := k.classify(); got.Class != c.class {
			t.Errorf("%s: class %v, want %v\nreasons %v", c.name, got.Class, c.class, got.Reasons)
		}
		dropped := false
		for _, u := range k.units {
			if u.kind != unitAlt || u.rule != "c" || !u.hasSource {
				continue
			}
			if len(k.withoutReentry(u)) < len(u.conts) {
				dropped = true
			}
		}
		if dropped != c.dropped {
			t.Errorf("%s: a continuation left out %v, want %v", c.name, dropped, c.dropped)
		}
		reentryOn = false
		if got := classifySource(t, c.src).Class; got != c.without {
			t.Errorf("%s without the re-entry rule: class %v, want %v", c.name, got, c.without)
		}
	}
}
