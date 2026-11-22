package state

import (
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// The engine records the runs that the Go library of generated parsers records (exec.MinRecordedRun,
// which it is printed with), so that both resume the same elements.
func TestMinRecordedRunMatchesTheGoLibrary(t *testing.T) {
	if minRecordedLen != exec.MinRecordedRun {
		t.Fatalf("the engine records runs of at least %d elements, the Go library of at least %d", minRecordedLen, exec.MinRecordedRun)
	}
}
