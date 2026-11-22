package libgo

import (
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// splicedMemo stands for the typed memo of a Document, which keeps the edit log while it holds one.
type splicedMemo struct{}

func (splicedMemo) splice([]documentEdit) {}

// A Document parse under a memory limit counts the history the Document keeps beside its memo table,
// the run records and the edit log, as the engine does (spec/runtime/documents.md): the bytes of an
// edit log, three ints for each edit it has room for, add to the count of a parse that reuses the
// memo; and a Document whose memo, runs and edit log alone pass the limit drops all of them, the
// typed memo that keeps the log included, as ResetHistory does, and parses from scratch.
func TestDocumentMemoryLimitCountsEditLog(t *testing.T) {
	main, _ := documentTestRules()
	text := strings.Repeat("a", 2000)
	opts := func(mem uint64) parseOptions {
		return parseOptions{maxDepth: defaultMaxDepth, limits: sxirruntime.Limits{Memory: mem}}
	}
	least := func(what string, ok func(mem uint64) bool) uint64 {
		t.Helper()
		lo, hi := uint64(0), uint64(1<<40)
		if !ok(hi) {
			t.Fatalf("%s: fails under 2^40", what)
		}
		for hi-lo > 1 {
			if mid := lo + (hi-lo)/2; ok(mid) {
				hi = mid
			} else {
				lo = mid
			}
		}
		return hi
	}
	// parsed returns a Document of text after a parse, whose edit log has room for room edits.
	parsed := func(room int) *documentState {
		d := testDocumentState(main, text, sxirinput.CodePoints)
		if _, err := d.parse(opts(0)); err != nil {
			t.Fatal(err)
		}
		d.edits = make([]documentEdit, 0, room)
		return d
	}
	const editBytes = 3 * 8
	if d := parsed(100); d.historyBytes() != runBytes(d.runs)+100*editBytes {
		t.Fatalf("history of a log with room for 100 edits: %d bytes, want %d and the runs' %d", d.historyBytes(), 100*editBytes, runBytes(d.runs))
	}

	// The count of a parse that reuses the memo grows by the bytes of the log.
	reuse := func(room int) uint64 {
		return least("a parse that reuses the memo", func(mem uint64) bool {
			d := parsed(room)
			_, err := d.parse(opts(mem))
			return err == nil && d.stats.Reused > 0
		})
	}
	if short, long := reuse(0), reuse(1000); long-short != 1000*editBytes {
		t.Fatalf("a parse that reuses the memo counts %d bytes with an empty log, %d with room for 1000 edits; want %d more", short, long, 1000*editBytes)
	}

	// A log that alone passes the limit is dropped with the memo, the runs and the typed memo.
	fresh := least("a first parse", func(mem uint64) bool {
		_, err := testDocumentState(main, text, sxirinput.CodePoints).parse(opts(mem))
		return err == nil
	})
	d := parsed(int(fresh/editBytes) + 1)
	d.typedMemo = splicedMemo{}
	if _, err := d.parse(opts(fresh)); err != nil {
		t.Fatalf("a Document whose log alone passes the limit of a first parse: %v", err)
	}
	if d.stats.Reused != 0 || cap(d.edits) != 0 || d.typedMemo != nil {
		t.Fatalf("the parse kept its history: stats %+v, room for %d edits, typed memo %v", d.stats, cap(d.edits), d.typedMemo)
	}
}
