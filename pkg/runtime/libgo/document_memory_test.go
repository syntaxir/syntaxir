package libgo

import (
	"errors"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
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

// A Document that both Node and typed parses use keeps both memos, and a parse of either form under a
// memory limit counts all it keeps once: the Node memo table and runs, the estimate of the typed memo
// and the edit log. Each part that grows by some bytes grows the count of a parse that reuses its
// memo by those bytes, whichever form parses; and the parse of either form resets the whole history
// when it alone passes the limit, the other form's memo included.
func TestDocumentMemoryLimitCountsBothMemos(t *testing.T) {
	nodeMain, _ := documentTestRules()
	typedMain := typedTestRules()
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
	parses := map[string]func(d *Document, mem uint64) error{
		"Node": func(d *Document, mem uint64) error {
			_, err := d.State.parse(opts(mem))
			return err
		},
		"typed": func(d *Document, mem uint64) error {
			_, err := TypedDocumentParse(d, typedMain, opts(mem), noChunks{}, func(v any) any { return v })
			return err
		},
	}
	newDoc := func() *Document {
		d := &Document{State: testDocumentState(nodeMain, text, sxirinput.CodePoints)}
		typedMain.Rule.g = nodeMain.g
		return d
	}
	// both returns a Document after a Node parse and a typed parse, with what grow adds to it.
	both := func(grow func(d *Document)) *Document {
		d := newDoc()
		for _, form := range []string{"Node", "typed"} {
			if err := parses[form](d, 0); err != nil {
				t.Fatalf("%s parse: %v", form, err)
			}
		}
		grow(d)
		return d
	}
	growths := []struct {
		name  string
		bytes uint64
		grow  func(d *Document)
	}{
		{"nothing", 0, func(*Document) {}},
		{"the Node memo table", 10_000, func(d *Document) { d.State.memo.memBytes += 10_000 }},
		{"the typed memo", 1000 * 8, func(d *Document) {
			c := d.State.typedMemo.(*typedDocumentState)
			c.slots = append(c.slots, make([]*tmemoEntry, 1000)...)
		}},
		{"the edit log", 1000 * 3 * 8, func(d *Document) { d.State.edits = make([]documentEdit, 0, 1000) }},
		{"the kept child stack", 1000 * 8, func(d *Document) { d.State.kids = make([]*sxirvalue.Node, 0, 1000) }},
	}
	d := both(func(*Document) {})
	want := d.State.memo.chargedBytes() + runBytes(d.State.runs) + d.State.typedMemo.(*typedDocumentState).retainedBytes() + d.State.kidsBytes()
	if got := d.State.retainedBytes(); got != want {
		t.Fatalf("a Document with both memos retains %d bytes, want the Node memo's, the runs', the typed memo's and the child stack's %d", got, want)
	}
	for _, form := range []string{"Node", "typed"} {
		reuse := func(grow func(d *Document)) uint64 {
			return least(form+" parse that reuses its memo", func(mem uint64) bool {
				d := both(grow)
				return parses[form](d, mem) == nil && d.State.stats.Reused > 0
			})
		}
		base := reuse(growths[0].grow)
		// The count is what the Document retains and what the parse takes into use besides; one
		// below it, the parse keeps the history and fails reporting the count.
		var le *sxirruntime.LimitError
		if kept := both(func(*Document) {}).State.retainedBytes(); base <= kept {
			t.Errorf("%s parse that reuses its memo counts %d bytes, no more than the %d the Document retains", form, base, kept)
		} else if err := parses[form](both(func(*Document) {}), base-1); !errors.As(err, &le) || le.Kind != sxirruntime.LimitMemory || le.Used != base {
			t.Errorf("%s parse under one less than its count %d: %v", form, base, err)
		}
		for _, g := range growths[1:] {
			if got := reuse(g.grow) - base; got != g.bytes {
				t.Errorf("%s parse: %d more bytes in %s count %d more, want %d", form, g.bytes, g.name, got, g.bytes)
			}
		}

		// What the Document keeps alone passes the limit of a first parse: the parse resets it all.
		fresh := least("a first "+form+" parse", func(mem uint64) bool { return parses[form](newDoc(), mem) == nil })
		other := map[string]int{"Node": 2, "typed": 1}[form] // the other form's memo
		d := both(func(d *Document) {
			for range fresh / growths[other].bytes {
				growths[other].grow(d)
			}
			growths[other].grow(d)
		})
		if err := parses[form](d, fresh); err != nil {
			t.Fatalf("%s parse of a Document whose other memo alone passes the limit of a first parse: %v", form, err)
		}
		nodeSlots, typed := len(d.State.memo.slots), d.State.typedMemo
		if form == "Node" {
			nodeSlots = 0
		} else {
			typed = nil
		}
		if d.State.stats.Reused != 0 || nodeSlots != 0 || typed != nil {
			t.Fatalf("%s parse kept the history: stats %+v, %d Node memo slots, typed memo %v", form, d.State.stats, nodeSlots, typed)
		}
	}
}
