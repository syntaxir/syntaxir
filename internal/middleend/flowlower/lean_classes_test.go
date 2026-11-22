package flowlower

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// leanClasses returns a value-free rule whose first alternative begins with two classes and is guarded
// by the first, the forms that the class passes rewrite.
func leanClasses(t *testing.T) (*core.Module, *core.Rule) {
	t.Helper()
	class := func(lo, hi rune) *core.ClassNode {
		return &core.ClassNode{Class: &core.Class{Ranges: []core.Range{{Lo: lo, Hi: hi}}}}
	}
	m, r := valueFree(alts(&core.Seq{Items: []core.Node{class('a', 'z'), class('0', '9')}}, &core.Lit{Text: "q"}))
	if _, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Of(corepass.GuardsPass), m); err != nil {
		t.Fatal(err)
	}
	return m, r
}

// lean is the Go target with the passes that record nothing of the input they read.
var lean = flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true}

// leanFinish runs the lean passes on f, with class-tables at one range so that every class is a
// candidate, ends them, and returns the listing.
func leanFinish(t *testing.T, f *code.Module) string {
	t.Helper()
	settings := with(t, with(t, pass.Settings{}, flowpass.PassClassTables, 1), flowpass.PassInlineLiterals, 0)
	f, _, err := flowpass.Run(f, flowpass.RunOptions{Settings: settings, Target: lean, VerifyEach: true})
	if err != nil {
		t.Fatal(err)
	}
	return dump(finishFlow(t, f))
}

// TestLeanClassesSkipDocument checks that the class passes, which record nothing of the input they
// read, leave a function lowered for Documents as the lowering wrote it, and rewrite the same function
// lowered without them.
func TestLeanClassesSkipDocument(t *testing.T) {
	for _, document := range []bool{true, false} {
		m, r := leanClasses(t)
		plain, err := lowerFinished(t, m, r, lowerdata.Options{Document: document}, 0)
		if err != nil {
			t.Fatal(err)
		}
		m, r = leanClasses(t)
		f, err := Lower(m, r, "r", lowerdata.Options{Document: document})
		if err != nil {
			t.Fatal(err)
		}
		got := leanFinish(t, f)
		if same := got == dump(plain); same != document {
			t.Errorf("document %v: the passes rewrote it %v, want %v:\n%s", document, !same, !document, got)
		}
	}
}

// TestLeanClassesSkipDocumentFrame is the same check for the framed lowering.
func TestLeanClassesSkipDocumentFrame(t *testing.T) {
	for _, document := range []bool{true, false} {
		m, r := leanClasses(t)
		plain, err := LowerFrame(m, r, r.Body, nil, false, "r", lowerdata.Options{Document: document})
		if err != nil {
			t.Fatal(err)
		}
		finish(t, plain, flowpass.Target{}, 0)
		m, r = leanClasses(t)
		f, err := LowerFrame(m, r, r.Body, nil, false, "r", lowerdata.Options{Document: document})
		if err != nil {
			t.Fatal(err)
		}
		got := leanFinish(t, f)
		if same := got == dump(plain); same != document {
			t.Errorf("document %v: the passes rewrote it %v, want %v:\n%s", document, !same, !document, got)
		}
	}
}
