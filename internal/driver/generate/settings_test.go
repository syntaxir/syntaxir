package generate_test

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/ir/core"
)

// Settings applies the profile, then Opt, then NoOpt, so an optimization that is both given a
// value and turned off is off, and a value outside what the optimization takes is an error.
func TestSettingsOrder(t *testing.T) {
	t.Parallel()
	s, err := generate.Settings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.On("guards") || s.Value("class-tables") != 0 || s.On("ts-literal-sharing") {
		t.Errorf("the default profile: guards %v, class-tables %d, ts-literal-sharing %v", s.On("guards"), s.Value("class-tables"), s.On("ts-literal-sharing"))
	}

	s, err = generate.Settings(common.GenOptions{
		Opt:   map[string]int{"class-tables": 3, "ts-literal-sharing": 1, " scans ": 0, "inline-literals": 2},
		NoOpt: []string{"inline-literals", " guards"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Value("class-tables") != 3 || !s.On("ts-literal-sharing") {
		t.Errorf("Opt did not give its values: class-tables %d, ts-literal-sharing %v", s.Value("class-tables"), s.On("ts-literal-sharing"))
	}
	if s.On("scans") {
		t.Errorf("Opt scans=0 left scans on")
	}
	if s.On("inline-literals") || s.On("guards") {
		t.Errorf("NoOpt did not win: inline-literals %d, guards %v", s.Value("inline-literals"), s.On("guards"))
	}

	for _, c := range []struct {
		opts common.GenOptions
		want string
	}{
		{common.GenOptions{Opt: map[string]int{"nosuch": 1}}, `unknown optimization "nosuch"`},
		{common.GenOptions{Opt: map[string]int{"guards": 2}}, "does not take the value 2"},
		{common.GenOptions{Opt: map[string]int{"class-tables": -1}}, "does not take the value -1"},
		{common.GenOptions{Opt: map[string]int{"guards": 1, " guards ": 0}}, "optimization guards is given twice"},
		{common.GenOptions{NoOpt: []string{"nosuch"}}, `unknown optimization "nosuch"`},
		{common.GenOptions{Optimize: "nosuch"}, "unknown optimization profile"},
	} {
		if _, err := generate.Settings(c.opts); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want an error with %q", c.opts, err, c.want)
		}
	}
}

// Passes gives every runner the settings, the debugging settings of Debug and the hooks, writes the dumps of each runner to the writer that it is made with only, and
// reports the error of the settings.
func TestPassesRunners(t *testing.T) {
	t.Parallel()
	var dumps bytes.Buffer
	hooks := generate.Hooks{CoreHook: func(string, *core.Module) {}}
	p := generate.Passes(common.GenOptions{NoOpt: []string{"guards"}, Debug: common.Debug{PrintAfter: []string{"guards"}, PrintChanged: true, VerifyEach: true, Stats: true, TimePasses: true, Output: &dumps}}, hooks)
	if err := p.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var runners []*driver.Driver
	for _, w := range []io.Writer{nil, &dumps} {
		d, ok := p.Runner(w).(*driver.Driver)
		if !ok {
			t.Fatalf("the runner is %T, not a *driver.Driver", p.Runner(w))
		}
		if d.Settings().On("guards") || !slices.Equal(d.PrintAfter, []string{"guards"}) || !d.PrintChanged || !d.VerifyEach || !d.Stats || !d.TimePasses || d.CoreHook == nil || d.Output != w {
			t.Errorf("runner of %v: guards %v, PrintAfter %q, PrintChanged %v, VerifyEach %v, Stats %v, TimePasses %v, CoreHook set %v, Output %v", w, d.Opt.On("guards"), d.PrintAfter, d.PrintChanged, d.VerifyEach, d.Stats, d.TimePasses, d.CoreHook != nil, d.Output)
		}
		runners = append(runners, d)
	}
	if runners[0] == runners[1] {
		t.Error("two runners share a driver")
	}
	for _, d := range []common.Debug{{PrintAfter: []string{"guards"}}, {PrintChanged: true}, {Stats: true}, {TimePasses: true}} {
		if err := generate.Passes(common.GenOptions{Debug: d}, generate.Hooks{}).Validate(); err == nil || !strings.Contains(err.Error(), "need an Output writer") {
			t.Errorf("%+v without Output: %v", d, err)
		}
	}
	if err := generate.Passes(common.GenOptions{Debug: common.Debug{VerifyEach: true}}, generate.Hooks{}).Validate(); err != nil {
		t.Errorf("VerifyEach writes nothing and needs no Output: %v", err)
	}
	if err := generate.Passes(common.GenOptions{Debug: common.Debug{PrintAfter: []string{"core"}, Output: &dumps}}, generate.Hooks{}).Validate(); err == nil || !strings.Contains(err.Error(), "the step that makes its IR is elaborate") {
		t.Errorf("a stage name: %v", err)
	}
}
