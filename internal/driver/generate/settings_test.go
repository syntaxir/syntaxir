package generate_test

import (
	"bytes"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/generate"
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
		{common.GenOptions{NoOpt: []string{" flowlower "}}, "flowlower is a required step and always runs"},
		{common.GenOptions{Opt: map[string]int{"execlower": 1}}, "execlower is a required step and always runs"},
	} {
		if _, err := generate.Settings(c.opts); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want an error with %q", c.opts, err, c.want)
		}
	}
}

// Settings validates the debugging settings of Debug with the optimizations: the settings of a valid
// Debug are the optimizations, a Debug that writes anything needs an Output, and PrintAfter names a step
// or a pass, not a stage.
func TestSettingsDebug(t *testing.T) {
	t.Parallel()
	var dumps bytes.Buffer
	s, err := generate.Settings(common.GenOptions{NoOpt: []string{"guards"}, Debug: common.Debug{PrintAfter: []string{"guards"}, PrintChanged: true, VerifyEach: true, Stats: true, TimePasses: true, Output: &dumps}})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if s.On("guards") {
		t.Error("guards is on although NoOpt turns it off")
	}
	for _, d := range []common.Debug{{PrintAfter: []string{"guards"}}, {PrintChanged: true}, {Stats: true}, {TimePasses: true}} {
		if _, err := generate.Settings(common.GenOptions{Debug: d}); err == nil || !strings.Contains(err.Error(), "need an Output writer") {
			t.Errorf("%+v without Output: %v", d, err)
		}
	}
	if _, err := generate.Settings(common.GenOptions{Debug: common.Debug{VerifyEach: true}}); err != nil {
		t.Errorf("VerifyEach writes nothing and needs no Output: %v", err)
	}
	if _, err := generate.Settings(common.GenOptions{Debug: common.Debug{PrintAfter: []string{"core"}, Output: &dumps}}); err == nil || !strings.Contains(err.Error(), "the step that makes its IR is elaborate") {
		t.Errorf("a stage name: %v", err)
	}
}
