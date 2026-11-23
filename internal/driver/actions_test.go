package driver_test

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
)

// The debugging settings cover the Action segment: a dump after an Action pass or step shows the
// function of each site that it ran on, under the label of its table and site, in the listing of the
// Action stage.
func TestActionSegmentDumps(t *testing.T) {
	t.Parallel()
	g, err := parser.Parse("type R = { A string }\n@start\nrule main R = x:\"a\" {text($x) == \"a\" || text($x) == \"b\"} $$ => R{A: text($x)}\n")
	if err != nil {
		t.Fatal(err)
	}
	p, err := driver.Check(g, common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	flags, err := driver.GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var dump strings.Builder
	debug := common.Debug{PrintAfter: []string{actionlower.PassLower, actionpass.PassTextDispatch, gopass.PassStructCall, actionpass.PassFinish}, Output: &dump}
	if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: flags, Debug: debug}, driver.Target{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Types: true}}); err != nil {
		t.Fatal(err)
	}
	got := dump.String()
	for _, want := range []string{
		"-- after actionlower [typed] --\nfunc site0(",
		"-- after text-dispatch [typed/site0] --\nfunc site0(",
		"action.textIn(",
		"-- after go-struct-call [typed/site1] --\nfunc site1(",
		"action.new1([R]",
		"-- after action-finish [typed/site1_result] --\nfunc site1_result(",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the dump lacks %q:\n%s", want, got)
		}
	}
	// The dumps list the functions of the sites, not the compositions that they call.
	if strings.Contains(got, "func push(") {
		t.Errorf("a dump lists a composition:\n%s", got)
	}
}
