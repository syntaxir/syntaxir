package pass

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

// text is a unit of a second IR, which the lowering of a unit makes.
type text struct{ s string }

var (
	unitIR = IR[*unit]{Stage: "u", Verify: func(u *unit) error {
		if u.broken != "" {
			return errors.New("broken by " + u.broken)
		}
		return nil
	}, Print: func(w io.Writer, l Label, after string, u *unit) {
		fmt.Fprintf(w, "%s/%s after %s: %s\n", l.Table, l.Func, after, strings.Join(u.log, ","))
	}}
	textIR = IR[*text]{Stage: "t", Verify: func(t *text) error {
		if strings.Contains(t.s, "bad") {
			return errors.New("bad text")
		}
		return nil
	}, Print: func(w io.Writer, l Label, after string, t *text) {
		fmt.Fprintf(w, "%s after %s: %s\n", l.Func, after, t.s)
	}}
)

// recorder is an observer that records the events and the dumps, and skips the runs whose
// numbers are in skip.
type recorder struct {
	events []string
	dumps  strings.Builder
	skip   map[int]bool
}

func (o *recorder) Before(e Event) bool { return !o.skip[e.Seq] }

func (o *recorder) After(e Event, print func(io.Writer), _ any) {
	o.events = append(o.events, fmt.Sprintf("%d:%s:%s:%s", e.Seq, e.Step.Name, e.Step.Dump, e.Label.Func))
	print(&o.dumps)
}

func transforms(p *Pipeline[*unit, mode]) Stage[*unit, *unit] {
	return Transforms(Transform[*unit, mode]{IR: unitIR, Pipeline: p,
		Env: func(*Run, Label, *unit) mode { return mode{} }})
}

func lowerText(name string) Stage[*unit, *text] {
	return Lower(name, "u", textIR, func(u *unit) (*text, error) { return &text{s: strings.Join(u.log, "+")}, nil })
}

// exclaim is a required step that appends "!" to a copy of the text.
func exclaim(x *text) *text { return &text{s: x.s + "!"} }

func TestStageComposition(t *testing.T) {
	s := Then(Then(transforms(fakePipeline(logging("a", nil, none), logging("b", nil, none))), lowerText("lower")),
		Do("finish", textIR, exclaim))
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, st := range s.Steps() {
		names = append(names, fmt.Sprintf("%s:%v:%s>%s", st.Name, st.Required, st.In, st.Out))
	}
	if want := []string{"a:false:u>u", "b:false:u>u", "lower:true:u>t", "finish:true:t>t"}; !slices.Equal(names, want) {
		t.Errorf("steps %v, want %v", names, want)
	}
	rec := &recorder{}
	r := &Run{Observers: []Observer{rec}}
	out, err := s.Run(r, Label{Table: "rules", Func: "f"}, &unit{})
	if err != nil || out.s != "a+b+analyze!" {
		t.Fatalf("got %q, %v", out.s, err)
	}
	// The final verification computes the analysis that it requires, after the last dump. A lowering
	// and a required step are dumped under their names.
	if want := []string{"1:a:a:f", "2:b:b:f", "0:lower:lower:f", "0:finish:finish:f"}; !slices.Equal(rec.events, want) {
		t.Errorf("events %v, want %v", rec.events, want)
	}
	if want := "rules/f after a: a\nrules/f after b: a,b\nf after lower: a+b+analyze\nf after finish: a+b+analyze!\n"; rec.dumps.String() != want {
		t.Errorf("dumps %q, want %q", rec.dumps.String(), want)
	}
}

func TestStageCheck(t *testing.T) {
	twice := Then(transforms(fakePipeline(logging("a", nil, none))), transforms(fakePipeline(logging("a", nil, none))))
	if err := twice.Check(); err == nil || !strings.Contains(err.Error(), "step a is in the pipeline twice") {
		t.Errorf("duplicate name: %v", err)
	}
	// A lowering declared from the wrong stage: the types meet, the stages do not.
	wrong := Then(lowerText("lower"), Lower("again", "u", textIR, func(x *text) (*text, error) { return x, nil }))
	if err := wrong.Check(); err == nil || !strings.Contains(err.Error(), `step again takes the stage "u" but follows lower, which returns "t"`) {
		t.Errorf("stage mismatch: %v", err)
	}
}

// TestStageNumbering checks that the runs of optional passes are numbered in the order they
// happen across units, once per unit, and that an observer skips a run by its number.
func TestStageNumbering(t *testing.T) {
	s := transforms(fakePipeline(logging("a", nil, none), logging("b", nil, none)))
	rec := &recorder{skip: map[int]bool{3: true}}
	r := &Run{Observers: []Observer{rec}, Enabled: func(n string) bool { return true }}
	if _, err := s.Run(r, Label{Func: "f"}, &unit{}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Run(r, Label{Func: "g"}, &unit{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1:a:a:f", "2:b:b:f", "4:b:b:g"}; !slices.Equal(rec.events, want) {
		t.Errorf("events %v, want %v", rec.events, want)
	}
	if !slices.Equal(v.log, []string{"b", "analyze"}) || r.Runs() != 4 {
		t.Errorf("second unit %v, runs %d", v.log, r.Runs())
	}
	// A pass that the settings turn off takes no number.
	r = &Run{Enabled: func(n string) bool { return n != "a" }}
	if _, err := s.Run(r, Label{}, &unit{}); err != nil || r.Runs() != 1 {
		t.Errorf("runs %d, %v", r.Runs(), err)
	}
}

// TestStageFailure checks that after a failing pass only the required steps run, and that the
// first error is the one returned.
func TestStageFailure(t *testing.T) {
	failing := Pass[*unit, mode]{Name: "bad", Run: func(u *unit, _ mode) (*unit, PreservedSet, error) {
		return u.with("half"), none, errors.New("no")
	}}
	later := transforms(fakePipeline(logging("c", nil, none)))
	s := Then(Then(Then(transforms(fakePipeline(logging("a", nil, none), failing)), lowerText("lower")),
		Do("finish", textIR, exclaim)), Lower("back", "t", unitIR, func(x *text) (*unit, error) { return &unit{log: []string{x.s}}, nil }))
	s2 := Then(s, later)
	rec := &recorder{}
	out, err := s2.Run(&Run{Observers: []Observer{rec}}, Label{}, &unit{})
	if err == nil || err.Error() != "pass bad: no" {
		t.Fatalf("error %v", err)
	}
	// The required steps go on with the unit as the passes left it: what a made, without the unit
	// that bad returned with its failure.
	if !slices.Equal(out.log, []string{"a!"}) {
		t.Errorf("unit %v, want [a!]: the optional pass c ran after the failure, or not the unit that bad was given went on", out.log)
	}
	if want := []string{"1:a:a:", "0:lower:lower:", "0:finish:finish:", "0:back:back:"}; !slices.Equal(rec.events, want) {
		t.Errorf("events %v, want %v", rec.events, want)
	}
}

// TestStageLoweringPostcondition checks that the verifier of a lowering's output runs right after
// it, that the error names the lowering, and that nothing runs after it.
func TestStageLoweringPostcondition(t *testing.T) {
	ran := false
	s := Then(Lower("lower", "u", textIR, func(u *unit) (*text, error) { return &text{s: "bad"}, nil }),
		Do("finish", textIR, func(x *text) *text { ran = true; return x }))
	out, err := s.Run(&Run{}, Label{}, &unit{})
	var se *StepError
	if !errors.As(err, &se) || se.Step.Name != "lower" || err.Error() != "bad text" {
		t.Fatalf("error %v", err)
	}
	if out != nil || ran {
		t.Errorf("a step ran after the failed lowering")
	}
}

// TestStageVerifyEach checks that the verification after every pass names the pass that broke the
// unit, and that the stage returns the unit that failed it.
func TestStageVerifyEach(t *testing.T) {
	breaks := Pass[*unit, mode]{Name: "breaks", Run: func(u *unit, _ mode) (*unit, PreservedSet, error) {
		v := u.with("breaks")
		v.broken = "breaks"
		return v, none, nil
	}}
	s := transforms(fakePipeline(logging("a", nil, none), breaks, logging("c", nil, none)))
	u, err := s.Run(&Run{VerifyEach: true}, Label{}, &unit{})
	if err == nil || err.Error() != "after pass breaks: broken by breaks" || slices.Contains(u.log, "c") {
		t.Errorf("error %v, log %v", err, u.log)
	}
	if u.broken != "breaks" {
		t.Errorf("the stage did not return the unit that breaks made: %v", u.log)
	}
}

// pair is a unit with a part, the unit of a projected stage.
type pair struct {
	part *unit
	tag  string
}

// TestStageProject checks that a projected stage runs on the part, and that the unit it returns holds
// the part the stage returned, also when the stage changed the part in place.
func TestStageProject(t *testing.T) {
	s := Project(func(p *pair) *unit { return p.part }, func(p *pair, u *unit) (*pair, error) { return &pair{part: u, tag: p.tag}, nil },
		transforms(fakePipeline(logging("a", nil, none))))
	in := &pair{part: &unit{}, tag: "x"}
	out, err := s.Run(&Run{}, Label{}, in)
	if err != nil {
		t.Fatal(err)
	}
	if out == in || out.tag != "x" || !slices.Equal(out.part.log, []string{"a", "analyze"}) {
		t.Errorf("out %v (same as in: %v), part log %v", out, out == in, out.part.log)
	}
	// A stage that changes its part in place returns the same part, which put still takes: the
	// unit may have to check it.
	var put []*unit
	same := Project(func(p *pair) *unit { return p.part }, func(p *pair, u *unit) (*pair, error) {
		put = append(put, u)
		return &pair{part: u, tag: p.tag + "!"}, nil
	}, transforms(fakePipeline(inPlace("b"))))
	in = &pair{part: &unit{}, tag: "x"}
	part := in.part
	if out, err := same.Run(&Run{}, Label{}, in); err != nil || len(put) != 1 || put[0] != part || out.part != part || out.tag != "x!" || !slices.Equal(part.log, []string{"b", "analyze"}) {
		t.Errorf("part changed in place: out %v, put %v, part log %v, err %v", out, put, part.log, err)
	}
	// A part that put refuses fails the stage, which returns the unit as it was.
	cause := errors.New("refused")
	refuses := Project(func(p *pair) *unit { return p.part }, func(*pair, *unit) (*pair, error) { return nil, cause },
		transforms(fakePipeline(logging("a", nil, none))))
	if out, err := refuses.Run(&Run{}, Label{}, in); err != cause || out != in {
		t.Errorf("refused part: out %v, err %v", out, err)
	}
}

// copyUnit is unitIR with a copy that counts in n.
func copyUnit(n *int) IR[*unit] {
	ir := unitIR
	ir.Copy = func(u *unit) *unit {
		*n++
		v := *u
		v.log = slices.Clone(u.log)
		return &v
	}
	return ir
}

// TestStageConsumesItsUnit checks that a stage copies nothing outside VerifyEach: its passes and a
// required step change the unit that the stage was given in place, whether the passes before ran,
// failed, or a lowering made the unit, and the stage returns that unit. Only passes that keep their
// unit copy it, once, and the step after them goes on with the unit as they got it if one fails.
func TestStageConsumesItsUnit(t *testing.T) {
	var passCopies, stepCopies int
	finish := Do("finish", copyUnit(&stepCopies), func(u *unit) *unit {
		u.log = append(u.log, "!")
		return u
	})
	segment := func(passes ...Pass[*unit, mode]) Stage[*unit, *unit] {
		return transforms(copying(fakePipeline(passes...), &passCopies))
	}
	kept := func(passes ...Pass[*unit, mode]) Stage[*unit, *unit] {
		return Transforms(Transform[*unit, mode]{IR: unitIR, Pipeline: copying(fakePipeline(passes...), &passCopies), Keep: true,
			Env: func(*Run, Label, *unit) mode { return mode{} }})
	}
	check := func(what string, s Stage[*unit, *unit], log []string, wantErr, same bool, copies int) {
		t.Helper()
		passCopies, stepCopies = 0, 0
		in := &unit{log: []string{"in"}}
		out, err := s.Run(&Run{}, Label{}, in)
		if (err != nil) != wantErr || !slices.Equal(out.log, log) || (out == in) != same {
			t.Errorf("%s: log %v, the input returned %v, err %v; want %v, the input %v", what, out.log, out == in, err, log, same)
		}
		if passCopies != copies || stepCopies != 0 {
			t.Errorf("%s: %d copies by the passes and %d by the step, want %d and none", what, passCopies, stepCopies, copies)
		}
	}
	check("passes, then a step", Then(segment(inPlace("a"), inPlace("b")), finish), []string{"in", "a", "b", "analyze", "!"}, false, true, 0)
	check("a step on the caller's unit", finish, []string{"in", "!"}, false, true, 0)
	made := Lower("make", "u", unitIR, func(u *unit) (*unit, error) { return &unit{log: append(slices.Clone(u.log), "made")}, nil })
	check("a step after a lowering", Then(made, finish), []string{"in", "made", "!"}, false, false, 0)
	fails := Pass[*unit, mode]{Name: "fails", Run: func(u *unit, _ mode) (*unit, PreservedSet, error) {
		u.log = append(u.log, "half")
		return u, none, errors.New("no")
	}}
	check("a step after a failed pass", Then(segment(inPlace("a"), fails), finish), []string{"in", "a", "half", "!"}, true, true, 0)
	check("a step after kept passes", Then(kept(inPlace("a"), inPlace("b")), finish), []string{"in", "a", "b", "analyze", "!"}, false, false, 1)
	check("a step after a failed kept pass", Then(kept(inPlace("a"), fails), finish), []string{"in", "!"}, true, true, 1)
}

// keeper is an observer that keeps every unit it is given, with the log the unit had then.
type keeper struct {
	units []*unit
	logs  [][]string
}

func (*keeper) Before(Event) bool { return true }

func (k *keeper) After(_ Event, _ func(io.Writer), u any) {
	if u, ok := u.(*unit); ok {
		k.units = append(k.units, u)
		k.logs = append(k.logs, slices.Clone(u.log))
	}
}

// TestStageVerifyEachKeepsObservedUnits checks that under VerifyEach a required step changes no unit
// that an observer received, whether the passes before it ran or a lowering made the unit.
func TestStageVerifyEachKeepsObservedUnits(t *testing.T) {
	var passCopies, stepCopies int
	finish := Do("finish", copyUnit(&stepCopies), func(u *unit) *unit {
		u.log = append(u.log, "fin")
		return u
	})
	made := Lower("make", "u", unitIR, func(u *unit) (*unit, error) { return &unit{log: []string{"made"}}, nil })
	// The pipeline computes no analysis, which would add to the log of the unit it verifies.
	plain := &Pipeline[*unit, mode]{Passes: []Pass[*unit, mode]{inPlace("p")}, Verify: unitIR.Verify}
	for _, c := range []struct {
		what string
		s    Stage[*unit, *unit]
	}{
		{"passes, then a step", Then(Then(made, transforms(copying(plain, &passCopies))), finish)},
		{"a step after a lowering", Then(made, finish)},
	} {
		k := &keeper{}
		out, err := c.s.Run(&Run{VerifyEach: true, Observers: []Observer{k}}, Label{}, &unit{})
		if err != nil {
			t.Fatal(err)
		}
		if out.log[len(out.log)-1] != "fin" {
			t.Errorf("%s: the step did not run: %v", c.what, out.log)
		}
		for i, u := range k.units[:len(k.units)-1] {
			if !slices.Equal(u.log, k.logs[i]) {
				t.Errorf("%s: unit %d that the observer got was changed later: %v, was %v", c.what, i, u.log, k.logs[i])
			}
		}
	}
}

// A stage that keeps a pointer unit it was given needs a Copy to keep it with: without one its passes
// would change the caller's unit in place. Such a stage reports the error from Check, its run returns
// the error without running a step, and so does a stage composed of it; the same stage with a Copy, or
// without Keep, is fine.
func TestKeepNeedsACopyOfAPointerUnit(t *testing.T) {
	keptWithout := Transforms(Transform[*unit, mode]{IR: unitIR, Pipeline: fakePipeline(inPlace("a")), Keep: true,
		Env: func(*Run, Label, *unit) mode { return mode{} }})
	if err := keptWithout.Check(); err == nil || !strings.Contains(err.Error(), "no Copy") {
		t.Fatalf("Check of a kept pointer unit without a Copy: %v", err)
	}
	in := &unit{log: []string{"in"}}
	if out, err := keptWithout.Run(&Run{}, Label{}, in); err == nil || out != nil || !slices.Equal(in.log, []string{"in"}) {
		t.Errorf("Run: returned %v, err %v, the input log %v; want the error, no unit, and the input untouched", out, err, in.log)
	}
	composed := Then(keptWithout, Do("finish", unitIR, func(u *unit) *unit { return u }))
	if err := composed.Check(); err == nil {
		t.Errorf("Check of a stage composed with a kept pointer unit without a Copy passed")
	}
	withoutKeep := Transforms(Transform[*unit, mode]{IR: unitIR, Pipeline: fakePipeline(inPlace("a")),
		Env: func(*Run, Label, *unit) mode { return mode{} }})
	if err := withoutKeep.Check(); err != nil {
		t.Errorf("a stage without Keep: %v", err)
	}
}
