package driver

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

func checkSource(t *testing.T, src string, o common.CompileOptions, start string) (*Program, error) {
	t.Helper()
	g, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return Check(g, o, start)
}

// Check gives the program its start rule, the grammar's default when none is named, and rejects a
// start rule that the grammar does not mark.
func TestCheckStartRule(t *testing.T) {
	t.Parallel()
	const src = "@start(default)\nrule A = \"a\" B\n@start\nrule B = \"b\"\n"
	p, err := checkSource(t, src, common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Start().Name != "A" || p.View() == nil {
		t.Errorf("default start = %s, want A", p.Start().Name)
	}
	if p, err = checkSource(t, src, common.CompileOptions{SourceInfo: true}, "B"); err != nil || p.Start().Name != "B" || !p.Options().SourceInfo {
		t.Fatalf("start B: %v", err)
	}
	if _, err := checkSource(t, src, common.CompileOptions{}, "C"); err == nil {
		t.Error("an unknown start rule was accepted")
	}
	rec, err := p.Recognizer()
	if err != nil || rec == nil {
		t.Fatalf("recognizer: %v", err)
	}
	if again, _ := p.Recognizer(); again != rec {
		t.Error("the recognizer was checked twice")
	}
}

// Compile fails before any step for a target whose options disagree with the program or the call, and
// takes one target until the targets are forked.
func TestCompileChecksTheTargets(t *testing.T) {
	t.Parallel()
	p, err := checkSource(t, "@start\nrule A = \"a\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Settings: speed}
	for _, c := range []struct {
		name    string
		targets []Target
		want    string
	}{
		{"no target", nil, "one target, not 0"},
		{"two targets", []Target{{Backend: result.Go}, {Backend: result.TypeScript}}, "one target, not 2"},
		{"start", []Target{{Backend: result.Go, Gen: common.GenOptions{Start: "B"}}}, "target go: Start"},
		{"source info", []Target{{Backend: result.Go, Gen: common.GenOptions{SourceInfo: true}}}, "target go: SourceInfo"},
		{"verify", []Target{{Backend: result.Go, Gen: common.GenOptions{Debug: common.Debug{VerifyEach: true}}}}, "target go: Debug"},
		{"settings", []Target{{Backend: result.Go, Gen: common.GenOptions{Optimize: "size"}}}, "target go: Optimize"},
		{"unknown profile", []Target{{Backend: result.Go, Gen: common.GenOptions{Optimize: "nope"}}}, "nope"},
		{"no pipeline", []Target{{Backend: result.VM}}, "no pipeline is assembled for the backend vm"},
	} {
		if _, err := Compile(context.Background(), p, o, c.targets...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	// The debugging settings of the call are checked before any step.
	if _, err := Compile(context.Background(), p, Options{Settings: speed, Debug: common.Debug{PrintAfter: []string{"elaborate"}}}, Target{Backend: result.Go}); err == nil || !strings.Contains(err.Error(), "need an Output writer") {
		t.Errorf("print after without Output: err = %v", err)
	}
	// Fields equal to the program's and the call's are accepted.
	equal := Target{Backend: result.VM, Gen: common.GenOptions{Start: "A", Optimize: "speed", Opt: map[string]int{"guards": 1}}}
	if _, err := Compile(context.Background(), p, o, equal); err == nil || !strings.Contains(err.Error(), "no pipeline") {
		t.Errorf("equal fields: err = %v, want the missing pipeline", err)
	}
}

// Settings are equal when every registered optimization has the same value, however they were given.
func TestSettingsEqual(t *testing.T) {
	t.Parallel()
	a, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenSettings(common.GenOptions{Optimize: "speed", Opt: map[string]int{"guards": 1}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := GenSettings(common.GenOptions{NoOpt: []string{"guards"}})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Equal(b) || a.Equal(c) || !c.Equal(c) {
		t.Errorf("Equal: speed=speed+guards %v, speed=no-guards %v", a.Equal(b), a.Equal(c))
	}
}

// The observers of a compilation write the dumps of the program first and then those of each table
// together, in the order of their first dumps, and only the steps that PrintAfter names.
func TestDumpsGroupByTable(t *testing.T) {
	t.Parallel()
	var w bytes.Buffer
	dump := func(names []string, events []struct{ step, table string }) {
		d := debugger(Options{Debug: common.Debug{PrintAfter: names, Output: &w}})
		obs := d.observers("")
		for _, e := range events {
			step := e.step
			obs[0].After(pipe.Event{Step: pipe.StepInfo{Name: step, Instance: step}, Label: pipe.Label{Table: e.table}},
				&pipe.Result{Ran: true, Print: func(w2 io.Writer) { w2.Write([]byte(step + "\n")) }})
		}
		if err := d.flush(); err != nil {
			t.Fatal(err)
		}
	}
	dump([]string{PrintAll}, []struct{ step, table string }{
		{"elaborate", "rules"}, {"guards", "rules"}, {"elaborate", "typed"}, {"plan", ""},
		{"flowlower", "rules"}, {"flowlower", "typed"}, {"elaborate", "recRules"},
	})
	want := `-- after elaborate [rules] --
elaborate
-- after guards [rules] --
guards
-- after flowlower [rules] --
flowlower
-- after elaborate [typed] --
elaborate
-- after flowlower [typed] --
flowlower
-- after elaborate [recRules] --
elaborate
`
	if w.String() != want {
		t.Errorf("dumps:\n%s\nwant (plan is dumped only when named):\n%s", w.String(), want)
	}
	w.Reset()
	dump([]string{"plan", "elaborate"}, []struct{ step, table string }{{"elaborate", "rules"}, {"plan", ""}, {"flowlower", "rules"}})
	if want := "-- after plan [program] --\nplan\n-- after elaborate [rules] --\nelaborate\n"; w.String() != want {
		t.Errorf("dumps:\n%s\nwant:\n%s", w.String(), want)
	}
}
