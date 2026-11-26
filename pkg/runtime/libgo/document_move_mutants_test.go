package libgo_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// moveMutant is a goMutant of a function of the head (its receiver).
type moveMutant struct{ name, file, head, fn, old, new string }

// moveMutants change the moves of reused values of a Document (document.go) in one way each: the
// generation stamps that tell which edits the positions of a node account for, and the copies and
// replayed shifts that a move keeps only while it lasts.
var moveMutants = []moveMutant{
	{"a moved node keeps its old generation", "document.go", "func (p *Parser) ", "moveTree", "D(n).Mark = p.gen\n", ""},
	{"a moved entry keeps the generation of its nodes", "document.go", "func (p *Parser) ", "documentMove", "e.ngen, e.shift, e.shifted = p.gen, 0, false", "e.shift, e.shifted = 0, false"},
	{"a node moved through another value moves by the value's shift", "document.go", "func (p *Parser) ", "shiftOf", "if D(n).Mark <= p.moveBase {", "if true {"},
	{"the copies of empty nodes outlive their move", "document.go", "func (p *Parser) ", "endMove", "clear(p.moved)\n", ""},
	{"the replayed shifts outlive their move", "document.go", "func (p *Parser) ", "endMove", "clear(p.shifts)\n", ""},
}

// TestMutantsOfDocumentMoves is the proof that the move tests of document_test.go and
// document_step_positions_test.go hold the moves of the Go library: with the source of each mutant,
// one of them fails.
func TestMutantsOfDocumentMoves(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	for _, m := range moveMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			overlay := mutatedLib(t, m.file, m.head, m.fn, m.old, m.new)
			// -vet=off: the verdict reads what the tests report, and vet would only analyze the mutated code again.
			out, err := goTestLib(overlay, "-count=1", "-vet=off", "-run", "^(TestDocumentMove.*|TestDocumentLimit.*EqualsFresh|TestDocumentStepResultsHaveCurrentPositions|TestPrivateDocument.*)$").CombinedOutput()
			if err == nil {
				t.Fatalf("the move tests pass with the mutant %q:\n%s", m.name, out)
			}
			if !strings.Contains(string(out), "--- FAIL: Test") {
				t.Fatalf("the mutant %q fails no move test:\n%s", m.name, out)
			}
		})
	}
}
