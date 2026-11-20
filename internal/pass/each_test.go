package pass

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// table is a unit of several functions, each a unit of its own.
type table struct {
	names []string
	funcs []*unit
}

var tableFuncs = Funcs[*table, *unit]{
	Len:  func(t *table) int { return len(t.funcs) },
	Name: func(t *table, i int) string { return t.names[i] },
	Get:  func(t *table, i int) *unit { return t.funcs[i] },
	Put: func(t *table, i int, u *unit) error {
		t.funcs[i] = u
		return nil
	},
}

func newTable(names ...string) *table {
	t := &table{names: names}
	for range names {
		t.funcs = append(t.funcs, &unit{})
	}
	return t
}

// eachFunc runs p on each function of a table, keeping the functions whose passes fail; the context of
// a function says whether its name is "lean", for passes that apply to lean functions only.
func eachFunc(p *Pipeline[*unit, mode]) Stage[*table, *table] {
	return eachFuncWith(p, true, nil)
}

// eachFuncWith is eachFunc with keep, and with the managers of the analyses that analyses returns for a
// function's unit (nil: a manager of its own per run).
func eachFuncWith(p *Pipeline[*unit, mode], keep bool, analyses func(*unit) *AnalysisManager[*unit]) Stage[*table, *table] {
	if p.Copy == nil {
		p.Copy = func(u *unit) *unit {
			v := *u
			v.log = slices.Clone(u.log)
			return &v
		}
	}
	return EachFunc(tableFuncs, Transform[*unit, mode]{IR: unitIR, Pipeline: p, Keep: keep, Analyses: analyses,
		Env: func(_ *Run, l Label, _ *unit) mode { return mode{lean: l.Func == "lean"} }})
}

// runEach runs s on a table of the functions f, lean and g under an observer that skips the runs in
// skip, and returns the table, the events and the dumps.
func runEach(t *testing.T, s Stage[*table, *table], skip map[int]bool) (*table, *recorder, *Run, error) {
	t.Helper()
	rec := &recorder{skip: skip}
	r := &Run{Observers: []Observer{rec}}
	out, err := s.Run(r, Label{Table: "rules"}, newTable("f", "lean", "g"))
	return out, rec, r, err
}

// TestEachFuncPassMajor checks that the passes run on the functions of a unit pass by pass, that each
// run of an optional pass is numbered and dumped in that order with its function in the label, that
// each function has its own context, and that the numbering and the dumps are the same on every run.
func TestEachFuncPassMajor(t *testing.T) {
	onlyLean := logging("b", nil, none)
	onlyLean.Applies = func(x mode) bool { return x.lean }
	s := eachFunc(fakePipeline(logging("a", nil, none), onlyLean, logging("c", nil, none)))
	out, rec, r, err := runEach(t, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1:a:a:f", "2:a:a:lean", "3:a:a:g", "4:b:b:lean", "5:c:c:f", "6:c:c:lean", "7:c:c:g"}
	if !slices.Equal(rec.events, want) || r.Runs() != 7 {
		t.Errorf("events %v, runs %d, want %v", rec.events, r.Runs(), want)
	}
	for i, w := range [][]string{{"a", "c", "analyze"}, {"a", "b", "c", "analyze"}, {"a", "c", "analyze"}} {
		if !slices.Equal(out.funcs[i].log, w) {
			t.Errorf("function %s: %v, want %v", out.names[i], out.funcs[i].log, w)
		}
	}
	wantDump := "rules/f after a: a\nrules/lean after a: a\nrules/g after a: a\nrules/lean after b: a,b\n" +
		"rules/f after c: a,c\nrules/lean after c: a,b,c\nrules/g after c: a,c\n"
	if rec.dumps.String() != wantDump {
		t.Errorf("dumps %q, want %q", rec.dumps.String(), wantDump)
	}
	// The same compilation numbers and dumps the same runs in the same order: bisection by number
	// (an observer that skips a run) names one pass on one function.
	_, again, _, err := runEach(t, s, map[int]bool{2: true})
	if err != nil {
		t.Fatal(err)
	}
	if w := slices.Delete(slices.Clone(want), 1, 2); !slices.Equal(again.events, w) {
		t.Errorf("events skipping run 2: %v, want %v", again.events, w)
	}
}

// TestEachFuncFailure checks that a pass that fails on one function ends the passes of that
// function alone, which a kept stage rolls back, and that its failure is the stage's.
func TestEachFuncFailure(t *testing.T) {
	bad := Pass[*unit, mode]{Name: "b", Run: func(u *unit, x mode) (*unit, PreservedSet, error) {
		if x.lean {
			return u, none, errors.New("no")
		}
		return u.with("b"), none, nil
	}}
	s := Then(eachFunc(fakePipeline(logging("a", nil, none), bad, logging("c", nil, none))),
		eachFunc(fakePipeline(logging("d", nil, none))))
	out, rec, _, err := runEach(t, s, nil)
	if err == nil || !strings.Contains(err.Error(), "function lean: pass b: no") {
		t.Fatalf("error %v", err)
	}
	for i, w := range [][]string{{"a", "b", "c", "analyze"}, nil, {"a", "b", "c", "analyze"}} {
		if !slices.Equal(out.funcs[i].log, w) {
			t.Errorf("function %s: %v, want %v", out.names[i], out.funcs[i].log, w)
		}
	}
	// The passes of a later stage do not run after the failure.
	if want := []string{"1:a:a:f", "2:a:a:lean", "3:a:a:g", "4:b:b:f", "6:b:b:g", "7:c:c:f", "8:c:c:g"}; !slices.Equal(rec.events, want) {
		t.Errorf("events %v, want %v", rec.events, want)
	}
}

// TestEachFuncKeepNeedsACopy checks that a kept pointer unit without a Copy is refused before any
// pass runs, as for Transforms.
func TestEachFuncKeepNeedsACopy(t *testing.T) {
	s := EachFunc(tableFuncs, Transform[*unit, mode]{IR: unitIR, Pipeline: fakePipeline(logging("a", nil, none)), Keep: true,
		Env: func(*Run, Label, *unit) mode { return mode{} }})
	if err := s.Check(); err == nil {
		t.Fatal("Check accepted a kept pointer unit without a Copy")
	}
	if _, _, r, err := runEach(t, s, nil); err == nil || r.Runs() != 0 {
		t.Errorf("runs %d, error %v", r.Runs(), err)
	}
}

// failOnLean is a pass that fails on the lean function and records itself on the others.
var failOnLean = Pass[*unit, mode]{Name: "b", Requires: []Analysis{"attrs"}, Run: func(u *unit, x mode) (*unit, PreservedSet, error) {
	if x.lean {
		return u.with("b"), none, errors.New("no")
	}
	return u.with("b"), all, nil
}}

// TestEachFuncFailureConsumes checks the failure of a stage that does not keep its units: the
// function whose pass failed is left as the failing pass got it (a pass may change it in place), its
// analyses are dropped from its own manager, and the managers of the other functions keep theirs.
func TestEachFuncFailureConsumes(t *testing.T) {
	ams := map[*unit]*AnalysisManager[*unit]{}
	in := newTable("f", "lean", "g")
	units := slices.Clone(in.funcs) // the stage puts its units in place of these
	for _, u := range units {
		ams[u] = NewAnalysisManager[*unit](nil)
	}
	s := eachFuncWith(fakePipeline(logging("a", []Analysis{"attrs"}, all), failOnLean), false, func(u *unit) *AnalysisManager[*unit] { return ams[u] })
	out, err := s.Run(&Run{}, Label{Table: "rules"}, in)
	if err == nil || !strings.Contains(err.Error(), "function lean: pass b: no") {
		t.Fatalf("error %v", err)
	}
	for i, w := range [][]string{{"analyze", "a", "b"}, {"analyze", "a"}, {"analyze", "a", "b"}} {
		if !slices.Equal(out.funcs[i].log, w) {
			t.Errorf("function %s: %v, want %v", out.names[i], out.funcs[i].log, w)
		}
	}
	for i, cached := range []bool{true, false, true} {
		am := ams[units[i]]
		if am.Cached("attrs") != cached || am.Computations("attrs") != 1 {
			t.Errorf("function %s: attrs cached %v, computed %d times; want cached %v, once", in.names[i], am.Cached("attrs"), am.Computations("attrs"), cached)
		}
	}
}

// TestEachFuncSharedAnalyses checks that functions that share one manager of analyses get the results
// of their own unit: the manager discards the cache of one function when another asks, so sharing it
// costs computations but not correctness, and a failure on one function drops what it holds.
func TestEachFuncSharedAnalyses(t *testing.T) {
	shared := NewAnalysisManager[*unit](nil)
	p := func() *Pipeline[*unit, mode] { return fakePipeline(logging("a", []Analysis{"attrs"}, all), failOnLean) }
	own, err := eachFuncWith(p(), false, nil).Run(&Run{}, Label{Table: "rules"}, newTable("f", "lean", "g"))
	if err == nil {
		t.Fatal("no failure")
	}
	out, err := eachFuncWith(p(), false, func(*unit) *AnalysisManager[*unit] { return shared }).Run(&Run{}, Label{Table: "rules"}, newTable("f", "lean", "g"))
	if err == nil || !strings.Contains(err.Error(), "function lean: pass b: no") {
		t.Fatalf("error %v", err)
	}
	// The fake analysis records each computation in the unit; the passes' records must agree.
	passes := func(log []string) []string {
		return slices.DeleteFunc(slices.Clone(log), func(s string) bool { return s == "analyze" })
	}
	for i := range out.funcs {
		if !slices.Equal(passes(out.funcs[i].log), passes(own.funcs[i].log)) {
			t.Errorf("function %s: %v with a shared manager, %v with its own", out.names[i], out.funcs[i].log, own.funcs[i].log)
		}
	}
	if n := shared.Computations("attrs"); n <= 3 {
		t.Errorf("attrs computed %d times by the shared manager, want more than once per function", n)
	}
}
