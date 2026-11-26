//go:build sxir_long

package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

var (
	engineTextCommitRun    = engineObservationRun{"./internal/runtime/state", "TestStreamTextCutAtCommit", ""}
	generatedTextCommitRun = engineObservationRun{"./internal/backend/codegen/golang/print", "TestGeneratedStreamTextCutAtCommit", ""}
	engineTextUnitsRun     = engineObservationRun{"./internal/runtime/state", "TestStreamTextUnits", ""}
)

// streamTextCommitMutants cut a text in a stream at the base the discard left instead of the last
// commit point, drop the cut, or count the units of its whole range instead of its cut part, in the
// engine and in the library of generated Go parsers. No grammar can tell them: the checker rejects
// every read after the #stream repetition of a text that the cut would shorten, so each text that a
// stream takes lies after the last commit point, where the cut and the base change nothing. The
// tests of the texts of a stream pass with each of them, which pins that the cut stays unobservable.
// The TypeScript library has its mutants in the stream oracle of the TypeScript backend
// (TestStreamOracleMutants).
var streamTextCommitMutants = []engineObservationMutant{
	{name: "engine text cut at the base", file: "internal/runtime/state/input.go",
		old: "start, end = max(start, in.TextFloor), max(end, in.TextFloor)", new: "start, end = max(start, in.Base), max(end, in.Base)",
		runs: []engineObservationRun{engineTextCommitRun, generatedTextCommitRun}},
	{name: "engine text not cut at the commit point", file: "internal/runtime/state/stream_exec.go",
		old: "\tp.TextFloor = pos\n", new: "",
		runs: []engineObservationRun{engineTextCommitRun, engineTextUnitsRun, generatedTextCommitRun}},
	{name: "engine text counts its whole range", file: "internal/runtime/state/budget_input.go",
		old: "p.countUnits(max(end, p.TextFloor) - max(start, p.TextFloor))", new: "p.countUnits(end - start)",
		runs: []engineObservationRun{engineTextUnitsRun, generatedTextCommitRun}},
	{name: "generated text cut at the base", file: "pkg/runtime/libgo/stream.go",
		old: "start, end = max(start, s.textFloor), max(end, s.textFloor)", new: "start, end = max(start, s.base), max(end, s.base)",
		runs: []engineObservationRun{generatedTextCommitRun}},
	{name: "generated text counts its whole range", file: "pkg/runtime/libgo/stream.go",
		old: "p.unitsSized(max(end, f) - max(start, f))", new: "p.unitsSized(end - start + 0*f)",
		runs: []engineObservationRun{generatedTextCommitRun}},
	// The control changes nothing the tests observe, so that a failure of the others is their change.
	{name: "control", file: "pkg/runtime/libgo/stream.go",
		old: "\ts := p.Stream\n\tstart, end = max(start, s.textFloor)", new: "\ts := p.Stream\n\t_ = s\n\tstart, end = max(start, s.textFloor)",
		runs: []engineObservationRun{engineTextCommitRun, engineTextUnitsRun, generatedTextCommitRun}},
}

// TestStreamTextCommitMutants checks that the tests of the texts of a stream pass with an engine or a
// generated Go parser that cuts a text at the base of the buffer, which depends on the chunks, does
// not cut it, or counts the units of the whole range: no grammar takes a text the cut shortens.
func TestStreamTextCommitMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the engine again for each mutant")
	}
	runSourceMutants(t, streamTextCommitMutants)
}
