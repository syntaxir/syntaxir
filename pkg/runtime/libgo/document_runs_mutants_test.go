package libgo_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// runMutants change the resumption of repetition runs in a Document parse in one way each: in the
// compositions printed into call_exec.go (runReuse, runPrefix, runResync, runTail) or in the run
// records of document_runs.go.
var runMutants = []goMutant{
	{"a reused element observes no context", "call_exec.go", "runReuse", "p.poll()\n\tv := p.runTake", "v := p.runTake"},
	{"a resumed element counts only as a memo hit", "call_exec.go", "runReuse", "p.Document.stats.Resumed++\n", ""},
	{"the reused prefix ignores the edit", "call_exec.go", "runPrefix", "if r.old[r.K].examined > r.EditStart {", "if false {"},
	{"the reused tail does not move", "call_exec.go", "runTail", "p.runReuse(r, i, r.EditDelta,", "p.runReuse(r, i, 0,"},
	{"a run resyncs at an old element that starts elsewhere", "call_exec.go", "runResync", "r.Seek == n || r.old[r.Seek].start != at", "r.Seek == n"},
	{"a moved element keeps the positions of its value", "document_runs.go", "runTake", "el.v = p.moveValue(el.v, shift, uint32(gen))\n", ""},
	{"the elements after the edit are recorded before the old ones", "document_runs.go", "runAdd", "if after {", "if false {"},
}

// TestDocumentRunMutants is the proof that the run tests of document_runs_test.go hold the run
// resumption of the Go library: with the source of each mutant, one of them fails.
func TestDocumentRunMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	for _, m := range runMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			overlay := mutatedLib(t, m.file, "func (p *Parser) ", m.fn, m.old, m.new)
			// -vet=off: the verdict reads what the tests report, and vet would only analyze the mutated code again.
			out, err := goTestLib(overlay, "-count=1", "-vet=off", "-run", "^TestDocument(ResumesRuns|ResumedRunObservesCancellation|RunResyncsOnlyAtElementStarts|RecordsRunsInTextOrder)$").CombinedOutput()
			if err == nil {
				t.Fatalf("the run tests pass with the mutant %q:\n%s", m.name, out)
			}
			if !strings.Contains(string(out), "--- FAIL: TestDocument") {
				t.Fatalf("the mutant %q fails no run test:\n%s", m.name, out)
			}
		})
	}
}
