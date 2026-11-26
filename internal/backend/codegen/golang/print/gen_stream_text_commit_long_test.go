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
	// The checks of a text of a node that begins before the last commit point, in a module made to
	// take one and in the library of generated Go parsers.
	engineTextBeforeCommitRun = engineObservationRun{"./internal/runtime/engine", "TestCompiledStreamTextBeforeCommitFails", "begins before the commit point"}
	libTextBeforeCommitRun    = engineObservationRun{"./pkg/runtime/libgo", "TestStreamRangeTextBeforeCommitFails", "begins before the commit point"}
)

// streamTextCommitMutants change how a stream treats its last commit point, in the engine and in the
// library of generated Go parsers. The first ones cut a host read of the input at the base the discard
// left instead of the commit point, or drop the commit point. No grammar can tell them: the checker
// rejects every read after the #stream repetition of a text that begins before the commit point, so
// each text that a stream takes lies after it, where the cut and the base change nothing. The tests
// of the texts of a stream pass with each of them, which pins that the commit point stays
// unobservable. The others take the text of a node that begins before the commit point instead of
// ending the parse, and the checks of such a text fail with them. The TypeScript library has its
// mutants in the stream oracle of the TypeScript backend (TestStreamOracleMutants) and in
// TestStreamRangeTextBeforeCommitFails there.
var streamTextCommitMutants = []engineObservationMutant{
	{name: "engine text cut at the base", file: "internal/runtime/state/input.go",
		old: "start, end = max(start, in.TextFloor), max(end, in.TextFloor)", new: "start, end = max(start, in.Base), max(end, in.Base)",
		runs: []engineObservationRun{engineTextCommitRun, generatedTextCommitRun}},
	{name: "engine text not cut at the commit point", file: "internal/runtime/state/stream_exec.go",
		old: "\tp.TextFloor = pos\n", new: "",
		runs: []engineObservationRun{engineTextCommitRun, engineTextUnitsRun, generatedTextCommitRun}},
	{name: "engine text of a node cut at the commit point", file: "internal/runtime/state/budget_input.go",
		old: "\t\tif start < p.TextFloor {\n\t\t\tp.Fail(", new: "\t\tif false {\n\t\t\tp.Fail(",
		runs: []engineObservationRun{engineTextBeforeCommitRun}},
	{name: "generated text cut at the base", file: "pkg/runtime/libgo/stream.go",
		old: "start, end = max(start, s.textFloor), max(end, s.textFloor)", new: "start, end = max(start, s.base), max(end, s.base)",
		runs: []engineObservationRun{generatedTextCommitRun}},
	{name: "generated text of a node cut at the commit point", file: "pkg/runtime/libgo/stream.go",
		old: "\t\tif start < s.textFloor {\n\t\t\tpanic(", new: "\t\tif false {\n\t\t\tpanic(",
		runs: []engineObservationRun{libTextBeforeCommitRun}},
	// The control changes nothing the tests observe, so that a failure of the others is their change.
	{name: "control", file: "pkg/runtime/libgo/stream.go",
		old: "\ts := p.Stream\n\tstart, end = max(start, s.textFloor)", new: "\ts := p.Stream\n\t_ = s\n\tstart, end = max(start, s.textFloor)",
		runs: []engineObservationRun{engineTextCommitRun, engineTextUnitsRun, generatedTextCommitRun,
			{libTextBeforeCommitRun.pkg, libTextBeforeCommitRun.test, ""}}},
}

// TestStreamTextCommitMutants checks that the tests of the texts of a stream pass with an engine or a
// generated Go parser that cuts a host read at the base of the buffer, which depends on the chunks, or
// drops the commit point, since no grammar takes a text before it, and that they fail with one that
// takes the text of a node that begins before it.
func TestStreamTextCommitMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the engine again for each mutant")
	}
	runSourceMutants(t, streamTextCommitMutants)
}
