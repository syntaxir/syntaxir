package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// The doc operations of the conformance suite on the TypeScript library: the examined input of the
// calls of a Document parse and of an observed parse, and the statistics of a Document. Each case runs
// on the library's parser of its parse (DocumentParser, ObservedParser or the batch Parser), with the
// operations as the printer prints them in that class. The cases and the randomized sequences are
// those that the engine and the Go library run.

func tsDocSuite() conform.Suite {
	return conform.Suite{Name: "typescript doc", Ops: tsconformtest.DocOps}
}

// tsDocRun runs the handwritten cases of the doc operations and seeds randomized cases of n calls, with
// the configurations of the Go library's suite: a Document parse in two seeds of three, an observed
// parse without a Document in one of six, and a parse from scratch in the rest.
func tsDocRun(r tsconformtest.Runner, seeds, n int) tsConformResult {
	s := tsDocSuite()
	return tsConformRunCases(r, tsconformtest.Doc(), s, conform.DocCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 66))
		return conform.RandomDoc(fmt.Sprint("random_", seed), rng, s.Ops, n, ref.Config{Document: seed%3 != 2, Observed: seed%6 == 2})
	})
}

func TestTypeScriptConformanceDoc(t *testing.T) {
	node := tooltest.RequireNode(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsConformLib(t))}
	res := tsDocRun(runner, seeds, 150)
	if res.ran < seeds+len(conform.DocCases()) {
		t.Fatalf("only %d cases ran (%d skipped, %d pending)", res.ran, res.skipped, res.pending)
	}
	tsReport(t, res)
}

// tsDocDetect requires the doc suite on the session's library to fail.
func tsDocDetect(t *testing.T, runner tsconformtest.Runner) {
	t.Helper()
	res := tsDocRun(runner, 100, 150)
	for _, err := range res.fails {
		op, kind := tsFailureKind(err)
		t.Logf("detected at %s (%s):\n%v", op, kind, strings.SplitN(err.Error(), "\ncase:", 2)[0])
		return
	}
	t.Fatalf("the suite did not notice: %d cases ran, %d failed", res.ran, len(res.fails))
}

// TestTypeScriptConformanceDocDetectsMutants is the negative control of TestTypeScriptConformanceDoc for
// the library's methods that the printed doc operations call: with one changed, the suite fails.
func TestTypeScriptConformanceDocDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := tooltest.RequireNode(t)
	mutants := []tsMutant{
		{name: "reading moves the examined end back", sig: "override documentTouch(end: number)", old: "if (end > this.docExamined) this.docExamined = end;", new: "this.docExamined = end;"},
		{name: "examining before the start moves the start forward", sig: "override documentTouchFrom(from: number)", old: "if (from < this.docLow) this.docLow = from;", new: "this.docLow = from;"},
		{name: "an observed call keeps the examined end of the enclosing call", sig: "docExaminedBegin(): number", old: "this.docExamined = this.pos;", new: ""},
		{name: "an observed call does not merge its examined end", sig: "docExaminedEnd(enclosing: number)", old: "Math.max(enclosing, examined)", new: "enclosing"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			tsDocDetect(t, tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsMutate(t, m))})
		})
	}
}

// TestTypeScriptConformanceDocPrintedDetectsMutants is the negative control of the code that the printer
// prints for the doc operations in the DocumentParser and the ObservedParser: the suite runs that code, so a
// change of it must fail.
func TestTypeScriptConformanceDocPrintedDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	doc, obs := "PrintedDocumentParser", "PrintedObservedParser"
	mutants := []tsPrintedMutant{
		{name: "a call keeps the examined end of the enclosing call", class: doc, method: "op_doc_enter", old: "this.docExamined = p0;", new: ""},
		{name: "a call keeps the start of the enclosing call", class: doc, method: "op_doc_enter", old: "this.docLow = p0;", new: ""},
		{name: "leaving a call drops its examined end", class: doc, method: "op_doc_leave", old: "Math.max(p0, this.docExamined)", new: "p0"},
		{name: "leaving a call keeps its start", class: doc, method: "op_doc_leave", old: "this.docLow = p1;", new: ""},
		{name: "a Document parse reads no Document", class: doc, method: "op_doc_active", old: "true", new: "false"},
		{name: "a Document parse does not track", class: doc, method: "op_doc_tracking", old: "true", new: "false"},
		{name: "the start is the examined end", class: doc, method: "op_doc_low", old: "this.docLow", new: "this.docExamined"},
		{name: "a reuse counts as an evaluation", class: doc, method: "op_doc_reused", old: "this.reused++", new: "this.evaluated++"},
		{name: "an evaluation is not counted", class: doc, method: "op_doc_evaluated", old: "this.evaluated++", new: "this.evaluated"},
		{name: "an observed call of a Document parse ends without merging", class: doc, method: "op_doc_examinedEnd", old: "this.docExaminedEnd(p0)", new: "this.docExamined"},
		{name: "an observed parse reads a Document", class: obs, method: "op_doc_active", old: "false", new: "true"},
		{name: "an observed parse does not track", class: obs, method: "op_doc_tracking", old: "true", new: "false"},
		{name: "an observed parse reads no examined end", class: obs, method: "op_doc_examined", old: "this.docExamined", new: "0"},
	}
	tsDetectPrinted(t, mutants, func(r tsconformtest.Runner, _ bool) tsConformResult { return tsDocRun(r, 100, 150) })
}
