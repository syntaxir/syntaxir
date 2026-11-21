package engine

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/document"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// documentVersionForCollection returns the version token of a document that nothing else refers to
// when it returns, and sends on finalized when the document is finalized. A non-nil keep receives the
// document, which makes it reachable for as long as the caller holds it.
//
//go:noinline
func documentVersionForCollection(t *testing.T, finalized chan<- struct{}, keep func(*document.Document)) DocumentVersion {
	t.Helper()
	p := mustCompile(t, `rule main = .*`)
	d, err := p.NewDocument("main", strings.Repeat("x", 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetFinalizer(d, func(*document.Document) { finalized <- struct{}{} })
	if keep != nil {
		keep(d)
	}
	return d.Version()
}

// collectionRounds is how many collections a finalizer gets to run in. The wait counts collections, not
// seconds: a finalizer runs on a goroutine after the collection that finds its object unreachable, so
// the count does not depend on the load of the machine, and an object that stays reachable is never
// finalized however many rounds there are.
const collectionRounds = 400

// finalizedWithinRounds runs collections until a finalizer has sent on finalized or the rounds are
// used, and reports whether it was sent.
func finalizedWithinRounds(finalized <-chan struct{}) bool {
	for range collectionRounds {
		runtime.GC()
		select {
		case <-finalized:
			return true
		default:
			runtime.Gosched()
		}
	}
	return false
}

func TestDocumentVersionDoesNotRetainDocument(t *testing.T) {
	t.Parallel()
	finalized := make(chan struct{}, 1)
	version := documentVersionForCollection(t, finalized, nil)
	defer runtime.KeepAlive(version)
	if !finalizedWithinRounds(finalized) {
		t.Fatal("version token retained its document")
	}
	if version.Sequence() != 1 {
		t.Fatal("retained token changed")
	}
}

// TestDocumentCollectionWaitSeesRetention is the negative control of
// TestDocumentVersionDoesNotRetainDocument: a document that is still reachable is not finalized, and
// the wait must report that, so the wait cannot pass without the collection it is meant to see.
func TestDocumentCollectionWaitSeesRetention(t *testing.T) {
	t.Parallel()
	finalized := make(chan struct{}, 1)
	var held *document.Document
	version := documentVersionForCollection(t, finalized, func(d *document.Document) { held = d })
	if finalizedWithinRounds(finalized) {
		t.Fatal("a reachable document was finalized")
	}
	runtime.KeepAlive(held)
	runtime.KeepAlive(version)
}

func batchOracle(text string, unit sxirinput.PositionUnit, edits []sxirinput.TextEdit) string {
	if unit == sxirinput.Bytes {
		var out strings.Builder
		pos := 0
		for _, e := range edits {
			out.WriteString(text[pos:e.Start])
			out.WriteString(e.Text)
			pos = e.End
		}
		out.WriteString(text[pos:])
		return out.String()
	}
	input := []rune(text)
	out := []rune{}
	pos := 0
	for _, e := range edits {
		out = append(out, input[pos:e.Start]...)
		out = append(out, []rune(e.Text)...)
		pos = e.End
	}
	out = append(out, input[pos:]...)
	return string(out)
}

func TestDocumentBatchFreshParity(t *testing.T) {
	t.Parallel()
	replacements := []string{"", "a", "\n", "é", "😀", "\xc3", "\xa9", "\xff", "@name\n", "x = 1+2\n"}
	for gi, grammar := range []string{`rule main = item* $$
rule item = .`, codegentest.IncrementalGrammar, `rule main = ("a" @error("need a") #recover(skip=.))* $$`} {
		original := mustCompile(t, grammar)
		programs := []*Program{original}
		loaded := saveLoad(t, original, "main", CompiledDocument)
		programs = append(programs, loaded)
		for pi, p := range programs {
			for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if pi == 1 && back == Closure {
					_, err := p.NewDocumentWith("main", "", ParseOptions{Backend: back})
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					for _, large := range []bool{false, true} {
						t.Run(fmt.Sprintf("%d/%d/%s/%s/%v", gi, pi, back, unit, large), func(t *testing.T) {
							initial := "é\xc3x\xa9\n"
							if large {
								initial += strings.Repeat("a", state.DocumentPieceMinSize)
							}
							d, err := p.NewDocumentWith("main", initial, ParseOptions{Backend: back, Unit: unit})
							if err != nil {
								t.Fatal(err)
							}
							rng := rand.New(rand.NewSource(39))
							for i := 0; i < 60; i++ {
								// Include an ordinary edit not yet parsed when the batch is captured.
								if i%3 == 0 {
									if err := d.Edit(0, 0, "a"); err != nil {
										t.Fatal(err)
									}
								}
								before := d.Text()
								positions := decodedBoundaries(before)
								if unit == sxirinput.CodePoints {
									for j := range positions {
										positions[j] = j
									}
								}
								var edits []sxirinput.TextEdit
								at := 0
								for j := 0; j < 3 && at < len(positions); j++ {
									start := at + rng.Intn(len(positions)-at)
									end := start + rng.Intn(min(3, len(positions)-start))
									edits = append(edits, sxirinput.TextEdit{Start: positions[start], End: positions[end], Text: replacements[rng.Intn(len(replacements))]})
									at = end + 1
								}
								wantText := batchOracle(before, unit, edits)
								base := d.Version()
								if err := d.EditBatch(base, edits...); err != nil {
									t.Fatal(err)
								}
								if d.Text() != wantText || d.Version().Sequence() != base.Sequence()+1 {
									t.Fatal("original-coordinate text/version mismatch")
								}
								got, err := d.Parse()
								// A document-mode program parses whole texts only when traced.
								want, werr := p.ParseWith("main", wantText, servedBy(CompiledDocument, ParseOptions{Backend: back, Unit: unit}))
								if dump(t, got, err) != dump(t, want, werr) {
									t.Fatalf("batch%d: %q got%s want%s", i, wantText, dump(t, got, err), dump(t, want, werr))
								}
							}
						})
					}
				}
			}
		}
	}
}
