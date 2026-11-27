//go:build sxir_release

package optcatalog

import (
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

// The release criterion of design record 080: every mechanism of the catalog is classified, and every
// optimization mechanism has a control on every backend it acts on. The release checklist runs it
// with -tags sxir_release.
func TestReleaseCriterion(t *testing.T) {
	if u := pass.Unclassified(); len(u) > 0 {
		t.Errorf("%d mechanisms are not classified: %v", len(u), u)
	}
	for _, m := range pass.Mechanisms() {
		if pass.ControlOf(m.ID, pass.Flags()) == pass.ControlNone {
			t.Errorf("mechanism %s has no control", m.ID)
		} else if u := pass.Uncontrolled(m, pass.Flags()); u != 0 {
			t.Errorf("mechanism %s has no control on %s", m.ID, u)
		}
	}
}
