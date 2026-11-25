package libgo

import (
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// The rename of an expectation set to a label is no event of logical units: a set far larger than the
// compute limit is renamed whole without counting a unit or stopping the parse.
func TestDiagnosticRenameOfLargeExpectationSetsCountsNoUnits(t *testing.T) {
	ids := make([]ExpID, 512)
	for i := range ids {
		ids[i] = ExpID(i + 1)
	}
	ids[7] |= MsgBit
	p := &Parser{}
	p.startUnits(prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: 64}}, 0, false))
	got := RenamedExpected(p, ids, 999)
	if p.UnitsNow() != 0 {
		t.Fatalf("rename counted %d units, want none", p.UnitsNow())
	}
	for i, id := range got {
		want := ExpID(999)
		if i == 7 {
			want = ExpID(8) | MsgBit
		}
		if id != want {
			t.Fatalf("entry %d = %d, want %d", i, id, want)
		}
	}
}
