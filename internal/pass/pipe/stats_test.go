package pipe

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

// tally counts declared counters in the reverse of their alphabetical order, so that a report in
// map or name order would differ from the declaration order.
var tally = New(Def[*mod, *mod]{Decl: Decl{Name: "t-tally", Test: true, Counters: []string{"zeta", "alpha"}}, In: modIR, Out: modIR,
	Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
		c.Count("alpha", len(m.fns))
		if m.fns[0].name == "a1" {
			c.Count("zeta", 1)
			return m, pass.PreserveAll(), nil
		}
		return m, pass.PreserveNone(), nil
	}})

// sloppy counts a counter that it did not declare.
var sloppy = New(Def[*mod, *mod]{Decl: Decl{Name: "t-sloppy", Test: true, Counters: []string{"declared"}}, In: modIR, Out: modIR,
	Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
		c.Count("undeclared", 1)
		return m, pass.PreserveAll(), nil
	}})

func TestCountPanicsOnAnUndeclaredCounter(t *testing.T) {
	defer func() {
		if x := recover(); x == nil || !strings.Contains(fmt.Sprint(x), "undeclared counter undeclared") {
			t.Errorf("recovered %v, want the panic of an undeclared counter", x)
		}
	}()
	run(t, &Run{}, Seq(modIR, sloppy), &mod{fns: []*fn{{"f", "x"}}})
}

func TestNewRejectsCountersThatTheManagerKeepsOrThatRepeat(t *testing.T) {
	for i, counters := range [][]string{{CounterRuns}, {CounterChanged}, {"a", "a"}, {""}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("counters %q were accepted", counters)
				}
			}()
			New(Def[*mod, *mod]{Decl: Decl{Name: fmt.Sprintf("t-bad-counters-%d", i), Test: true, Counters: counters}, In: modIR, Out: modIR,
				Run: func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveAll(), nil }})
		}()
	}
}

// A required pass always runs, so New rejects one that declares Applies.
func TestNewRejectsAppliesOnARequiredPass(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "required pass \"t-required-applies\" has an Applies") {
			t.Errorf("recover() = %v", r)
		}
	}()
	New(Def[*mod, *mod]{Decl: Decl{Name: "t-required-applies", Required: true}, In: modIR, Out: modIR,
		Applies: func(*Ctx[*mod]) bool { return false },
		Run:     func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveAll(), nil }})
}

// Stats sums each instance over its units: the runs, the runs that reported a change, and the
// declared counters in the order of their declaration. A pass that did not run is not listed.
func TestStatsSumsRunsChangesAndCountersInDeclarationOrder(t *testing.T) {
	maybeOn = false
	st := NewStats()
	if _, err := run(t, &Run{Observers: []Observer{st}}, tablePipe(Seq(modIR, tally, tally, maybe)), &prog{tables: []string{"a", "b", "c"}}); err != nil {
		t.Fatal(err)
	}
	counters := []Counter{{"zeta", 1}, {"alpha", 6}}
	want := []InstanceStats{
		{Instance: "t-build", Runs: 3, Changed: 3},
		{Instance: "t-tally", Runs: 3, Changed: 2, Counters: counters},
		{Instance: "t-tally#2", Runs: 3, Changed: 2, Counters: counters},
	}
	if got := st.Instances(); !reflect.DeepEqual(got, want) {
		t.Errorf("stats\n%+v,\nwant\n%+v", got, want)
	}
}

// The decisions of a pattern are counted in the result of the pass that asked: applied, or skipped
// by an observer. The events of the decisions add no runs.
func TestStatsCountsTheDecisionsOfPatterns(t *testing.T) {
	in := &prog{tables: []string{"a"}}
	veto := &recorder{veto: func(e Event) bool { return e.Step.Pattern && e.Seq == 3 }}
	st := NewStats()
	if _, err := run(t, &Run{Observers: []Observer{veto, st}}, tablePipe(Seq(modIR, canon)), in); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Instance("", "t-canon")
	want := InstanceStats{Instance: "t-canon", Runs: 1, Changed: 1, Patterns: []PatternDecisions{{"t-fold", Decisions{Applied: 1, Skipped: 1}}}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("with a veto: %+v, want %+v", got, want)
	}
	st = NewStats()
	all := &recorder{veto: func(e Event) bool { return e.Step.Pattern }}
	if _, err := run(t, &Run{Observers: []Observer{all, st}}, tablePipe(Seq(modIR, canon)), in); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Instance("", "t-canon")
	if want := []PatternDecisions{{"t-fold", Decisions{Skipped: 2}}}; !reflect.DeepEqual(got.Patterns, want) {
		t.Errorf("with every decision vetoed: %+v, want %+v", got.Patterns, want)
	}
}

// A reused Shared result counts as the run it replays, with the counters of the first run, so the
// statistics are the same with and without sharing.
func TestStatsCountReplaysAsRuns(t *testing.T) {
	p := twoBranches(Seq(modIR, tally))
	in := &prog{tables: []string{"a", "b"}}
	shared, alone := NewStats(), NewStats()
	if _, err := run(t, &Run{Observers: []Observer{shared}}, p, in); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, &Run{Observers: []Observer{alone}, NoShare: true}, p, in); err != nil {
		t.Fatal(err)
	}
	if a, b := shared.Instances(), alone.Instances(); !reflect.DeepEqual(a, b) {
		t.Errorf("with sharing\n%+v,\nwithout\n%+v", a, b)
	}
	got, _ := shared.Instance("ts", "t-tally")
	if want := (InstanceStats{Backend: "ts", Instance: "t-tally", Runs: 2, Changed: 1, Counters: []Counter{{"zeta", 1}, {"alpha", 4}}}); !reflect.DeepEqual(got, want) {
		t.Errorf("replayed branch: %+v, want %+v", got, want)
	}
}

// Watching a run with Stats changes neither its output nor what the other observers see; a pass
// that changes the output is told apart, so the comparison would catch an observer that did.
func TestStatsDoesNotChangeTheRun(t *testing.T) {
	p := tablePipe(Seq(modIR, upper, tally, canon, appnd))
	in := &prog{tables: []string{"a", "b"}}
	rec, recStats := &recorder{needs: Needs{Print: true}}, &recorder{needs: Needs{Print: true}}
	plain, err := run(t, &Run{Observers: []Observer{rec}}, p, in)
	if err != nil {
		t.Fatal(err)
	}
	watched, err := run(t, &Run{Observers: []Observer{recStats, NewStats()}}, p, in)
	if err != nil {
		t.Fatal(err)
	}
	if plain.String() != watched.String() || !reflect.DeepEqual(rec.lines, recStats.lines) || !reflect.DeepEqual(rec.dumps, recStats.dumps) {
		t.Errorf("Stats changed the run:\n%s\n%s", plain, watched)
	}
	// The negative control: turning one optional pass off changes the output, so equal outputs
	// above are not equal by construction.
	off, err := run(t, &Run{Settings: settings(t, "t-append"), Observers: []Observer{NewStats()}}, p, in)
	if err != nil {
		t.Fatal(err)
	}
	if off.String() == plain.String() {
		t.Errorf("the comparison does not see a changed output: %q", off)
	}
}

// work is the count of work that worker adds to and Run.Work reads.
var work uint64

// worker does one unit of work per function of its module.
var worker = New(Def[*mod, *mod]{Decl: Decl{Name: "t-worker", Test: true}, In: modIR, Out: modIR,
	Run: func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
		work += uint64(len(m.fns))
		return m, pass.PreserveAll(), nil
	}})

// Stats sums the work of each run of a pass (Run.Work), a replay counting the work of the run it
// replays, so the steps are the same with and without sharing; without Run.Work they are 0.
func TestStatsSumTheWorkOfTheRuns(t *testing.T) {
	p := twoBranches(Seq(modIR, worker))
	in := &prog{tables: []string{"a", "b"}}
	var got [2]InstanceStats
	for i, noShare := range []bool{false, true} {
		st := NewStats()
		work = 0
		if _, err := run(t, &Run{Observers: []Observer{st}, NoShare: noShare, Work: func() uint64 { return work }}, p, in); err != nil {
			t.Fatal(err)
		}
		got[i], _ = st.Instance("ts", "t-worker")
		if g, _ := st.Instance("go", "t-worker"); g.Steps == 0 || g.Steps != got[i].Steps {
			t.Errorf("NoShare %t: steps go %d, ts %d", noShare, g.Steps, got[i].Steps)
		}
	}
	if !reflect.DeepEqual(got[0], got[1]) {
		t.Errorf("with sharing %+v, without %+v", got[0], got[1])
	}
	st := NewStats()
	if _, err := run(t, &Run{Observers: []Observer{st}}, p, in); err != nil {
		t.Fatal(err)
	}
	if g, _ := st.Instance("go", "t-worker"); g.Steps != 0 {
		t.Errorf("steps without Run.Work: %d", g.Steps)
	}
}
