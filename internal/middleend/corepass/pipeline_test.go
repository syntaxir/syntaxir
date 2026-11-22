package corepass

import (
	"context"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The declarations of the pipeline can be run: the manager accepts it (names, After), and every
// pass states what it preserves.
func TestPipelineIsValid(t *testing.T) {
	if _, _, err := Run(&core.Module{}, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	want := []string{PassGuards, PassScans}
	if got := Names(); !slices.Equal(got, want) {
		t.Fatalf("passes %v, want %v", got, want)
	}
	for _, p := range Passes {
		if p.Decl().Obligation == "" {
			t.Errorf("pass %s states no obligation", p.Name())
		}
	}
}

// The passes know no mode: a flag that is off removes its pass, and nothing else does.
func TestPipelineAppliesByFlag(t *testing.T) {
	all := []string{PassGuards, PassScans}
	off := func(names ...string) pass.Settings {
		s, err := pass.Settings{}.Without(names...)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	cases := []struct {
		name string
		opt  pass.Settings
		want []string
	}{
		{"default", pass.Settings{}, all},
		{"without guards", off(PassGuards), []string{PassScans}},
		{"without scans", off(PassScans), []string{PassGuards}},
		{"nothing", off(PassGuards, PassScans), nil},
	}
	for _, c := range cases {
		m := &core.Module{}
		_, got, err := Run(m, RunOptions{Settings: c.opt})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: ran %v, want %v", c.name, got, c.want)
		}
	}
}

// A run consumes its module: the passes that ran changed it in place, and the run returns it, not a copy. Under VerifyEach each
// pass takes a copy, so the module is returned unchanged in another one.
func TestRunTakesItsModule(t *testing.T) {
	build := func() *core.Module {
		return analyzed(t, module(rule(0, "a", choice(lit("ab"), lit("cd")))))
	}
	m := build()
	out, ran, err := Run(m, RunOptions{})
	if err != nil || len(ran) == 0 {
		t.Fatalf("ran %v, err %v", ran, err)
	}
	if out != m {
		t.Error("the run returned a copy of its module")
	}
	m = build()
	before := core.Print(m, true)
	out, _, err = Run(m, RunOptions{VerifyEach: true})
	if err != nil || out == m || core.Print(m, true) != before {
		t.Errorf("VerifyEach: the module returned %v, the module changed %v, err %v", out == m, core.Print(m, true) != before, err)
	}
}

// The verifier of the Core kind checks the attributes of the nodes and never writes them: it rejects
// a module whose attributes are not those that core.Analyze computes, and leaves it as it was, so a
// pass that leaves stale attributes fails under VerifyEach, named.
func TestVerifyChecksTheAttributes(t *testing.T) {
	m := analyzed(t, module(rule(0, "a", choice(lit("ab"), &core.Opt{Body: lit("cd")}))))
	if err := Verify(m); err != nil {
		t.Fatalf("Verify of an analyzed module: %v", err)
	}
	stale := func(m *core.Module) { m.Rules[0].Body.Attrs().Nullable = false }
	stale(m)
	before := core.Print(m, true)
	if err := Verify(m); err == nil || !strings.Contains(err.Error(), "rule a: ") {
		t.Errorf("Verify of a module with a stale attribute: %v, want an error that names rule a", err)
	}
	if core.Print(m, true) != before {
		t.Error("Verify wrote the attributes of the module")
	}

	stalePass := pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "stale-attributes", Test: true, Obligation: "Leaves a stale attribute (test)."},
		In:   IR, Out: IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			stale(m)
			return m, pass.PreserveNone(), nil
		},
	})
	m = analyzed(t, module(rule(0, "a", choice(lit("ab"), &core.Opt{Body: lit("cd")}))))
	_, err := pipe.Execute(context.Background(), &pipe.Run{VerifyEach: true, Stop: true}, pipe.Seq(IR, stalePass), m)
	if err == nil || !strings.Contains(err.Error(), "stale-attributes") {
		t.Errorf("a pass that leaves a stale attribute: %v, want an error that names it", err)
	}
}

// The attributes of the nodes are content of a Core module, not an analysis that a pass computes on
// demand: no pass of the pipeline computes them, and a pass that changes nodes and leaves their
// attributes stale fails the run at the verification of its end, without VerifyEach.
func TestStaleAttributesFailTheRun(t *testing.T) {
	src := func() *core.Module {
		m := module(rule(0, "a", seq(&core.Repeat{Min: 1, Max: -1, Body: class('a', 'z')}, choice(lit("ab"), lit("cd")))))
		core.Number(m)
		return m
	}

	// A module whose attributes were never computed: guards and scans read them and none of them
	// computes them, so the run fails.
	if _, _, err := Run(src(), RunOptions{}); err == nil || !strings.Contains(err.Error(), "verify core") {
		t.Errorf("Run of a module without its attributes: %v, want a failure of the verification", err)
	}

	// A pass that replaces a node by one without the attributes of the node it replaces, as scans
	// would if it did not copy them.
	dropping := pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "drop-attributes", Test: true, Obligation: "Replaces a repetition without its attributes (test)."},
		In:   IR, Out: IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			s := m.Rules[0].Body.(*core.Seq)
			rep := s.Items[0].(*core.Repeat)
			sc := &core.Scan{Class: rep.Body.(*core.ClassNode).Class, Min: rep.Min, Max: rep.Max}
			sc.Attrs().ID = rep.Attrs().ID
			s.Items[0] = sc
			return m, pass.PreserveNone(), nil
		},
	})
	m := analyzed(t, src())
	_, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Seq(IR, GuardsPass, dropping), m)
	if err == nil || !strings.Contains(err.Error(), "verify core") || !strings.Contains(err.Error(), "rule a: ") {
		t.Errorf("a pass that leaves stale attributes, without VerifyEach: %v, want a failure of the verification that names rule a", err)
	}

	// introduceScans makes the same replacement and keeps the attributes, so the run verifies.
	if _, _, err := Run(analyzed(t, src()), RunOptions{}); err != nil {
		t.Errorf("Run of an analyzed module: %v", err)
	}
}
