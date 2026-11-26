package libgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

func documentTestRules() (*Rule, *Rule) {
	item := &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}
	item.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		return p.MatchLiteral([]rune("a"), "a", IdAny, true)
	}
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		start := p.Pos
		var children []*sxirvalue.Node
		for {
			v, ok := p.Call(item, 0)
			if !ok {
				break
			}
			children = append(children, v)
		}
		if !p.AtEnd() {
			return nil, false
		}
		return p.buildNode(Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: children}), true
	}
	return main, item
}

func TestPrivateDocumentPrefixReuseAndFreshResult(t *testing.T) {
	main, _ := documentTestRules()
	for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		d := testDocumentState(main, "aa", unit)
		o := parseOptions{unit: unit, maxDepth: defaultMaxDepth}
		first, err := d.parse(o)
		if err != nil || first == nil || len(D(first).Children) != 2 {
			t.Fatalf("unit %d: first parse = %v, %v", unit, first, err)
		}
		if err := d.edit(2, 2, "a"); err != nil {
			t.Fatal(err)
		}
		second, err := d.parse(o)
		if err != nil || second == nil || len(D(second).Children) != 3 || d.stats.Reused < 1 {
			t.Fatalf("unit %d: edited parse = %v, %v; stats = %+v", unit, second, err, d.stats)
		}
		if D(first).Children[0] != D(second).Children[0] {
			t.Fatalf("unit %d: safe prefix memo was not reused", unit)
		}
		if err := d.edit(0, 1, "b"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.parse(o); err == nil {
			t.Fatalf("unit %d: changed prefix unexpectedly parsed", unit)
		}
	}
}

// TestPrivateDocumentExaminedSuffixAndEOF checks that an entry whose evaluation examined the slot
// after its end is dropped by an edit of that slot, after which the slot is the end of the text
// ("Reuse Model" in spec/runtime/documents.md: the examined range covers every slot examined).
func TestPrivateDocumentExaminedSuffixAndEOF(t *testing.T) {
	item := &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}
	item.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		start := p.Pos
		v, ok := p.MatchLiteral([]rune("a"), "a", IdAny, true)
		if !ok {
			return nil, false
		}
		_, _, ok = p.Peek() // lookahead that extends beyond the consumed text
		p.Pos = start + 1
		return v, ok
	}
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) { return p.Call(item, 0) }
	d := testDocumentState(main, "ab", sxirinput.CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err == nil { // suffix remains after the rule
		t.Fatal("expected whole-input failure")
	}
	if e := d.memo.get(item.Id, 0, 0, nil); e == nil || e.examined <= 1 {
		t.Fatalf("lookahead dependency was not recorded: %+v", e)
	}
	if err := d.edit(1, 2, ""); err != nil {
		t.Fatal(err)
	}
	if e := liveEntry(d, item.Id, 0); e != nil {
		t.Fatal("lookahead-dependent memo survived edit")
	}
	if _, err := d.parse(o); err == nil {
		t.Fatal("EOF made the lookahead fail; parse must not reuse the old result")
	}
}

// liveEntry returns the node entry of rule at pos that a lookup of the next parse would find: an
// edit leaves an entry in its slot, and the lookup applies the edits it does not account for yet.
func liveEntry(d *documentState, rule, pos int) *memoEntry {
	e := d.memo.get(rule, pos, 0, nil)
	if e == nil || !advanceEntry(e, d.edits) {
		return nil
	}
	return e
}

func TestPrivateDocumentEditBoundariesAndUnit(t *testing.T) {
	main, _ := documentTestRules()
	d := testDocumentState(main, "é", sxirinput.Bytes)
	if err := d.edit(1, 1, "x"); err == nil || d.currentText() != "é" {
		t.Fatalf("interior UTF-8 edit = %v, text = %q", err, d.currentText())
	}
	if err := d.edit(0, 0, "\xff"); err != nil || !strings.HasPrefix(d.currentText(), "\xff") {
		t.Fatalf("invalid byte insertion = %v, text = %q", err, d.currentText())
	}
	if _, err := d.parse(parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("unit change = %v", err)
	}
	if err := d.edit(-1, 0, "x"); err == nil {
		t.Fatal("invalid range was accepted")
	}
}

func TestPrivateDocumentInterruptionDiscardsMemo(t *testing.T) {
	main, _ := documentTestRules()
	d := testDocumentState(main, "aa", sxirinput.CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.parse(parseOptions{maxDepth: defaultMaxDepth, context: ctx}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-execution interruption = %v", err)
	}
	if len(d.memo.slots) == 0 {
		t.Fatal("pre-execution interruption discarded complete memo")
	}
	if err := d.edit(2, 2, "a"); err != nil {
		t.Fatal(err)
	}
	main.Body = func(*Parser, int) (*sxirvalue.Node, bool) { panic(fatal{err: sxirruntime.ErrLimit}) }
	if _, err := d.parse(o); !errors.Is(err, sxirruntime.ErrLimit) {
		t.Fatalf("interruption = %v", err)
	}
	if len(d.memo.slots) != 0 {
		t.Fatalf("execution interruption retained %d memo slots", len(d.memo.slots))
	}
}

func TestDocumentReturnedRecoveryErrorOwnership(t *testing.T) {
	r := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	r.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		p.Pos = 1
		p.PushRecovered(&sxirvalue.SyntaxError{Line: 1, Col: 1, Expected: []string{"original"}, Messages: []string{"original"}})
		return p.buildNode(Data{Kind: valueimpl.ErrorKind, End: 1}), true
	}
	d := testDocumentState(r, "x", sxirinput.CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	_, first := d.parse(o)
	e, ok := first.(sxirvalue.SyntaxErrors)
	if !ok || len(e) != 1 {
		t.Fatal(first)
	}
	e[0].Messages[0] = "mutated"
	e[0].Expected[0] = "mutated"
	_, second := d.parse(o)
	next, ok := second.(sxirvalue.SyntaxErrors)
	if !ok || len(next) != 1 {
		t.Fatal(second)
	}
	if next[0].Messages[0] != "original" || next[0].Expected[0] != "original" {
		t.Fatalf("caller mutation changed cached diagnostics: %+v", next[0])
	}
}

// The copy that publishes the diagnostics of a reused result is Document work, which no Compute
// limit counts: a limit too small for the copy does not interrupt it, and the memo stays. The
// cancellation of the copy is TestDocumentDiagnosticPublicationCancellation.
func TestDocumentDiagnosticPublicationBudget(t *testing.T) {
	calls := 0
	r := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	r.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		calls++
		p.Pos = 1
		expected := make([]string, 2048)
		for i := range expected {
			expected[i] = "original"
		}
		p.PushRecovered(&sxirvalue.SyntaxError{Line: 1, Col: 1, Expected: expected, Messages: []string{"original"}})
		return p.buildNode(Data{Kind: valueimpl.ErrorKind, End: 1}), true
	}
	d := testDocumentState(r, "x", sxirinput.CodePoints)
	normal := parseOptions{maxDepth: defaultMaxDepth, controls: true}
	if _, err := d.parse(normal); err == nil {
		t.Fatal("missing recovered diagnostic")
	}
	if calls != 1 {
		t.Fatalf("initial evaluations: %d", calls)
	}
	limited := normal
	limited.limits = sxirruntime.Limits{Compute: 64}
	if _, err := d.parse(limited); err == nil || errors.Is(err, sxirruntime.ErrLimit) {
		t.Fatalf("the copy of the diagnostics met the limit: %v", err)
	}
	if calls != 1 {
		t.Fatal("warm parse should reuse memo before publication")
	}
	if len(d.memo.slots) == 0 {
		t.Fatal("the published parse dropped its memo")
	}
}

type diagnosticCancelContext struct {
	context.Context
	done  chan struct{}
	polls int
}

func (c *diagnosticCancelContext) Done() <-chan struct{} { return c.done }
func (c *diagnosticCancelContext) Err() error {
	c.polls++
	if c.polls == 4 {
		close(c.done)
	}
	if c.polls >= 4 {
		return context.Canceled
	}
	return nil
}

func TestDocumentDiagnosticPublicationCancellation(t *testing.T) {
	r := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	r.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		p.Pos = 1
		p.PushRecovered(&sxirvalue.SyntaxError{Expected: make([]string, 2048)})
		return p.buildNode(Data{Kind: valueimpl.ErrorKind, End: 1}), true
	}
	d := testDocumentState(r, "x", sxirinput.CodePoints)
	normal := parseOptions{maxDepth: defaultMaxDepth, controls: true}
	if _, err := d.parse(normal); err == nil {
		t.Fatal("missing recovered diagnostic")
	}
	ctx := &diagnosticCancelContext{Context: context.Background(), done: make(chan struct{})}
	o := normal
	o.context = ctx
	converted := false
	out, err := DocumentParseConverted(d, o, func(n *sxirvalue.Node, _ *BudgetControl, _ error) *sxirvalue.Node {
		converted = true
		return n
	})
	if out != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("diagnostic cancellation: %v %v", out, err)
	}
	if converted {
		t.Fatal("diagnostic copying did not poll cancellation before conversion")
	}
	if len(d.memo.slots) != 0 {
		t.Fatal("cancelled publication retained memo")
	}
}

// defaultMaxDepth is the rule-call limit of the test grammars.
const defaultMaxDepth = 100_000

// testDocumentState gives root a grammar with the fixed expectations and
// returns a document state for it.
func testDocumentState(root *Rule, text string, unit sxirinput.PositionUnit) *documentState {
	g := NewGrammar(defaultMaxDepth)
	g.Descs = []string{"any", "begin input", "end input", "begin line", "end line"}
	root.g = g
	return newDocumentState(root, text, unit)
}

// TestDocumentLimitInsideCallThenParseEqualsFresh interrupts a Document parse by its compute limit at
// every event of an edited parse, inside the rule calls included, and checks that the next parse of
// the Document gives what a parse of a fresh Document gives.
func TestDocumentLimitInsideCallThenParseEqualsFresh(t *testing.T) {
	text := "aaaaaaaa"
	edited := text[:4] + "a" + text[4:]
	o := parseOptions{maxDepth: defaultMaxDepth, controls: true}
	// The rules count a unit at each evaluation, as a generated parser counts its events, so that a
	// limit stops the parse inside a call.
	rules := func() *Rule {
		main, item := documentTestRules()
		for _, r := range []*Rule{main, item} {
			body := r.Body
			r.Body = func(p *Parser, min int) (*sxirvalue.Node, bool) {
				p.unitsSized(1)
				return body(p, min)
			}
		}
		return main
	}
	fresh := func() *sxirvalue.Node {
		main := rules()
		n, err := testDocumentState(main, edited, sxirinput.CodePoints).parse(o)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	want := documentNodeString(fresh())
	interrupted := 0
	for budget := uint64(1); ; budget++ {
		main := rules()
		d := testDocumentState(main, text, sxirinput.CodePoints)
		if _, err := d.parse(o); err != nil {
			t.Fatal(err)
		}
		if err := d.edit(4, 4, "a"); err != nil {
			t.Fatal(err)
		}
		limited := o
		limited.limits = sxirruntime.Limits{Compute: budget}
		if _, err := d.parse(limited); err == nil {
			break
		} else if !errors.Is(err, sxirruntime.ErrLimit) {
			t.Fatalf("budget %d: %v", budget, err)
		}
		interrupted++
		got, err := d.parse(o)
		if err != nil {
			t.Fatalf("budget %d: the parse after the interruption: %v", budget, err)
		}
		if s := documentNodeString(got); s != want {
			t.Fatalf("budget %d: the parse after the interruption gives\n%s\na fresh Document\n%s", budget, s, want)
		}
	}
	if interrupted < len(edited) {
		t.Fatalf("%d budgets interrupted the parse, want at least one per rule call", interrupted)
	}
}

// documentNodeString lists the kinds and spans of a tree.
func documentNodeString(n *sxirvalue.Node) string {
	if n == nil {
		return "nil"
	}
	s := fmt.Sprintf("(%v %d-%d", n.Kind(), D(n).Start, D(n).End)
	for _, c := range D(n).Children {
		s += " " + documentNodeString(c)
	}
	return s + ")"
}

// TestCallDocumentRestoresOnPanic checks that a Document call restores the enclosing call's
// examined end when its rule panics, memoized and not: the enclosing call keeps the farther end,
// which here is its own.
func TestCallDocumentRestoresOnPanic(t *testing.T) {
	for _, memo := range []bool{true, false} {
		r := &Rule{Id: 0, Name: "main", Memo: memo, DocMemo: memo, Seen: -1}
		r.Body = func(*Parser, int) (*sxirvalue.Node, bool) { panic(fatal{err: sxirruntime.ErrLimit}) }
		d := testDocumentState(r, "abc", sxirinput.CodePoints)
		p := &Parser{unit: d.unit, MaxDepth: defaultMaxDepth}
		p.setGrammar(d.root.g)
		p.setSource(d.currentText())
		p.Document, p.memo = d, d.memo
		p.Pos, p.docExamined = 1, 3
		func() {
			defer func() {
				if x := recover(); x == nil {
					t.Fatalf("memo=%v: the call did not panic", memo)
				}
			}()
			p.callDocument(r, 0)
		}()
		if p.docExamined != 3 {
			t.Errorf("memo=%v: the panic left the examined end %d, want 3", memo, p.docExamined)
		}
	}
}

// TestDocumentMoveReplaysEditsOfSharedNodes checks that a move after edits whose shifts cancel out
// still moves a value whose nodes another value moved in between: the entry records that edits shifted
// it, and the shared node, which the other move left at the positions of a later text, replays the
// edits since.
func TestDocumentMoveReplaysEditsOfSharedNodes(t *testing.T) {
	shared := nodeOfData(&Data{Kind: valueimpl.MatchKind, Start: 5, End: 6})
	first := nodeOfData(&Data{Kind: valueimpl.ListKind, Start: 5, End: 6, Children: []*sxirvalue.Node{shared}})
	second := nodeOfData(&Data{Kind: valueimpl.ListKind, Start: 5, End: 6, Children: []*sxirvalue.Node{shared}})
	// Two insertions before the values: the first adds two positions, the second removes them.
	log := []documentEdit{{start: 0, end: 0, delta: 2}, {start: 0, end: 2, delta: -2}}
	a := &memoEntry{node: first, shift: 2, shifted: true}
	p := &Parser{Document: &documentState{}, edits: log[:1], gen: 1}
	p.documentMove(a)
	if D(first).Start != 7 || D(shared).Start != 7 || D(shared).Mark != 1 || a.shifted || a.ngen != 1 {
		t.Fatalf("after the first edit: value at %d, shared node at %d of generation %d", D(first).Start, D(shared).Start, D(shared).Mark)
	}
	b := &memoEntry{node: second, shift: 0, shifted: true}
	p = &Parser{Document: &documentState{}, edits: log, gen: 2}
	p.documentMove(b)
	if D(second).Start != 5 || D(shared).Start != 5 || D(shared).End != 6 || D(second).Mark != 2 || D(shared).Mark != 2 {
		t.Fatalf("after the second edit: value at %d, shared node at [%d,%d)", D(second).Start, D(shared).Start, D(shared).End)
	}
}

// TestDocumentMoveGenerationsRestartWithTheLog checks that a Document whose edit log is full parses
// without its memo and numbers the generations of its nodes from 0 again, and that the parses after
// that reuse and move values to the positions of a parse from scratch.
func TestDocumentMoveGenerationsRestartWithTheLog(t *testing.T) {
	defer func(n int) { maxDocumentEdits = n }(maxDocumentEdits)
	maxDocumentEdits = 2
	main, _ := documentTestRules()
	d := testDocumentState(main, "aa", sxirinput.CodePoints)
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}
	for i := range 6 {
		if i > 0 {
			if err := d.edit(0, 0, "a"); err != nil {
				t.Fatal(err)
			}
		}
		root, err := d.parse(o)
		if err != nil {
			t.Fatalf("parse %d: %v", i, err)
		}
		// The edit at i == 3 finds the log full: that parse starts from scratch.
		if reused := d.stats.Reused != 0; reused != (i > 0 && i != 3) {
			t.Fatalf("parse %d: %d reused, edit log of %d", i, d.stats.Reused, len(d.edits))
		}
		for j, c := range D(root).Children {
			if D(c).Start != int32(j) || D(c).End != int32(j+1) {
				t.Fatalf("parse %d: child %d at [%d,%d)", i, j, D(c).Start, D(c).End)
			}
		}
	}
}

// TestDocumentMoveMovesEveryValue checks that a move moves a value of nodes and basic values, and one
// whose node holds a host value alike, a node that the value holds twice once, and the entry's
// generation with it.
func TestDocumentMoveMovesEveryValue(t *testing.T) {
	leaf := func(box any) *sxirvalue.Node {
		return nodeOfData(&Data{Kind: valueimpl.MatchKind, Start: 1, End: 2, Box: box})
	}
	// A root of three children, two distinct leaves of which one is held twice, and a field.
	build := func(box any) (root, a *sxirvalue.Node) {
		a, b := leaf(nil), leaf(box)
		return nodeOfData(&Data{Kind: valueimpl.ListKind, Start: 1, End: 2, Children: []*sxirvalue.Node{a, b, a}, Fields: valueimpl.Fields{{Name: "x", Value: "v"}}}), a
	}
	for _, c := range []struct {
		name string
		box  any
	}{
		{"basic", nil},
		{"host value", []int{1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &Parser{Document: &documentState{}, edits: []documentEdit{{start: 0, end: 0, delta: 1}}, gen: 1}
			root, a := build(c.box)
			e := &memoEntry{node: root, shift: 1, shifted: true}
			if !p.documentMove(e) {
				t.Fatal("the move refused the value")
			}
			if e.shift != 0 || e.shifted || e.ngen != 1 || e.node != root {
				t.Fatalf("entry shift %d, shifted %v, generation %d", e.shift, e.shifted, e.ngen)
			}
			for i, n := range []*sxirvalue.Node{root, a, D(root).Children[1]} {
				if D(n).Start != 2 || D(n).End != 3 || D(n).Mark != 1 {
					t.Fatalf("node %d at [%d,%d) of generation %d, want [2,3) of 1", i, D(n).Start, D(n).End, D(n).Mark)
				}
			}
		})
	}
}

// TestDocumentMoveKeepsSharedNodesShared checks that a reused result that holds a node twice, as a
// child and as a field, holds it once moved, not two copies, and that the move is in place: the
// result of the parse before the edit holds the same nodes.
func TestDocumentMoveKeepsSharedNodesShared(t *testing.T) {
	item := &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}
	item.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		return p.MatchLiteral([]rune("a"), "a", IdAny, true)
	}
	pair := &Rule{Id: 2, Name: "pair", Memo: true, DocMemo: true, Seen: -1}
	pair.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		start := p.Pos
		v, ok := p.Call(item, 0)
		if !ok {
			return nil, false
		}
		return p.buildNode(Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: []*sxirvalue.Node{v}, Fields: valueimpl.Fields{{Name: "x", Value: v}}}), true
	}
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		start := p.Pos
		for {
			if _, ok := p.MatchLiteral([]rune("b"), "b", IdAny, true); !ok {
				break
			}
		}
		v, ok := p.Call(pair, 0)
		if !ok || !p.AtEnd() {
			return nil, false
		}
		return p.buildNode(Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: []*sxirvalue.Node{v}}), true
	}
	d := testDocumentState(main, "ba", sxirinput.CodePoints)
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}
	first, err := d.parse(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.edit(0, 0, "b"); err != nil {
		t.Fatal(err)
	}
	second, err := d.parse(o)
	if err != nil || d.stats.Reused == 0 {
		t.Fatalf("the pair is not reused: err %v, stats %+v", err, d.stats)
	}
	pairOf := func(root *sxirvalue.Node) (*sxirvalue.Node, *sxirvalue.Node, *sxirvalue.Node) {
		pr := D(root).Children[0]
		return pr, D(pr).Children[0], D(pr).Fields[0].Value.(*sxirvalue.Node)
	}
	pr, child, field := pairOf(second)
	if child != field {
		t.Fatal("the moved pair holds two copies of the node that it held once")
	}
	if D(pr).Start != 2 || D(child).Start != 2 || D(child).End != 3 {
		t.Fatalf("moved pair [%d,%d), node [%d,%d); want [2,3), [2,3)", D(pr).Start, D(pr).End, D(child).Start, D(child).End)
	}
	// The pair was moved in place: the result of the parse before the edit holds it, at the positions
	// of the current text (spec/runtime/documents.md allows that; Clone keeps a tree as it was).
	if pr0, child0, field0 := pairOf(first); pr0 != pr || child0 != field0 || child0 != child {
		t.Fatal("the reused pair is not the pair of the parse before the edit")
	}
}

// TestDocumentLimitAfterMovesThenEditsEqualsFresh interrupts by its compute limit, at every event, a
// Document parse after an edit before the whole text, which moves the reused results in place
// (documentMove) as it reaches them, and checks that the parse after the interruption and the parses
// after two more edits give what parses of fresh Documents of the same texts give: the moves that the
// interrupted parse made leave the kept trees at positions that the next parses account for.
func TestDocumentLimitAfterMovesThenEditsEqualsFresh(t *testing.T) {
	o := parseOptions{maxDepth: defaultMaxDepth, controls: true}
	rules := func() *Rule {
		main, item := documentTestRules()
		for _, r := range []*Rule{main, item} {
			body := r.Body
			r.Body = func(p *Parser, min int) (*sxirvalue.Node, bool) {
				p.unitsSized(1)
				return body(p, min)
			}
		}
		return main
	}
	fresh := func(text string) string {
		n, err := testDocumentState(rules(), text, sxirinput.CodePoints).parse(o)
		if err != nil {
			t.Fatal(err)
		}
		return documentNodeString(n)
	}
	type edit struct {
		start, end int
		text       string
	}
	// The first edit moves every reused item; the later ones delete before and insert after items
	// that the interrupted parse moved.
	edits := []edit{{0, 0, "a"}, {2, 4, ""}, {5, 5, "aa"}}
	text := "aaaaaaaa"
	var want []string
	for _, e := range edits {
		text = text[:e.start] + e.text + text[e.end:]
		want = append(want, fresh(text))
	}
	interrupted, movedBefore := 0, 0
	for budget := uint64(1); ; budget++ {
		d := testDocumentState(rules(), "aaaaaaaa", sxirinput.CodePoints)
		first, err := d.parse(o)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.edit(edits[0].start, edits[0].end, edits[0].text); err != nil {
			t.Fatal(err)
		}
		limited := o
		limited.limits = sxirruntime.Limits{Compute: budget}
		if _, err := d.parse(limited); err == nil {
			break
		} else if !errors.Is(err, sxirruntime.ErrLimit) {
			t.Fatalf("budget %d: %v", budget, err)
		}
		interrupted++
		// The items of the first parse are the reused results; one at a new position was moved before
		// the limit stopped the parse.
		for j, c := range D(first).Children {
			if D(c).Start != int32(j) {
				movedBefore++
				break
			}
		}
		for i, e := range edits {
			if i > 0 {
				if err := d.edit(e.start, e.end, e.text); err != nil {
					t.Fatal(err)
				}
			}
			got, err := d.parse(o)
			if err != nil {
				t.Fatalf("budget %d, edit %d: %v", budget, i, err)
			}
			if s := documentNodeString(got); s != want[i] {
				t.Fatalf("budget %d, edit %d: the Document gives\n%s\na fresh Document\n%s", budget, i, s, want[i])
			}
		}
	}
	if interrupted == 0 || movedBefore == 0 {
		t.Fatalf("%d budgets interrupted the parse, %d of them after a move; want both", interrupted, movedBefore)
	}
}

// TestDocumentMoveCopiesEmptyNodesPerMove checks that the copies of empty nodes that a move makes
// belong to that move: an empty node that two values hold, moved by different shifts, is copied for
// each at its own positions, and the node itself stays as it was.
func TestDocumentMoveCopiesEmptyNodesPerMove(t *testing.T) {
	empty := nodeOfData(&Data{Kind: valueimpl.MatchKind, Start: 3, End: 3})
	a := nodeOfData(&Data{Kind: valueimpl.ListKind, Start: 3, End: 3, Children: []*sxirvalue.Node{empty}})
	b := nodeOfData(&Data{Kind: valueimpl.ListKind, Start: 3, End: 3, Children: []*sxirvalue.Node{empty}})
	p := &Parser{Document: &documentState{}, edits: []documentEdit{{start: 0, end: 0, delta: 2}}, gen: 1}
	ea := &memoEntry{node: a, shift: 1, shifted: true}
	eb := &memoEntry{node: b, shift: 2, shifted: true}
	p.documentMove(ea)
	p.documentMove(eb)
	for _, c := range []struct {
		e     *memoEntry
		shift int32
	}{{ea, 1}, {eb, 2}} {
		k := D(c.e.node).Children[0]
		if D(c.e.node).Start != 3+c.shift || k == empty || D(k).Start != 3+c.shift || D(k).End != 3+c.shift {
			t.Fatalf("shift %d: value at %d, its empty child at [%d,%d)", c.shift, D(c.e.node).Start, D(k).Start, D(k).End)
		}
	}
	if D(empty).Start != 3 || D(a).Start != 3 || D(b).Start != 3 {
		t.Fatal("a move modified an empty node")
	}
}

// TestDocumentMoveReplaysShiftsPerMove checks that the shifts that a move replays for the nodes of a
// generation belong to that move: nodes of one generation, moved since through other values, lie on
// either side of a later edit, and the values that hold them, moved one after the other, move each
// by its own replayed shift.
func TestDocumentMoveReplaysShiftsPerMove(t *testing.T) {
	before := nodeOfData(&Data{Kind: valueimpl.MatchKind, Start: 10, End: 11, Mark: 1})
	after := nodeOfData(&Data{Kind: valueimpl.MatchKind, Start: 100, End: 101, Mark: 1})
	// The nodes hold the positions of generation 1; the entries were last current at generation 0,
	// so the moves replay the edits since 1: one at 50 that adds 5 positions.
	log := []documentEdit{{start: 0, end: 0, delta: 0}, {start: 50, end: 50, delta: 5}}
	p := &Parser{Document: &documentState{}, edits: log, gen: 2}
	ea := &memoEntry{node: before, shifted: true}
	eb := &memoEntry{node: after, shifted: true}
	p.documentMove(ea)
	p.documentMove(eb)
	if D(before).Start != 10 || D(after).Start != 105 || D(after).End != 106 {
		t.Fatalf("the nodes before and after the edit are at %d and [%d,%d), want 10 and [105,106)", D(before).Start, D(after).Start, D(after).End)
	}
}
