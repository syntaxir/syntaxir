package pipe

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

// The tests of the manager's policies at their edges: sharing, analyses, failures, copies, the
// kinds without a printer, the allocations of an unobserved run, and the checks before a run.

// failOnA fails on the table a and changes nothing on the others.
var failOnA = New(Def[table, table]{Decl: Decl{Name: "t-fail-on-a", Test: true}, In: tableIR, Out: tableIR,
	Run: func(_ *Ctx[table], t table) (table, pass.PreservedSet, error) {
		if t.name == "a" {
			return t, pass.PreservedSet{}, errors.New("broken")
		}
		return t, pass.PreserveAll(), nil
	}})

// failTable always fails on a table, a value kind.
var failTable = New(Def[table, table]{Decl: Decl{Name: "t-fail-table", Test: true}, In: tableIR, Out: tableIR,
	Run: func(_ *Ctx[table], t table) (table, pass.PreservedSet, error) {
		return t, pass.PreservedSet{}, errors.New("broken")
	}})

func TestSharedKeyOfAFeatureProvidedInside(t *testing.T) {
	p := Each(tablesSplit, Shared(tableKey, Then(Of(build), WithConst(targetFeature, "go", Seq(modIR, feat)))))
	out, err := run(t, &Run{}, p, &prog{tables: []string{"a", "a"}})
	if err != nil || out.String() != "[a]\na1: x@go\na2: y@go\n[a]\na1: x@go\na2: y@go\n" {
		t.Errorf("%q %v", out, err)
	}
}

func TestSharedEntryAfterAFailureIsNotReused(t *testing.T) {
	key := RegisterKey(tableIR, &Key[table]{Name: "t-same-key-2", Of: func(table) string { return "same" }})
	p := Each(tablesSplit, Then(Seq(tableIR, failOnA), Shared(key, Then(Of(build), Seq(modIR, upper)))))
	shared, e1 := run(t, &Run{}, p, &prog{tables: []string{"a", "b"}})
	alone, e2 := run(t, &Run{NoShare: true}, p, &prog{tables: []string{"a", "b"}})
	if shared.String() != alone.String() || fmt.Sprint(e1) != fmt.Sprint(e2) {
		t.Errorf("sharing changes the run:\n%q %v\n%q %v", shared, e1, alone, e2)
	}
	if !strings.Contains(alone.String(), "b1: X") {
		t.Errorf("the table b skipped its passes after a failure of a: %q", alone)
	}
}

func TestEachPartStartsFromTheEntry(t *testing.T) {
	p := Each(tablesSplit, Then(Seq(tableIR, failOnA), Then(Of(build), Seq(modIR, upper))))
	out, err := run(t, &Run{}, p, &prog{tables: []string{"a", "b"}})
	if err == nil || out.String() != "[a]\na1: x\na2: y\n[b]\nb1: X\nb2: Y\n" {
		t.Errorf("a failure of one part skipped the passes of another: %q %v", out, err)
	}
}

func TestStopBreaksOnAValueKind(t *testing.T) {
	out, err := run(t, &Run{Stop: true}, Then(Seq(tableIR, failTable), Of(build)), table{"a"})
	if out != nil || err == nil {
		t.Errorf("under Stop the run went on after a failure: %v %v", out, err)
	}
}

func TestSharedKeepsItsResultWithoutAFork(t *testing.T) {
	p := Each(tablesSplit, Then(Shared(tableKey, Of(build)), Seq(modIR, appnd)))
	// The third use replays the entry after the second, which replayed it too.
	out, err := run(t, &Run{Stop: true}, p, &prog{tables: []string{"a", "a", "a"}})
	if want := strings.Repeat("[a]\na1: x+\na2: y+\n", 3); err != nil || out.String() != want {
		t.Errorf("a pass after a shared result changed it for the next use: %q %v", out, err)
	}
}

func TestRollbackMarksTheUnitKept(t *testing.T) {
	in := &mod{fns: []*fn{{"f", "x"}}}
	out, err := run(t, &Run{Keep: true}, Seq(modIR, upper, failer, mark), in)
	if err == nil || out.String() != "f: x#\n" || in.String() != "f: x\n" {
		t.Errorf("out %q, in %q, err %v: the required step after the rollback changed the kept input", out, in, err)
	}
}

func TestObserverVetoesAPass(t *testing.T) {
	rec := &recorder{veto: func(e Event) bool { return e.Step.Name == "t-upper" }}
	out, err := run(t, &Run{Observers: []Observer{rec}}, Seq(modIR, upper, appnd), &mod{fns: []*fn{{"f", "x"}}})
	if err != nil || out.String() != "f: x+\n" || !contains(rec.lines, "1 t-upper [] ran=false changed=false") {
		t.Errorf("%q %v\n%s", out, err, strings.Join(rec.lines, "\n"))
	}
}

var counter = New(Def[*mod, *mod]{Decl: Decl{Name: "t-counter", Test: true, Counters: []string{"functions"}}, In: modIR, Out: modIR,
	Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
		c.Count("functions", len(m.fns))
		c.Count("functions", 1)
		return m, pass.PreserveAll(), nil
	}})

type counts struct{ got map[string]int }

func (c *counts) Needs() Needs                       { return Needs{} }
func (c *counts) Before(Event, func(io.Writer)) bool { return true }
func (c *counts) After(e Event, r *Result) {
	for k, v := range r.Counters {
		c.got[e.Step.Instance+"."+k] += v
	}
}

func TestCountReachesTheObserver(t *testing.T) {
	c := &counts{got: map[string]int{}}
	if _, err := run(t, &Run{Observers: []Observer{c}}, Seq(modIR, counter), &mod{fns: []*fn{{"f", "x"}, {"g", "y"}}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.got, map[string]int{"t-counter.functions": 3}) {
		t.Errorf("counters %v", c.got)
	}
}

var badBuild = New(Def[table, *mod]{Decl: Decl{Name: "t-bad-build", Required: true}, In: tableIR, Out: modIR,
	Run: func(_ *Ctx[table], t table) (*mod, pass.PreservedSet, error) {
		return &mod{fns: []*fn{{t.name + "1", "bad"}}}, pass.PreserveNone(), nil
	}})

func TestLoweringPostcondition(t *testing.T) {
	out, err := run(t, &Run{}, Then(Of(badBuild), Of(mark)), table{"a"})
	var se *StepError
	if out != nil || !errors.As(err, &se) || se.Step != "t-bad-build" {
		t.Errorf("a lowering whose result does not verify: %v %v", out, err)
	}
}

// pair is a composite unit whose part is a module.
type pair struct {
	m   *mod
	tag string
}

var (
	pairIR = RegisterIR(&IR[*pair]{Name: "t-pair",
		Print: func(w io.Writer, p *pair) { fmt.Fprintf(w, "%s:\n%s", p.tag, p.m) },
		Copy:  func(p *pair) *pair { return &pair{m: modIR.Copy(p.m), tag: p.tag} }})
	pairLens = RegisterLens(pairIR, pairIR, modIR, modIR, &Lens[*pair, *pair, *mod, *mod]{Name: "t-pair-mod",
		Get: func(p *pair) *mod { return p.m },
		Put: func(p *pair, m *mod) (*pair, error) { return &pair{m: m, tag: p.tag}, nil }})
	firstName = RegisterDerive(modIR, &Derive[*mod, string]{Name: "t-first-name", Key: targetFeature,
		Of: func(m *mod) string { return m.fns[0].name }})
)

func TestWithinRoutesThePart(t *testing.T) {
	in := &pair{m: &mod{fns: []*fn{{"f", "x"}}}, tag: "t"}
	n := 0
	out, err := (*pair)(nil), error(nil)
	n = copiesOf(func() { out, err = run(t, &Run{Keep: true}, Within(pairLens, Seq(modIR, upper, appnd)), in) })
	if err != nil || out.tag != "t" || out.m.String() != "f: X+\n" {
		t.Errorf("%v %v", out, err)
	}
	if in.m.String() != "f: x\n" || n != 1 {
		t.Errorf("the part of a kept whole: input %q after the run, %d copies, want it unchanged after 1", in.m, n)
	}
}

var (
	errRefused   = errors.New("refused")
	refusingLens = RegisterLens(pairIR, pairIR, modIR, modIR, &Lens[*pair, *pair, *mod, *mod]{Name: "t-pair-refuse",
		Get: func(p *pair) *mod { return p.m },
		Put: func(*pair, *mod) (*pair, error) { return nil, errRefused }})
)

// TestWithinRefusedPartFails checks that a part that the lens refuses to put back fails the run,
// named after the lens, and that no unit goes on.
func TestWithinRefusedPartFails(t *testing.T) {
	in := &pair{m: &mod{fns: []*fn{{"f", "x"}}}, tag: "t"}
	out, err := run(t, &Run{}, Then(Within(refusingLens, Seq(modIR, upper)), Within(pairLens, Seq(modIR, mark))), in)
	var se *StepError
	if out != nil || !errors.As(err, &se) || se.Step != "within t-pair-refuse" || !errors.Is(err, errRefused) {
		t.Errorf("a refused part: %v %v", out, err)
	}
}

func TestWithDerivedProvidesTheFeature(t *testing.T) {
	out, err := run(t, &Run{}, WithDerived(firstName, Seq(modIR, feat)), &mod{fns: []*fn{{"f", "x"}, {"g", "y"}}})
	if err != nil || out.String() != "f: x@f\ng: y@f\n" {
		t.Errorf("%q %v", out, err)
	}
}

func TestDerivedProviderReadsTheKindOfItsBlock(t *testing.T) {
	p := Pipeline[table, table]{&withNode{prov: lookup(providers, firstName.Name), body: Seq(tableIR).n}}
	if _, err := run(t, &Run{}, p, table{"a"}); err == nil || !strings.Contains(err.Error(), "provider t-first-name reads t-mod") {
		t.Errorf("a derived provider on a block of another kind: %v", err)
	}
}

// errRecorder records each event with the label of its error.
type errRecorder struct{ lines []string }

func (r *errRecorder) Needs() Needs                       { return Needs{} }
func (r *errRecorder) Before(Event, func(io.Writer)) bool { return true }
func (r *errRecorder) After(e Event, res *Result) {
	l := fmt.Sprintf("%s [%s]", e.Step.Instance, e.Label)
	if res.Err != nil {
		l += " failed"
	}
	r.lines = append(r.lines, l)
}

func TestReplayKeepsTheLabelsSetInside(t *testing.T) {
	progKey := RegisterKey(progIR, &Key[*prog]{Name: "t-prog-key", Of: func(p *prog) string { return strings.Join(p.tables, ",") }})
	body := Shared(progKey, Each(tablesSplit, Then(Of(build), Then(Seq(modIR, upper), PerFunc(funcsView, Seq(fnIR, fnFail))))))
	p := Fork(B("go", Then(body, Of(passThrough))), B("ts", Then(body, Of(passThrough))))
	shared, alone := &errRecorder{}, &errRecorder{}
	_, e1 := run(t, &Run{Observers: []Observer{shared}}, p, &prog{tables: []string{"a"}})
	_, e2 := run(t, &Run{Observers: []Observer{alone}, NoShare: true}, p, &prog{tables: []string{"a"}})
	if !reflect.DeepEqual(shared.lines, alone.lines) || fmt.Sprint(e1) != fmt.Sprint(e2) {
		t.Errorf("share\n%s\n%v\nno share\n%s\n%v", strings.Join(shared.lines, "\n"), e1, strings.Join(alone.lines, "\n"), e2)
	}
	if !contains(alone.lines, "t-fn-fail [ts/a/a2] failed") {
		t.Errorf("events\n%s", strings.Join(alone.lines, "\n"))
	}
}

// box is a pointer kind without a printer.
type box struct{ inner *fn }

var (
	boxIR = RegisterIR(&IR[*box]{Name: "t-box", Copy: func(b *box) *box { x := *b.inner; return &box{&x} }})
	boxed = New(Def[*box, *box]{Decl: Decl{Name: "t-boxed", Test: true}, In: boxIR, Out: boxIR,
		Run: func(_ *Ctx[*box], b *box) (*box, pass.PreservedSet, error) { return b, pass.PreserveAll(), nil }})
)

// inputs records the input dumps and the text comparisons.
type inputs struct {
	dumps []string
	text  []bool
}

func (r *inputs) Needs() Needs { return Needs{Before: true, TextChanged: true} }
func (r *inputs) Before(_ Event, in func(io.Writer)) bool {
	var b strings.Builder
	in(&b)
	r.dumps = append(r.dumps, b.String())
	return true
}
func (r *inputs) After(_ Event, res *Result) { r.text = append(r.text, res.TextChanged) }

func TestKindWithoutAPrinterHasNoText(t *testing.T) {
	if _, err := run(t, &Run{VerifyEach: true}, Seq(boxIR, boxed), &box{&fn{"f", "x"}}); err != nil {
		t.Errorf("-verify-each compares a text that the kind does not have: %v", err)
	}
	r := &inputs{}
	if _, err := run(t, &Run{Observers: []Observer{r}}, Seq(boxIR, boxed), &box{&fn{"f", "x"}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.dumps, []string{""}) || !reflect.DeepEqual(r.text, []bool{false}) {
		t.Errorf("dumps %q, text changed %v, want no dump and the pass's report", r.dumps, r.text)
	}
}

// decide asks for a fold once per function and changes nothing.
var decide = New(Def[*mod, *mod]{Decl: Decl{Name: "t-decide", Test: true, Patterns: []string{"t-fold"}}, In: modIR, Out: modIR,
	Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
		for range m.fns {
			c.Allow("t-fold")
		}
		return m, pass.PreserveAll(), nil
	}})

// maybe runs while maybeOn is set and changes nothing.
var (
	maybeOn bool
	maybe   = New(Def[*mod, *mod]{Decl: Decl{Name: "t-maybe", Test: true}, In: modIR, Out: modIR,
		Applies: func(*Ctx[*mod]) bool { return maybeOn },
		Run:     func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveAll(), nil }})
	// fnMaybe is maybe on one function.
	fnMaybe = New(Def[*fn, *fn]{Decl: Decl{Name: "t-fn-maybe", Test: true}, In: fnIR, Out: fnIR,
		Applies: func(*Ctx[*fn]) bool { return maybeOn },
		Run:     func(_ *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) { return f, pass.PreserveAll(), nil }})
)

func TestUnobservedRunAllocatesNothingPerStepBeyondTheContext(t *testing.T) {
	m := &mod{fns: []*fn{{"f", "x"}}}
	allocs := func(p Pipeline[*mod, *mod], m *mod) float64 {
		return testing.AllocsPerRun(50, func() { run(t, &Run{Stop: true}, p, m) })
	}
	p := Seq(modIR, maybe, maybe, maybe, maybe, maybe)
	maybeOn = false
	off := allocs(p, m)
	maybeOn = true
	on := allocs(p, m)
	maybeOn = false
	// The measured cost of running a step that nobody observes is 2 allocations (its context and
	// the typed context that Applies and Run share; the race detector adds a few per run); the bound
	// fails on one more per step, such as the input closure that observers get.
	if per := (on - off) / 5; per >= 3 {
		t.Errorf("%v allocations per step that runs, want 2", per)
	}
	many := &mod{fns: []*fn{{"f", "x"}, {"g", "x"}, {"h", "x"}, {"i", "x"}}}
	// A step run per function of a module costs the same per run as a step on the module.
	perFunc := PerFunc(funcsView, Seq(fnIR, fnMaybe, fnMaybe, fnMaybe, fnMaybe, fnMaybe))
	off = allocs(perFunc, many)
	maybeOn = true
	on = allocs(perFunc, many)
	maybeOn = false
	if per := (on - off) / (5 * float64(len(many.fns))); per >= 3 {
		t.Errorf("%v allocations per step that runs on a function, want 2", per)
	}
	if a, b := allocs(Seq(modIR, decide), m), allocs(Seq(modIR, decide), many); a != b {
		t.Errorf("a pattern decision allocates: %v allocations for 1 decision, %v for 4", a, b)
	}
}

func TestForkFromSeveralGoroutines(t *testing.T) {
	type unit struct{ s string }
	ir := RegisterIR(&IR[*unit]{Name: "t-race-unit"})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Fork(B("a", Seq(ir)), B("b", Seq(ir)))
		}()
	}
	wg.Wait()
}

var (
	fnSize = pass.NewAnalysisKey("t-fn-size", func(f *fn, _ *pass.AnalysisManager[*fn]) (int, error) {
		fnComputed++
		return len(f.body), nil
	})
	fnComputed int
	fnQuery    = New(Def[*fn, *fn]{Decl: Decl{Name: "t-fn-query", Test: true}, In: fnIR, Out: fnIR,
		Run: func(c *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) {
			_, err := Query(c, fnSize)
			return f, pass.PreserveAll(), err
		}})
	modSeen  []string
	modShape = pass.NewAnalysisKey("t-mod-text", func(m *mod, _ *pass.AnalysisManager[*mod]) (string, error) { return m.String(), nil })
	modQuery = New(Def[*mod, *mod]{Decl: Decl{Name: "t-mod-query", Test: true}, In: modIR, Out: modIR,
		Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
			s, err := Query(c, modShape)
			modSeen = append(modSeen, s)
			return m, pass.PreserveAll(), err
		}})
)

func TestQueryAfterAPartChanged(t *testing.T) {
	m := &mod{fns: []*fn{{"f", "x"}}}
	modSeen = nil
	p := Then3(Seq(modIR, modQuery), PerFunc(funcsView, Seq(fnIR, fnUpper)), Seq(modIR, modQuery))
	if _, err := run(t, &Run{Stop: true}, p, m); err != nil {
		t.Fatal(err)
	}
	if want := []string{"f: x\n", "f: X\n"}; !reflect.DeepEqual(modSeen, want) {
		t.Errorf("the queries saw %q, want %q: a stale analysis of the whole", modSeen, want)
	}
	// The analyses of each function live across the passes of a PerFunc segment.
	fnComputed = 0
	two := &mod{fns: []*fn{{"f", "x"}, {"g", "y"}}}
	if _, err := run(t, &Run{Stop: true}, PerFunc(funcsView, Seq(fnIR, fnQuery, fnQuery)), two); err != nil {
		t.Fatal(err)
	}
	if fnComputed != 2 {
		t.Errorf("%d computations of a function analysis, want one per function", fnComputed)
	}
}

var (
	fnSeen []string
	fnBody = pass.NewAnalysisKey("t-fn-body", func(f *fn, _ *pass.AnalysisManager[*fn]) (string, error) { return f.body, nil })
	fnRead = New(Def[*fn, *fn]{Decl: Decl{Name: "t-fn-read", Test: true}, In: fnIR, Out: fnIR,
		Run: func(c *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) {
			s, err := Query(c, fnBody)
			fnSeen = append(fnSeen, s)
			return f, pass.PreserveAll(), err
		}})
)

// Under Stop a transform of the whole changes its functions in place, so they keep their identity:
// their analyses must go with the change.
func TestWholeChangesItsPartsInPlace(t *testing.T) {
	m := &mod{fns: []*fn{{"f", "x"}}}
	fnSeen = nil
	p := Then3(PerFunc(funcsView, Seq(fnIR, fnRead)), Seq(modIR, upper), PerFunc(funcsView, Seq(fnIR, fnRead)))
	if _, err := run(t, &Run{Stop: true}, p, m); err != nil {
		t.Fatal(err)
	}
	if want := []string{"x", "X"}; !reflect.DeepEqual(fnSeen, want) {
		t.Errorf("the function queries saw %q, want %q: a stale analysis of a part", fnSeen, want)
	}
}

func TestStageEndVerificationRollsBack(t *testing.T) {
	in := func() *mod { return &mod{fns: []*fn{{"f", "x"}}} }
	if out, err := run(t, &Run{}, Seq(modIR, upper, breaker), in()); err == nil || out.String() != "f: x\n" {
		t.Errorf("a segment that fails its verification: %v %v, want its start", out, err)
	}
	// A required transform ends the segment: there is nothing to roll back to.
	if out, err := run(t, &Run{}, Seq(modIR, upper, breaker, mark), in()); out != nil || err == nil {
		t.Errorf("an invalid unit went on: %v %v", out, err)
	}
	if out, err := run(t, &Run{Stop: true}, Seq(modIR, upper, breaker), in()); out != nil || err == nil {
		t.Errorf("under Stop a failed verification did not break the run: %v %v", out, err)
	}
}

// Required transforms are trusted: outside -verify-each a run of them only is not verified after it,
// as the stages before the manager did not; verification is there to catch the optional passes.
// -verify-each verifies after every step, the required ones included.
func TestRequiredTransformsAreVerifiedOnlyUnderVerifyEach(t *testing.T) {
	in := func() *mod { return &mod{fns: []*fn{{"f", "x"}}} }
	verifies = 0
	out, err := run(t, &Run{}, Seq(modIR, mark, requiredBreaker), in())
	if err != nil || out == nil || out.String() != "f: bad\n" {
		t.Errorf("a run of required transforms: %v %v", out, err)
	}
	if verifies != 0 {
		t.Errorf("%d verifications after a run of required transforms, want 0", verifies)
	}
	verifies = 0
	if _, err := run(t, &Run{}, Seq(modIR, upper, mark), in()); err != nil || verifies != 1 {
		t.Errorf("a run with an optional pass: %v, %d verifications, want 1", err, verifies)
	}
	_, err = run(t, &Run{VerifyEach: true}, Seq(modIR, mark, requiredBreaker), in())
	var se *StepError
	if !errors.As(err, &se) || se.Step != "t-break-required" {
		t.Errorf("under -verify-each a broken required transform: %v", err)
	}
}

var fnNever = New(Def[*fn, *fn]{Decl: Decl{Name: "t-fn-never", Test: true}, In: fnIR, Out: fnIR,
	Applies: func(*Ctx[*fn]) bool { return false },
	Run:     func(_ *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) { return f, pass.PreserveNone(), nil }})

func TestPerFuncCopiesOnlyWhenAPassRuns(t *testing.T) {
	m := &mod{fns: []*fn{{"f", "x"}, {"g", "y"}}}
	if n := copiesOf(func() { run(t, &Run{}, PerFunc(funcsView, Seq(fnIR, fnNever)), m) }); n != 0 {
		t.Errorf("%d copies of a segment in which no pass runs", n)
	}
	// A kept unit is copied once; the result belongs to the run, so the next segment copies no more.
	if n := copiesOf(func() { run(t, &Run{Keep: true}, Then(PerFunc(funcsView, Seq(fnIR, fnUpper)), Seq(modIR, mark)), m) }); n != 1 {
		t.Errorf("%d copies, want 1", n)
	}
}

func TestFillKeepsTheRequiredSteps(t *testing.T) {
	p, err := Apply(Slot(modSlot, Seq(modIR, mark, upper)), Fill(modSlot, "", Seq(modIR, upper)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, &Run{}, p, &mod{fns: []*fn{{"f", "x"}}}); err == nil || !strings.Contains(err.Error(), "changes the required steps") {
		t.Errorf("a fill that drops a required step: %v", err)
	}
}

var unflagged = New(Def[*mod, *mod]{Decl: Decl{Name: "t-unflagged"}, In: modIR, Out: modIR,
	Run: func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveAll(), nil }})

func TestUnregisteredFlagFailsTheCheck(t *testing.T) {
	builds = 0
	_, err := run(t, &Run{}, Then(Of(build), Seq(modIR, unflagged)), table{"a"})
	if !errors.Is(err, errCheck) || builds != 0 {
		t.Errorf("an optional pass without a flag: %v, %d steps ran", err, builds)
	}
}

// TestGroupingMeansNothing checks the normal form: a sequence inside a chain and a body of one
// pass run as the flat sequence, so the grouping of the combinators does not move a verification.
// Without it, Then(Seq(upper), breaker!) verified after t-upper only and returned the broken unit,
// and a body Of(breaker) was not verified at all.
func TestGroupingMeansNothing(t *testing.T) {
	grouped := Then(Seq(modIR, upper), Of(requiredBreaker))
	flat := Seq(modIR, upper, requiredBreaker)
	in := func() *mod { return &mod{fns: []*fn{{"f", "x"}}} }
	_, e1 := run(t, &Run{}, grouped, in())
	_, e2 := run(t, &Run{}, flat, in())
	if e1 == nil || e2 == nil || e1.Error() != e2.Error() {
		t.Errorf("grouped: %v; flat: %v; want the same verification failure", e1, e2)
	}
	one := Within(pairLens, Of(breaker))
	seq := Within(pairLens, Seq(modIR, breaker))
	pin := func() *pair { return &pair{m: in(), tag: "t"} }
	o1, e1 := run(t, &Run{}, one, pin())
	o2, e2 := run(t, &Run{}, seq, pin())
	if e1 == nil || e2 == nil || o1 == nil || o2 == nil || o1.m.String() != "f: x\n" || o2.m.String() != "f: x\n" {
		t.Errorf("a body of one pass: %v %v, %v %v; want both verified and rolled back", o1, e1, o2, e2)
	}
}

// posts counts the runs of the postcondition of postBuild; postBad makes it fail.
var (
	posts   int
	postBad bool
)

// build-post! is build! with a postcondition of its own, which checks less than the verifier of
// t-mod: the invariants it makes by construction.
var postBuild = New(Def[table, *mod]{Decl: Decl{Name: "t-build-post", Required: true, Obligation: "lowers a table"},
	In: tableIR, Out: modIR,
	Run: func(_ *Ctx[table], t table) (*mod, pass.PreservedSet, error) {
		return &mod{fns: []*fn{{t.name + "1", "x"}}}, pass.PreserveNone(), nil
	},
	Post: func(*mod) error {
		posts++
		if postBad {
			return errors.New("not as built")
		}
		return nil
	}})

// A conversion's postcondition replaces the verifier of its output kind at its own output, and only
// there: the end of the run of transforms after it, and every pass under -verify-each, are checked
// by the kind's verifier.
func TestConversionPostconditionReplacesTheKindVerifierAtItsOutput(t *testing.T) {
	in := func() *prog { return &prog{tables: []string{"a"}} }
	posts, verifies, postBad = 0, 0, false
	if _, err := run(t, &Run{}, tablePipe(Seq(modIR, upper)), in()); err != nil || verifies != 2 {
		t.Fatalf("without a postcondition: %v, %d verifications, want 2 (the conversion and the stage end)", err, verifies)
	}
	posts, verifies = 0, 0
	if _, err := run(t, &Run{}, Each(tablesSplit, Then(Of(postBuild), Seq(modIR, upper))), in()); err != nil || posts != 1 || verifies != 1 {
		t.Errorf("with a postcondition: %v, %d postconditions and %d verifications, want 1 and 1", err, posts, verifies)
	}
	posts, verifies = 0, 0
	if _, err := run(t, &Run{VerifyEach: true}, Each(tablesSplit, Then(Of(postBuild), Seq(modIR, upper))), in()); err != nil || posts != 1 || verifies != 1 {
		t.Errorf("under -verify-each: %v, %d postconditions and %d verifications, want 1 and 1", err, posts, verifies)
	}
	// The kind's verifier still checks the unit where the transforms after the conversion end.
	_, err := run(t, &Run{}, Each(tablesSplit, Then(Of(postBuild), Seq(modIR, breaker))), in())
	var se *StepError
	if !errors.As(err, &se) || se.Step != "verify t-mod" {
		t.Errorf("a transform that breaks the unit after the conversion: %v", err)
	}
	// A failing postcondition names the conversion.
	postBad = true
	defer func() { postBad = false }()
	_, err = run(t, &Run{}, Each(tablesSplit, Then(Of(postBuild), Seq(modIR, upper))), in())
	if !errors.As(err, &se) || se.Step != "t-build-post" {
		t.Errorf("a failing postcondition: %v, want it to name the conversion", err)
	}
}

func TestTransformWithPostconditionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a transform with a postcondition of its own was registered")
		}
	}()
	New(Def[*mod, *mod]{Decl: Decl{Name: "t-post-transform", Test: true}, In: modIR, Out: modIR,
		Run:  func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveNone(), nil },
		Post: func(*mod) error { return nil }})
}

// agreeKey keys every table alike; the table c disagrees with any other.
var agreeKey = RegisterKey(tableIR, &Key[table]{Name: "t-agree-key", Of: func(table) string { return "one" },
	Agree: func(first, later table) error {
		if first.name == "c" || later.name == "c" {
			return fmt.Errorf("the tables %s and %s disagree", first.name, later.name)
		}
		return nil
	}})

// A later use of a Shared key gets the result of the earlier input only if the key's Agree accepts
// it; an input that disagrees fails the run and names the key.
func TestSharedKeyAgreement(t *testing.T) {
	p := Each(tablesSplit, Shared(agreeKey, Of(build)))
	builds = 0
	if out, err := run(t, &Run{}, p, &prog{tables: []string{"a", "b"}}); err != nil || builds != 1 || out.String() != "[a]\na1: x\na2: y\n[b]\na1: x\na2: y\n" {
		t.Errorf("tables that agree: %v, %d builds, %q; want one build that both get", err, builds, out)
	}
	_, err := run(t, &Run{}, p, &prog{tables: []string{"a", "c"}})
	var se *StepError
	if !errors.As(err, &se) || se.Step != "key t-agree-key" || se.Label.Table != "c" || !strings.Contains(err.Error(), "the tables a and c disagree") {
		t.Errorf("tables that disagree: %v", err)
	}
	// Without sharing every use runs the body on its own input: there is nothing to agree on.
	if _, err := run(t, &Run{NoShare: true}, p, &prog{tables: []string{"a", "c"}}); err != nil {
		t.Errorf("without sharing: %v", err)
	}
}
