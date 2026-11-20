package pipe

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/pass"
)

func settings(t *testing.T, off ...string) pass.Settings {
	t.Helper()
	s, err := pass.Settings{}.Without(off...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func run[I, O any](t *testing.T, r *Run, p Pipeline[I, O], in I) (O, error) {
	t.Helper()
	return Execute(context.Background(), r, p, in)
}

// tablePipe lowers each table and runs body on its module.
func tablePipe(body Pipeline[*mod, *mod]) Pipeline[*prog, *mods] {
	return Each(tablesSplit, Then(Of(build), body))
}

func TestTypedPipelineRunsAndNamesInstances(t *testing.T) {
	rec := &recorder{}
	p := tablePipe(Seq(modIR, upper, appnd, appnd))
	out, err := run(t, &Run{Observers: []Observer{rec}}, p, &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "[a]\na1: X++\na2: Y++\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
	want := []string{
		"0 t-build [a] ran=true changed=true",
		"1 t-upper [a] ran=true changed=true",
		"2 t-append [a] ran=true changed=true",
		"3 t-append#2 [a] ran=true changed=true",
	}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("events\n%s\nwant\n%s", strings.Join(rec.lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestDisabledPassDoesNotRun(t *testing.T) {
	rec := &recorder{}
	out, err := run(t, &Run{Settings: settings(t, "t-upper"), Observers: []Observer{rec}}, tablePipe(Seq(modIR, upper, appnd)), &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "[a]\na1: x+\na2: y+\n" {
		t.Errorf("output %q", got)
	}
	// A pass that the settings turn off is no event and takes no number.
	if want := []string{"0 t-build [a] ran=true changed=true", "1 t-append [a] ran=true changed=true"}; !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("events\n%s\nwant\n%s", strings.Join(rec.lines, "\n"), strings.Join(want, "\n"))
	}
}

// TestInapplicablePassDoesNotRun checks that a pass whose Applies says no is skipped like a disabled
// one: it is no event, takes no number and leaves the unit as it was.
func TestInapplicablePassDoesNotRun(t *testing.T) {
	rec := &recorder{}
	m := &mod{fns: []*fn{{"f", "x"}}}
	out, err := run(t, &Run{Observers: []Observer{rec}}, PerFunc(funcsView, Seq(fnIR, fnNever, fnUpper)), m)
	if err != nil || out.String() != "f: X\n" {
		t.Fatalf("%v %v", out, err)
	}
	if want := []string{"1 t-fn-upper [f] ran=true changed=true"}; !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("events\n%s\nwant\n%s", strings.Join(rec.lines, "\n"), strings.Join(want, "\n"))
	}
}

// TestPerFuncRunsPassByPass checks that the passes of a PerFunc block run pass by pass over the
// functions in their order, each run numbered in that order with its function in the label.
func TestPerFuncRunsPassByPass(t *testing.T) {
	rec := &recorder{}
	m := &mod{fns: []*fn{{"f", "x"}, {"g", "y"}}}
	if _, err := run(t, &Run{Observers: []Observer{rec}}, PerFunc(funcsView, Seq(fnIR, fnUpper, fnUpper)), m); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"1 t-fn-upper [f] ran=true changed=true",
		"2 t-fn-upper [g] ran=true changed=true",
		"3 t-fn-upper#2 [f] ran=true changed=true",
		"4 t-fn-upper#2 [g] ran=true changed=true",
	}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("events\n%s\nwant\n%s", strings.Join(rec.lines, "\n"), strings.Join(want, "\n"))
	}
}

// unitKeeper keeps every unit an observer is given, with its text at the time.
type unitKeeper struct {
	units []*mod
	texts []string
}

func (*unitKeeper) Needs() Needs                       { return Needs{Units: true} }
func (*unitKeeper) Before(Event, func(io.Writer)) bool { return true }
func (k *unitKeeper) After(_ Event, r *Result) {
	if m, ok := r.Unit.(*mod); ok {
		k.units, k.texts = append(k.units, m), append(k.texts, m.String())
	}
}

// TestObservedUnitsStayAsTheyWere checks that no later step changes a unit that an observer was
// given, with and without -verify-each, required steps included.
func TestObservedUnitsStayAsTheyWere(t *testing.T) {
	for _, verifyEach := range []bool{false, true} {
		k := &unitKeeper{}
		in := &mod{fns: []*fn{{"f", "x"}}}
		out, err := run(t, &Run{VerifyEach: verifyEach, Observers: []Observer{k}}, Seq(modIR, upper, appnd, mark), in)
		if err != nil || out.String() != "f: X+#\n" {
			t.Fatalf("verify each %v: %v %v", verifyEach, out, err)
		}
		if len(k.units) != 3 {
			t.Fatalf("verify each %v: %d units observed, want 3", verifyEach, len(k.units))
		}
		for i, m := range k.units {
			if m.String() != k.texts[i] {
				t.Errorf("verify each %v: unit %d was changed after the observer got it: %q, was %q", verifyEach, i, m, k.texts[i])
			}
		}
	}
}

// TestPartBodyInsideForkInEach checks that a Fork nested in an Each part keeps the part's table: a
// slot body filled for that part runs in the branch, and the branch's events name the table. It
// fails on a Fork that builds the branch's label from the backend alone.
func TestPartBodyInsideForkInEach(t *testing.T) {
	inner := Then3(Of(build), Fork(B("x", Slot(modSlot, Seq(modIR)))), Of(pick))
	p, err := Apply(Each(tablesSplit, inner), Fill(modSlot, "a", Seq(modIR, upper)))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	out, err := run(t, &Run{Observers: []Observer{rec}}, p, &prog{tables: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "[a]\na1: X\na2: Y\n[b]\nb1: x\nb2: y\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
	if !contains(rec.lines, "1 t-upper [x/a] ran=true changed=true") {
		t.Errorf("the part body's event does not carry the table:\n%s", strings.Join(rec.lines, "\n"))
	}
}

// TestChangedIndependentOfTextObserver checks that Changed is the pass's report whatever other
// observers watch, and that TextChanged compares the text forms.
func TestChangedIndependentOfTextObserver(t *testing.T) {
	p := tablePipe(Seq(modIR, noop, upper))
	plain, text := &recorder{}, &recorder{needs: Needs{TextChanged: true}}
	if _, err := run(t, &Run{Observers: []Observer{plain}}, p, &prog{tables: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, &Run{Observers: []Observer{plain, text}}, p, &prog{tables: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(plain.lines) / 2; !reflect.DeepEqual(plain.lines[:n], plain.lines[n:]) {
		t.Errorf("a second observer changed what the first reads:\n%s", strings.Join(plain.lines, "\n"))
	}
	if !contains(text.lines, "1 t-noop [a] ran=true changed=true text=false") || !contains(text.lines, "2 t-upper [a] ran=true changed=true text=true") {
		t.Errorf("text comparison:\n%s", strings.Join(text.lines, "\n"))
	}
}

func TestCopyPerSegment(t *testing.T) {
	in := func() *mod { return &mod{fns: []*fn{{"f", "x"}, {"g", "y"}}} }
	seg := Seq(modIR, upper, appnd)
	for _, c := range []struct {
		name string
		r    *Run
		want int
	}{
		{"failure rule", &Run{}, 1},
		{"kept input", &Run{Keep: true}, 1},
		{"stop", &Run{Stop: true}, 0},
		{"stop with kept input", &Run{Stop: true, Keep: true}, 1},
		{"verify each", &Run{VerifyEach: true}, 2},
		{"per function segment", nil, 1},
	} {
		got := copiesOf(func() {
			if c.r == nil {
				run(t, &Run{}, PerFunc(funcsView, Seq(fnIR, fnUpper, fnUpper)), in())
				return
			}
			run(t, c.r, seg, in())
		})
		if got != c.want {
			t.Errorf("%s: %d copies, want %d", c.name, got, c.want)
		}
	}
	m := in()
	if _, err := run(t, &Run{Keep: true}, seg, m); err != nil {
		t.Fatal(err)
	}
	if m.String() != "f: x\ng: y\n" {
		t.Errorf("the kept input changed: %q", m)
	}
}

func TestFailureRollsBackTheSegment(t *testing.T) {
	rec := &recorder{}
	out, err := run(t, &Run{Observers: []Observer{rec}}, tablePipe(Seq(modIR, upper, failer, appnd, mark)), &prog{tables: []string{"a", "b"}})
	var se *StepError
	if !errors.As(err, &se) || se.Step != "t-fail" || se.Label.Table != "a" {
		t.Fatalf("error %v, want the instance t-fail on table a", err)
	}
	// The segment rolls back to its start, the later optional pass is skipped, the required one runs.
	if got, want := out.String(), "[a]\na1: x#\na2: y#\n[b]\nb1: x#\nb2: y#\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
	if contains(rec.lines, "3 t-append [a] ran=true changed=true") {
		t.Errorf("a pass after the failure ran:\n%s", strings.Join(rec.lines, "\n"))
	}
}

func TestRequiredTransformSurvivesRollback(t *testing.T) {
	out, err := run(t, &Run{}, tablePipe(Seq(modIR, upper, mark, appnd, failer)), &prog{tables: []string{"a"}})
	if err == nil {
		t.Fatal("no error")
	}
	if got, want := out.String(), "[a]\na1: X#\na2: Y#\n"; got != want {
		t.Errorf("output %q, want %q: the rollback must stop at the required transform", got, want)
	}
}

func TestPerFuncFailureOnOneFunction(t *testing.T) {
	m := &mod{fns: []*fn{{"f1", "x"}, {"f2", "y"}, {"f3", "z"}}}
	out, err := run(t, &Run{}, Then(PerFunc(funcsView, Seq(fnIR, fnFail, fnUpper)), Seq(modIR, upper, mark)), m)
	var se *StepError
	if !errors.As(err, &se) || se.Step != "t-fn-fail" || se.Label.Func != "f2" {
		t.Fatalf("error %v, want t-fn-fail on f2", err)
	}
	// f2 rolls back and ends its passes; the others finish the segment; the later optional segment
	// is skipped and the required step runs.
	if got, want := out.String(), "f1: X-#\nf2: y#\nf3: Z-#\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

func TestStopBreaksTheRun(t *testing.T) {
	m := &mod{fns: []*fn{{"f1", "x"}, {"f2", "y"}}}
	out, err := run(t, &Run{Stop: true, Keep: true}, Then(PerFunc(funcsView, Seq(fnIR, fnFail)), Seq(modIR, mark)), m)
	if err == nil || out != nil {
		t.Fatalf("got %v, %v; want no output and an error", out, err)
	}
	if m.String() != "f1: x\nf2: y\n" {
		t.Errorf("the kept unit changed: %q", m)
	}
}

func TestVerificationNamesThePass(t *testing.T) {
	_, err := run(t, &Run{}, tablePipe(Seq(modIR, breaker, mark)), &prog{tables: []string{"a"}})
	var se *StepError
	if !errors.As(err, &se) || se.Step != "verify t-mod" {
		t.Errorf("without -verify-each the end of the stage verifies: %v", err)
	}
	_, err = run(t, &Run{VerifyEach: true}, tablePipe(Seq(modIR, breaker, mark)), &prog{tables: []string{"a"}})
	if !errors.As(err, &se) || se.Step != "t-break" {
		t.Errorf("under -verify-each the error names the pass: %v", err)
	}
	_, err = run(t, &Run{VerifyEach: true}, tablePipe(Seq(modIR, liar)), &prog{tables: []string{"a"}})
	if err == nil || !strings.Contains(err.Error(), "preserves all analyses but changed") {
		t.Errorf("a pass that claims to preserve everything and changes the unit: %v", err)
	}
}
