package corepass

import (
	"slices"
	"testing"

	"zeroparse.dev/zp/internal/ir/core"
)

// The declarations of Pipeline can be run: names are distinct, every pass states what it preserves,
// the analyses exist, and the order satisfies every After.
func TestPipelineIsValid(t *testing.T) {
	if err := Pipeline.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{PassGuards, PassScans}
	if got := Pipeline.Names(); !slices.Equal(got, want) {
		t.Fatalf("passes %v, want %v", got, want)
	}
	for _, p := range Pipeline.Passes {
		if p.Obligation == "" {
			t.Errorf("pass %s states no obligation", p.Name)
		}
	}
}

// The passes know no mode: a flag that is off removes its pass, and nothing else does.
func TestPipelineAppliesByFlag(t *testing.T) {
	all := []string{PassGuards, PassScans}
	off := func(names ...string) func(string) bool {
		return func(name string) bool { return !slices.Contains(names, name) }
	}
	cases := []struct {
		name string
		env  Env
		want []string
	}{
		{"default", Env{}, all},
		{"without guards", Env{Flag: off(PassGuards)}, []string{PassScans}},
		{"without scans", Env{Flag: off(PassScans)}, []string{PassGuards}},
		{"nothing", Env{Flag: func(string) bool { return false }}, nil},
	}
	for _, c := range cases {
		env := c.env
		m := &core.Module{}
		_, got, err := Run(m, &env, RunOptions{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: ran %v, want %v", c.name, got, c.want)
		}
	}
}

// A run consumes its module, as the run-time backends (closure.Core, vmlower.Compile) hand it over:
// the passes that ran changed it in place, and the run returns it, not a copy. Under VerifyEach each
// pass takes a copy, so the module is returned unchanged in another one.
func TestRunTakesItsModule(t *testing.T) {
	build := func() *core.Module {
		return analyzed(t, module(rule(0, "a", choice(lit("ab"), lit("cd")))))
	}
	m := build()
	out, ran, err := Run(m, &Env{}, RunOptions{})
	if err != nil || len(ran) == 0 {
		t.Fatalf("ran %v, err %v", ran, err)
	}
	if out != m {
		t.Error("the run returned a copy of its module")
	}
	m = build()
	before := core.Print(m, true)
	out, _, err = Run(m, &Env{}, RunOptions{VerifyEach: true})
	if err != nil || out == m || core.Print(m, true) != before {
		t.Errorf("VerifyEach: the module returned %v, the module changed %v, err %v", out == m, core.Print(m, true) != before, err)
	}
}
