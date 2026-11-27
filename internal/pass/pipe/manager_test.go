package pipe

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

// twoBranches builds every table twice, in two branches that share the build and its Core-like
// front by the table's key.
func twoBranches(front Pipeline[*mod, *mod]) Pipeline[*prog, []*mods] {
	shared := Each(tablesSplit, Shared(tableKey, Then(Of(build), front)))
	return Fork(B("go", Then(shared, Of(passThrough))), B("ts", Then(shared, Of(passThrough))))
}

var passThrough = New(Def[*mods, *mods]{Decl: Decl{Name: "t-mods-pass", Required: true}, In: modsIR, Out: modsIR,
	Run: func(_ *Ctx[*mods], m *mods) (*mods, pass.PreservedSet, error) { return m, pass.PreserveNone(), nil }})

func TestSharedKeepsEventsAndNumbering(t *testing.T) {
	p := twoBranches(Seq(modIR, upper, appnd))
	in := &prog{tables: []string{"a", "b"}}
	shared, alone := &recorder{needs: Needs{Print: true}}, &recorder{needs: Needs{Print: true}}
	builds = 0
	out1, err := run(t, &Run{Observers: []Observer{shared}}, p, in)
	if err != nil {
		t.Fatal(err)
	}
	if builds != 2 {
		t.Errorf("%d builds with sharing, want 2", builds)
	}
	builds = 0
	out2, err := run(t, &Run{Observers: []Observer{alone}, NoShare: true}, p, in)
	if err != nil {
		t.Fatal(err)
	}
	if builds != 4 {
		t.Errorf("%d builds without sharing, want 4", builds)
	}
	if out1[0].String() != out2[0].String() || out1[1].String() != out2[1].String() {
		t.Errorf("outputs differ:\n%s\n%s", out1[1], out2[1])
	}
	var strip []string
	for _, l := range shared.lines {
		strip = append(strip, strings.TrimSuffix(l, " replayed"))
	}
	if !reflect.DeepEqual(strip, alone.lines) {
		t.Errorf("events with sharing\n%s\nwithout\n%s", strings.Join(shared.lines, "\n"), strings.Join(alone.lines, "\n"))
	}
	if !reflect.DeepEqual(shared.dumps, alone.dumps) || len(shared.dumps) == 0 {
		t.Errorf("dumps with sharing\n%s\nwithout\n%s", strings.Join(shared.dumps, "\n"), strings.Join(alone.dumps, "\n"))
	}
	if !contains(shared.lines, "5 t-upper [ts/a] ran=true changed=true replayed") {
		t.Errorf("the reuse is not a replay with a fresh number:\n%s", strings.Join(shared.lines, "\n"))
	}
}

// TestNumberingContinuesAcrossExecutes checks that the Executes that share a Numbering number the
// decided runs of optional passes once, in the order they run, and that an Execute without one
// numbers its own from 1.
func TestNumberingContinuesAcrossExecutes(t *testing.T) {
	seqs := func(r *recorder) []int {
		var out []int
		for _, l := range r.lines {
			var n int
			fmt.Sscanf(l, "%d", &n)
			if n != 0 {
				out = append(out, n)
			}
		}
		return out
	}
	p := tablePipe(Seq(modIR, upper, appnd))
	numbering := &Numbering{}
	var got []int
	for _, table := range []string{"a", "b"} {
		rec := &recorder{}
		if _, err := run(t, &Run{Observers: []Observer{rec}, Numbering: numbering}, p, &prog{tables: []string{table}}); err != nil {
			t.Fatal(err)
		}
		got = append(got, seqs(rec)...)
	}
	if want := []int{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Errorf("shared numbering gave %v, want %v", got, want)
	}
	rec := &recorder{}
	if _, err := run(t, &Run{Observers: []Observer{rec}}, p, &prog{tables: []string{"c"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := seqs(rec), []int{1, 2}; !slices.Equal(got, want) {
		t.Errorf("an Execute of its own gave %v, want %v", got, want)
	}
}

// TestInPlaceAfterSharedKeepsTheEntry checks that a pass that changes a shared result in place
// does not change it for the next use, also when the run consumes its units.
func TestInPlaceAfterSharedKeepsTheEntry(t *testing.T) {
	for _, stop := range []bool{false, true} {
		shared := Each(tablesSplit, Then(Shared(tableKey, Of(build)), Seq(modIR, upper)))
		plain := Each(tablesSplit, Shared(tableKey, Of(build)))
		p := Fork(B("go", shared), B("ts", plain))
		out, err := run(t, &Run{Stop: stop}, p, &prog{tables: []string{"a"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := out[1].String(); got != "[a]\na1: x\na2: y\n" {
			t.Errorf("stop=%v: the second use sees %q", stop, got)
		}
	}
}

func TestSharedFailureEqualsNoShare(t *testing.T) {
	p := twoBranches(Seq(modIR, upper, failer, appnd))
	in := &prog{tables: []string{"a"}}
	out1, err1 := run(t, &Run{}, p, in)
	out2, err2 := run(t, &Run{NoShare: true}, p, in)
	if err1 == nil || err1.Error() != err2.Error() {
		t.Errorf("errors differ:\n%v\n%v", err1, err2)
	}
	if out1[0].String() != out2[0].String() || out1[1].String() != out2[1].String() {
		t.Errorf("outputs differ")
	}
	if !strings.Contains(err1.Error(), "t-fail [ts/a]") {
		t.Errorf("the replayed error is not relabeled: %v", err1)
	}
}

// TestSharedIgnoresPerPartSlotFill checks that a slot filled for one table does not reuse the
// result of another body.
func TestSharedIgnoresPerPartSlotFill(t *testing.T) {
	key := RegisterKey(tableIR, &Key[table]{Name: "t-same-key", Of: func(table) string { return "same" }})
	p, err := Apply(Each(tablesSplit, Shared(key, Then(Of(build), Slot(modSlot, Seq(modIR))))), Fill(modSlot, "b", Seq(modIR, upper)))
	if err != nil {
		t.Fatal(err)
	}
	out, err := run(t, &Run{}, p, &prog{tables: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "[a]\na1: x\na2: y\n[b]\nb1: X\nb2: Y\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

func TestSharedKeyFormatsFeatures(t *testing.T) {
	front := Seq(modIR, feat)
	p := Fork(B("go", WithConst(targetFeature, "x", Each(tablesSplit, Shared(tableKey, Then(Of(build), front))))),
		B("ts", WithConst(targetFeature, "y", Each(tablesSplit, Shared(tableKey, Then(Of(build), front))))))
	out, err := run(t, &Run{}, p, &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out[1].String(); got != "[a]\na1: x@y\na2: y@y\n" {
		t.Errorf("a feature value that differs must not share: %q", got)
	}
}

func TestNothingPrintsWithoutAReader(t *testing.T) {
	prints = 0
	p := tablePipe(Seq(modIR, upper, appnd))
	if _, err := run(t, &Run{Observers: []Observer{&recorder{}}}, p, &prog{tables: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if prints != 0 {
		t.Errorf("%d units printed without a reader", prints)
	}
	rec := &recorder{needs: Needs{Print: true}}
	if _, err := run(t, &Run{Observers: []Observer{rec}}, p, &prog{tables: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if len(rec.dumps) != 3 || !strings.Contains(rec.dumps[2], "a1: X+") {
		t.Errorf("dumps: %q", rec.dumps)
	}
}

func TestCancelNamesTheStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopper := modPass("t-cancel", func(_ *Ctx[*mod], _ *mod) error { cancel(); return nil }, func(d *Decl) { d.Required = true })
	_, err := Execute(ctx, &Run{}, tablePipe(Seq(modIR, stopper, mark)), &prog{tables: []string{"a"}})
	var se *StepError
	if !errors.As(err, &se) || se.Step != "t-mark" || !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want the cancellation before t-mark", err)
	}
}

func TestRecoverTurnsAPanicIntoAnError(t *testing.T) {
	type stop struct{}
	panicker := modPass("t-panic", func(_ *Ctx[*mod], _ *mod) error { panic(stop{}) }, func(d *Decl) { d.Required = true })
	r := &Run{Recover: func(x any) error {
		if _, ok := x.(stop); ok {
			return context.Canceled
		}
		panic(x)
	}}
	_, err := run(t, r, tablePipe(Seq(modIR, panicker)), &prog{tables: []string{"a"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v", err)
	}
}

func TestChecksBeforeAnyStep(t *testing.T) {
	cases := map[string]func() error{
		"unprovided feature": func() error {
			_, err := run(t, &Run{}, tablePipe(Seq(modIR, feat)), &prog{tables: []string{"a"}})
			return err
		},
		"after order": func() error {
			_, err := run(t, &Run{}, tablePipe(Seq(modIR, needsAppend, appnd)), &prog{tables: []string{"a"}})
			return err
		},
		"per function lowering": func() error {
			_, err := run(t, &Run{}, PerFunc(funcsView, Pipeline[*fn, *fn]{&passNode{p: build.d}}), &mod{})
			return err
		},
		"undeclared part": func() error {
			p, _ := Apply(tablePipe(Slot(modSlot, Seq(modIR))), Fill(modSlot, "z", Seq(modIR, upper)))
			_, err := run(t, &Run{}, p, &prog{tables: []string{"a"}})
			return err
		},
		"required step in a part body": func() error {
			p, _ := Apply(tablePipe(Slot(modSlot, Seq(modIR))), Fill(modSlot, "a", Seq(modIR, mark)))
			_, err := run(t, &Run{}, p, &prog{tables: []string{"a"}})
			return err
		},
		"part body outside each": func() error {
			p, _ := Apply(Slot(modSlot, Seq(modIR)), Fill(modSlot, "a", Seq(modIR, upper)))
			_, err := run(t, &Run{}, p, &mod{})
			return err
		},
		"shared key of another kind": func() error {
			_, err := run(t, &Run{}, Each(tablesSplit, Then(Of(build), Pipeline[*mod, *mod]{&sharedNode{key: tableKey2(), body: Seq(modIR, upper).n}})), &prog{tables: []string{"a"}})
			return err
		},
	}
	for name, f := range cases {
		builds = 0
		if err := f(); !errors.Is(err, errCheck) {
			t.Errorf("%s: error %v, want an invalid pipeline", name, err)
		}
		if builds != 0 {
			t.Errorf("%s: a step ran before the check", name)
		}
	}
}

func tableKey2() *keyDesc { return keys[tableKey.Name] }

func TestFeatureReachesThePass(t *testing.T) {
	out, err := run(t, &Run{}, WithConst(targetFeature, "go", tablePipe(Seq(modIR, feat))), &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "[a]\na1: x@go\na2: y@go\n" {
		t.Errorf("output %q", got)
	}
}

// TestAllowNumbersPatterns checks that each application of a pattern is a decision of its own:
// numbered and vetoed by an observer; the pattern has no flag, and its pass's flag turns off the pass
// with its decisions.
func TestAllowNumbersPatterns(t *testing.T) {
	rec := &recorder{veto: func(e Event) bool { return e.Seq == 3 }}
	out, err := run(t, &Run{Observers: []Observer{rec}}, tablePipe(Seq(modIR, canon)), &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "[a]\na1: xf\na2: y\n" {
		t.Errorf("output %q", got)
	}
	if !contains(rec.lines, "2 t-canon/t-fold [a] ran=true changed=false") || !contains(rec.lines, "3 t-canon/t-fold [a] ran=false changed=false") {
		t.Errorf("events:\n%s", strings.Join(rec.lines, "\n"))
	}
	if _, err := (pass.Settings{}).Without("t-fold"); err == nil {
		t.Error("the pattern t-fold is a flag")
	}
	rec = &recorder{}
	out, err = run(t, &Run{Settings: settings(t, "t-canon"), Observers: []Observer{rec}}, tablePipe(Seq(modIR, canon)), &prog{tables: []string{"a"}})
	if err != nil || out.String() != "[a]\na1: x\na2: y\n" {
		t.Errorf("the pass's flag does not turn its pattern off: %v %v", out, err)
	}
	if strings.Contains(strings.Join(rec.lines, "\n"), "t-canon/t-fold") {
		t.Errorf("a pass that is off decides its pattern:\n%s", strings.Join(rec.lines, "\n"))
	}
}

var computed int

var sizeKey = pass.NewAnalysisKey("t-size", func(m *mod, _ *pass.AnalysisManager[*mod]) (int, error) {
	computed++
	return len(m.String()), nil
})

func querier(name string, pres pass.PreservedSet) *Pass[*mod, *mod] {
	return New(Def[*mod, *mod]{Decl: Decl{Name: name, Test: true}, In: modIR, Out: modIR,
		Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
			if _, err := Query(c, sizeKey); err != nil {
				return nil, pass.PreservedSet{}, err
			}
			return m, pres, nil
		}})
}

func TestQueryCachesAndInvalidates(t *testing.T) {
	keep, drop := querier("t-query-keep", pass.PreserveAll()), querier("t-query-drop", pass.PreserveNone())
	computed = 0
	if _, err := run(t, &Run{Stop: true}, Seq(modIR, keep, keep, drop, keep), &mod{fns: []*fn{{"f", "x"}}}); err != nil {
		t.Fatal(err)
	}
	if computed != 2 {
		t.Errorf("%d computations, want 2", computed)
	}
}
