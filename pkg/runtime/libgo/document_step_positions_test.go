package libgo

import (
	"fmt"
	"testing"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// stepPositionRules returns main = a $$ over the left-recursive cycle of the leaders a and b:
//
//	a = b "+" num / num
//	b = a "-" num / num
//	num = "1"+
//
// whose growth of b at the start of a grows in the context of a's, so that its steps are kept and
// compared (canonicalSeed). num is memoized by Documents and returns its text; check runs on every
// result that a body of a or b returns, which is the step result that the comparison reads.
func stepPositionRules(check func(p *Parser, n *sxirvalue.Node), numEvals *int) *Rule {
	num := &Rule{Id: 3, Name: "num", Memo: true, DocMemo: true, Seen: -1}
	num.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		*numEvals++
		start := p.Pos
		for {
			m := p.Mark()
			if _, ok := p.MatchLiteral([]rune("1"), "1", IdAny, true); !ok {
				p.Reset(m)
				break
			}
		}
		if p.Pos == start {
			return nil, false
		}
		text := string(p.In[start:p.Pos])
		return p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: text, Terminal: true}), true
	}
	a := &Rule{Id: 1, Name: "a", Memo: true, Leader: true, Rec: true, Cycle: 1, DocMemo: true, Seen: -1}
	b := &Rule{Id: 2, Name: "b", Memo: true, Leader: true, Rec: true, Cycle: 1, DocMemo: true, Seen: -1}
	alt := func(other *Rule, op string) func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		return func(p *Parser, _ int) (*sxirvalue.Node, bool) {
			start := p.Pos
			m := p.Mark()
			if l, ok := p.Call(other, 0); ok {
				if _, ok := p.MatchLiteral([]rune(op), op, IdAny, true); ok {
					if r, ok := p.Call(num, 0); ok {
						n := p.buildNode(Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Children: []*sxirvalue.Node{l, r}})
						check(p, n)
						return n, true
					}
				}
			}
			p.Reset(m)
			r, ok := p.Call(num, 0)
			if !ok {
				return nil, false
			}
			n := p.buildNode(Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Children: []*sxirvalue.Node{r}})
			check(p, n)
			return n, true
		}
	}
	a.Body = alt(b, "+")
	b.Body = alt(a, "-")
	numberLeaders([]*Rule{a, b})
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		v, ok := p.Call(a, 0)
		if !ok || !p.AtEnd() {
			return nil, false
		}
		return v, true
	}
	return main
}

// stalePosition returns a description of the first node of n whose positions do not hold for the
// current text: the text of a num at its range, and the range of a sequence from its first child's start
// to its last child's end; "" if there is none.
func stalePosition(text []rune, n *sxirvalue.Node) string {
	d := D(n)
	if d.Start < 0 || d.End < d.Start || int(d.End) > len(text) {
		return fmt.Sprintf("%v %d-%d outside the text", n.Kind(), d.Start, d.End)
	}
	if d.Terminal {
		if got := string(text[d.Start:d.End]); got != d.Text {
			return fmt.Sprintf("%q at %d-%d reads %q", d.Text, d.Start, d.End, got)
		}
		return ""
	}
	if first, last := D(d.Children[0]), D(d.Children[len(d.Children)-1]); first.Start != d.Start || last.End != d.End {
		return fmt.Sprintf("sequence %d-%d over children %d-%d", d.Start, d.End, first.Start, last.End)
	}
	for _, c := range d.Children {
		if s := stalePosition(text, c); s != "" {
			return s
		}
	}
	return ""
}

// TestDocumentStepResultsHaveCurrentPositions checks that the results of the steps of a growth in a
// context, which canonicalSeed compares, hold positions of the current text in a Document parse after an
// edit before them, when they hold reused results that the edit moved: a Document moves a reused value
// (documentMove) before a step can hold it, so that comparing Start and End compares current positions.
func TestDocumentStepResultsHaveCurrentPositions(t *testing.T) {
	var (
		stale    []string
		ctxSteps int
		numEvals int
	)
	check := func(p *Parser, n *sxirvalue.Node) {
		if g := len(p.gamma); g >= 2 && p.gamma[g-2].pos == p.gamma[g-1].pos {
			ctxSteps++
		}
		if s := stalePosition(p.In, n); s != "" {
			stale = append(stale, s)
		}
	}
	main := stepPositionRules(check, &numEvals)
	text := "1+11-1+111-1+1"
	d := testDocumentState(main, text, sxirinput.CodePoints)
	o := parseOptions{unit: sxirinput.CodePoints, maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 || ctxSteps == 0 {
		t.Fatalf("first parse: stale positions %q, %d steps in a context", stale, ctxSteps)
	}
	if err := d.edit(0, 1, "111"); err != nil {
		t.Fatal(err)
	}
	ctxSteps, numEvals = 0, 0
	reused := d.stats.Reused
	got, err := d.parse(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("edited parse: step results with stale positions: %q", stale)
	}
	if ctxSteps == 0 {
		t.Fatal("edited parse: no step of a growth in a context")
	}
	// The nums after the edit come from the memo, moved; only the edited one is evaluated again.
	if d.stats.Reused == reused || numEvals != 1 {
		t.Fatalf("edited parse: %d reuses, %d evaluations of num; want the nums after the edit reused",
			d.stats.Reused-reused, numEvals)
	}
	fresh, err := testDocumentState(main, d.currentText(), sxirinput.CodePoints).parse(o)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := documentNodeString(got), documentNodeString(fresh); a != b {
		t.Fatalf("Document %s, parse from scratch %s", a, b)
	}
	if D(got).End != int32(len(d.currentText())) {
		t.Fatalf("edited parse ends at %d, want %d", D(got).End, len(d.currentText()))
	}
}
