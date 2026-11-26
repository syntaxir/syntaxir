package libgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// runTestRules returns the rule main = item* $$, whose repetition resumes its run in a Document parse
// as a generated rule body does (flow.resumeStart and the other resume operations), with item = [ab] ";".
// started, if not nil, is called when the body of main starts, after the parse has prepared its input.
func runTestRules(started func()) *Rule {
	return runTestRulesOf(started, []string{"a;", "b;"}, -1)
}

// runTestRulesOf returns the rule main = item{0,max} item* $$ (main = item* $$ for max -1), whose first
// repetition resumes its run in a Document parse and whose second does not, with item the ordered choice
// of the literals alts.
func runTestRulesOf(started func(), alts []string, max int) *Rule {
	item := &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}
	item.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		start := p.Pos
		for _, alt := range alts {
			if _, ok := p.MatchLiteral([]rune(alt), alt, IdAny, true); ok {
				return nodeOfData(&Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos)}), true
			}
		}
		return nil, false
	}
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		if started != nil {
			started()
		}
		start := p.Pos
		var children []*sxirvalue.Node
		on := p.ResumeOn()
		var r *RunState
		if on {
			r = p.ResumeStart(1, 0, max, true)
		}
		for max < 0 || len(children) < max {
			if on {
				if !p.ResumeMore(r) {
					break
				}
				if v, ok := p.ResumeNext(r); ok {
					children = append(children, v)
					continue
				}
			}
			m := p.Mark()
			if on {
				p.ResumeBegin(r)
			}
			v, ok := p.Call(item, 0)
			if on {
				p.ResumeEnd(r, ok, v)
			}
			if !ok {
				p.Reset(m)
				break
			}
			children = append(children, v)
		}
		if on {
			p.ResumeFinish(r)
		}
		for max >= 0 {
			m := p.Mark()
			v, ok := p.Call(item, 0)
			if !ok {
				p.Reset(m)
				break
			}
			children = append(children, v)
		}
		if !p.AtEnd() {
			return nil, false
		}
		return nodeOfData(&Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: children}), true
	}
	return main
}

// parseAsFromScratch parses the Document d after an edit and requires the tree of a parse of its text
// from scratch with the rule main.
func parseAsFromScratch(t *testing.T, d *documentState, main *Rule, o parseOptions) *sxirvalue.Node {
	t.Helper()
	got, err := d.parse(o)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := testDocumentState(main, d.currentText(), sxirinput.CodePoints).parse(o)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(fresh)
	if string(a) != string(b) {
		t.Fatalf("Document %s, parse from scratch %s", a, b)
	}
	return got
}

// A Document reuses the elements of a repetition before an edit and, moved, those after it, and
// returns the tree of a parse from scratch; Stats counts the resumed elements apart from the memo hits.
func TestDocumentResumesRuns(t *testing.T) {
	main := runTestRules(nil)
	text := strings.Repeat("a;", 40)
	d := testDocumentState(main, text, sxirinput.CodePoints)
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	for _, ed := range []struct {
		start, end int
		text       string
	}{{40, 40, "b;"}, {40, 42, ""}, {10, 12, "b;b;"}, {0, 0, "a;"}} {
		if err := d.edit(ed.start, ed.end, ed.text); err != nil {
			t.Fatal(err)
		}
		got, err := d.parse(o)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := testDocumentState(main, d.currentText(), sxirinput.CodePoints).parse(o)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(fresh)
		if string(a) != string(b) {
			t.Fatalf("edit %+v: Document %s, parse from scratch %s", ed, a, b)
		}
		s := d.stats
		if s.Resumed < 30 {
			t.Fatalf("edit %+v: resumed %d elements of %d (%+v)", ed, s.Resumed, len(D(got).Children), s)
		}
		if st := (&Document{State: d}).Stats(); st.ResumedElements != s.Resumed || st.MemoHits != s.Reused-s.Resumed {
			t.Fatalf("edit %+v: Stats %+v of %+v", ed, st, s)
		}
	}
}

// A run resumes after an edit only at an old element that starts where its next element starts, moved by
// the edit: with item = "ab" / "a" / "b", the b inserted into the element ab at 10 makes the elements ab
// and b, whose ends do not all fall on the starts of old elements.
func TestDocumentRunResyncsOnlyAtElementStarts(t *testing.T) {
	main := runTestRulesOf(nil, []string{"ab", "a", "b"}, -1)
	d := testDocumentState(main, strings.Repeat("ab", 40), sxirinput.CodePoints)
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	if err := d.edit(11, 11, "b"); err != nil {
		t.Fatal(err)
	}
	got := parseAsFromScratch(t, d, main, o)
	if s := d.stats; s.Resumed < 30 {
		t.Fatalf("resumed %d elements of %d (%+v)", s.Resumed, len(D(got).Children), s)
	}
}

// A run that resumes the old elements after an edit and then evaluates new ones after them records them in
// the order of the text: with at most 50 elements in the resumed repetition, deleting ten elements of 60
// leaves room for ten new elements after the old run, and the next parse reuses the recorded run.
func TestDocumentRecordsRunsInTextOrder(t *testing.T) {
	main := runTestRulesOf(nil, []string{"a;", "b;"}, 50)
	d := testDocumentState(main, strings.Repeat("a;", 60), sxirinput.CodePoints)
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	if err := d.edit(20, 40, ""); err != nil {
		t.Fatal(err)
	}
	parseAsFromScratch(t, d, main, o)
	if s := d.stats; s.Resumed < 35 {
		t.Fatalf("after the deletion: resumed %d elements (%+v)", s.Resumed, s)
	}
	if err := d.edit(98, 100, "b;"); err != nil {
		t.Fatal(err)
	}
	parseAsFromScratch(t, d, main, o)
	if s := d.stats; s.Resumed < 45 {
		t.Fatalf("after the change at the end: resumed %d elements (%+v)", s.Resumed, s)
	}
}

// countingContext counts the calls of Err once armed, and reports a cancellation from the counted
// call cancelAt on, if it is not zero.
type countingContext struct {
	context.Context
	armed           bool
	calls, cancelAt int
}

func (c *countingContext) Done() <-chan struct{} { return make(chan struct{}) }

func (c *countingContext) Err() error {
	if !c.armed {
		return nil
	}
	c.calls++
	if c.cancelAt != 0 && c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

// A resumed run counts no unit, but each reused element is a step at which the parse observes its
// context (budget.poll): a cancellation ends a parse that only reuses the prefix of a long run, within
// an interval of elements, and not only at the checks at the start and the end of the parse, although
// the reused elements do not advance the counter.
func TestDocumentResumedRunObservesCancellation(t *testing.T) {
	// The preparation of the input observes the context too: count only the observations of the
	// parse of main.
	var ctx *countingContext
	main := runTestRules(func() {
		if ctx != nil {
			ctx.armed = true
		}
	})
	n := 20 * int(budgetCheckInterval)
	d := testDocumentState(main, strings.Repeat("a;", n), sxirinput.CodePoints)
	if _, err := d.parse(parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}); err != nil {
		t.Fatal(err)
	}
	if err := d.edit(2*n, 2*n, "b;"); err != nil {
		t.Fatal(err)
	}
	// A parse that reuses the prefix polls the context about once each interval of reused elements.
	ctx = &countingContext{Context: context.Background()}
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth, context: ctx, controls: true}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	if d.stats.Resumed < n-1 {
		t.Fatalf("resumed %d of %d elements", d.stats.Resumed, n)
	}
	if ctx.calls < n/int(budgetCheckInterval) {
		t.Fatalf("the context was observed %d times over %d resumed elements", ctx.calls, d.stats.Resumed)
	}
	// The same parse with a context that is canceled at its fourth observation in main ends with the
	// cancellation, before the end of the reused prefix.
	if err := d.edit(2*n, 2*n+2, ""); err != nil {
		t.Fatal(err)
	}
	ctx = &countingContext{Context: context.Background(), cancelAt: 4}
	o.context = ctx
	if _, err := d.parse(o); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled parse = %v", err)
	}
	if ctx.calls != 4 {
		t.Fatalf("the parse went on after the cancellation: %d observations", ctx.calls)
	}
}
