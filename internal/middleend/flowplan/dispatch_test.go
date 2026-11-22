package flowplan_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// dispatchGrammar has guards whose skips pass over no call (the literals of x and u), over calls of
// memoized rules (inner, called by x, item and outer at one position), and, with guardPick, a guard
// that core.GuardCalls does not describe.
const dispatchGrammar = `@start
rule main = (x / u / item / z)* $$
rule x = inner ";" / "e"
rule u = outer "!" / outer "?" / "c"
rule item = inner "." / "b"
rule z = pick ";" / "w"
rule pick = "m1" / "m2"
rule outer = inner "-"
rule inner = "a" tail
rule tail = [a-z]*
`

// guardPick gives the alternative pick ";" of z a guard by hand. Its path enters a choice, where
// core.GuardCalls does not describe the calls, and the Core pass guards gives no such guard (the
// alternative has no First): the dispatch is still defined for it, and keeps it only where the skip
// needs to make nothing.
func guardPick(t *testing.T, m *core.Module) {
	t.Helper()
	for _, r := range m.Rules {
		if r != nil && r.Name == "z" {
			if c, ok := r.Body.(*core.Choice); ok && c.Alts[0].Guard == nil {
				c.Alts[0].Guard = &core.First{Lit: true, Rune: 'm'}
				return
			}
		}
	}
	t.Fatal("rule z has no unguarded first alternative")
}

// guardedAlts returns every alternative of m that has a guard (core.Alt.Guard).
func guardedAlts(m *core.Module) []*core.Alt {
	var out []*core.Alt
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		core.Walk(r.Body, func(n core.Node) {
			if c, ok := n.(*core.Choice); ok {
				for _, a := range c.Alts {
					if a.Guard != nil {
						out = append(out, a)
					}
				}
			}
		})
	}
	return out
}

// The pass dispatch keeps every guard where the skip makes nothing that the parse observes, makes the
// calls that core.GuardCalls describes under logical units, keeps only the guards whose skip makes
// none of them under lowerdata.GuardsEvent and every guard, with no calls, under
// lowerdata.GuardsKeep, and leaves a guard over a call of a memoized rule to the parses that are not
// a Document's.
func TestGuardedFollowsTheOptions(t *testing.T) {
	ir, _ := tableIR(t, dispatchGrammar, check.BuildValues)
	m := ir.M
	guardPick(t, m)
	all := guardedAlts(m)
	if len(all) == 0 {
		t.Fatal("the grammar has no guard")
	}
	described := 0
	withCalls := 0
	for _, a := range all {
		calls, ok := core.GuardCalls(m, a.Body)
		if ok {
			described++
			if len(calls) > 0 {
				withCalls++
			}
		}
	}
	if described == len(all) || withCalls == 0 {
		t.Fatalf("the grammar needs a guard that GuardCalls does not describe and one whose skip makes calls (%d of %d described, %d with calls)", described, len(all), withCalls)
	}
	for _, tc := range []struct {
		name string
		o    lowerdata.Options
		keep func(calls []int, described bool) bool
	}{
		{"ordinary", lowerdata.Options{}, func([]int, bool) bool { return true }},
		{"keep", lowerdata.Options{Logical: true, Guards: lowerdata.GuardsKeep}, func([]int, bool) bool { return true }},
		{"exact", lowerdata.Options{Logical: true}, func(_ []int, d bool) bool { return d }},
		{"event", lowerdata.Options{Logical: true, Guards: lowerdata.GuardsEvent}, func(c []int, d bool) bool { return d && len(c) == 0 }},
		{"document", lowerdata.Options{Document: true}, func(_ []int, d bool) bool { return d }},
		{"logical document", lowerdata.Options{Logical: true, Document: true}, func(_ []int, d bool) bool { return d }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := flowplan.Guarded(m, tc.o)
			outsides := 0
			for _, a := range all {
				calls, described := core.GuardCalls(m, a.Body)
				g, kept := got[a]
				if want := tc.keep(calls, described); kept != want {
					t.Errorf("guard %v: kept %v, want %v", a.Guard, kept, want)
				}
				if !kept {
					continue
				}
				wantCalls := []int(nil)
				if tc.o.Logical && tc.o.Guards != lowerdata.GuardsKeep {
					wantCalls = calls
				}
				if !slices.Equal(g.Calls, wantCalls) {
					t.Errorf("guard %v: skip makes calls %v, want %v", a.Guard, g.Calls, wantCalls)
				}
				outside := false
				if tc.o.Document && described {
					for _, k := range calls {
						outside = outside || m.Rules[k].Policy.Memo
					}
				}
				if g.Outside != outside {
					t.Errorf("guard %v: outside %v, want %v", a.Guard, g.Outside, outside)
				}
				if outside {
					outsides++
				}
			}
			if tc.o.Document && outsides == 0 {
				t.Error("no guard passes over a call of a memoized rule")
			}
			if len(got) > len(all) {
				t.Errorf("%d guarded alternatives for %d guards", len(got), len(all))
			}
		})
	}
}

// The pass dispatch sets the guarded alternatives of each table once for the units that share it,
// leaves the tables it is given as they are, and names the guarded alternatives in the dump of its
// result.
func TestDispatchingSetsTheGuardsOfEachTable(t *testing.T) {
	ir, _ := tableIR(t, dispatchGrammar, check.BuildValues)
	ir.Opts.Logical, ir.Opts.Guarded = true, nil
	c := &plandata.Cores{Units: []plandata.Unit{
		{Label: "rules", Table: "rules", IR: ir},
		{Label: "again", Table: "rules", IR: ir},
	}}
	got, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(flowplan.Dispatching), c)
	if err != nil {
		t.Fatal(err)
	}
	if ir.Opts.Guarded != nil || c.Units[0].IR != ir {
		t.Errorf("dispatch changed the tables it was given")
	}
	out := got.Units[0].IR
	if out == ir || len(out.Opts.Guarded) == 0 {
		t.Fatalf("dispatch kept no guard")
	}
	if got.Units[1].IR != out {
		t.Errorf("the units of one table do not share its table")
	}
	var b strings.Builder
	plandata.CoresIR.Print(&b, got)
	dump := b.String()
	for _, want := range []string{"unit rules table=rules\n", "    guard ", " calls=", "unit again table=rules\n"} {
		if !strings.Contains(dump, want) {
			t.Errorf("the dump has no %q:\n%s", want, dump)
		}
	}
}
