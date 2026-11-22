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

// typedTestRules returns the typed rules main = item* $$ and item = "a", which a typed Document
// memoizes.
func typedTestRules() *TRule {
	item := &TRule{Rule: &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}}
	item.Body = func(p *TParser, _ int) (any, bool) {
		return p.MatchLiteral([]rune("a"), "a", IdAny, true)
	}
	main := &TRule{Rule: &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}}
	main.Body = func(p *TParser, _ int) (any, bool) {
		var items []any
		for {
			v, ok := p.Call(item, 0)
			if !ok {
				break
			}
			items = append(items, v)
		}
		return items, p.AtEnd()
	}
	return main
}

// noChunks stands for the allocation chunks of a generated typed parser, which the test does not have.
type noChunks struct{}

func (noChunks) SetBudgetControl(*BudgetControl) {}

// A typed Document parse under a memory limit counts the edit log once beside the estimate of its
// typed memo, and a Document whose typed memo and edit log alone pass the limit drops its whole
// history, as ResetHistory does, the Node memo that keeps the log included.
func TestTypedDocumentMemoryLimitCountsEditLog(t *testing.T) {
	main := typedTestRules()
	text := strings.Repeat("a", 2000)
	opts := func(mem uint64) parseOptions {
		return parseOptions{maxDepth: defaultMaxDepth, limits: sxirruntime.Limits{Memory: mem}}
	}
	parse := func(d *Document, mem uint64) error {
		_, err := TypedDocumentParse(d, main, opts(mem), noChunks{}, func(v any) any { return v })
		return err
	}
	newDoc := func() *Document {
		return &Document{State: testDocumentState(main.Rule, text, sxirinput.CodePoints)}
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
	// parsed returns a Document of text after a typed parse, whose edit log has room for room edits.
	parsed := func(room int) *Document {
		d := newDoc()
		if err := parse(d, 0); err != nil {
			t.Fatal(err)
		}
		d.State.edits = make([]documentEdit, 0, room)
		return d
	}
	const editBytes = 3 * 8

	// The count of a typed parse that reuses the memo grows by the bytes of the log, once.
	reuse := func(room int) uint64 {
		return least("a typed parse that reuses the memo", func(mem uint64) bool {
			d := parsed(room)
			return parse(d, mem) == nil && d.State.stats.Reused > 0
		})
	}
	if short, long := reuse(0), reuse(1000); long-short != 1000*editBytes {
		t.Fatalf("a typed parse that reuses the memo counts %d bytes with an empty log, %d with room for 1000 edits; want %d more", short, long, 1000*editBytes)
	}

	// A log that alone passes the limit is dropped with the typed memo and the Node memo.
	fresh := least("a first typed parse", func(mem uint64) bool { return parse(newDoc(), mem) == nil })
	d := parsed(int(fresh/editBytes) + 1)
	// A Node memo with a slot, which keeps the log when only the typed memo is dropped.
	d.State.memo.slots = make([]*memoEntry, 1)
	if err := parse(d, fresh); err != nil {
		t.Fatalf("a Document whose log alone passes the limit of a first typed parse: %v", err)
	}
	if d.State.stats.Reused != 0 || cap(d.State.edits) != 0 || len(d.State.memo.slots) != 0 {
		t.Fatalf("the typed parse kept the history: stats %+v, room for %d edits, %d Node memo slots", d.State.stats, cap(d.State.edits), len(d.State.memo.slots))
	}
}
