package driver_test

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/pipetest"
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

// Each action site is lowered once per compilation mode: in one compilation for every backend,
// actionlower runs once on each table of each target, and the Action module it gives has one function
// for each action site of the table, as many as it counts.
func TestActionSitesLoweredOncePerMode(t *testing.T) {
	t.Parallel()
	// The sites: the binding and the predicate of main, the action of main and that of item.
	const sites = 4
	g, err := parser.Parse("type R = { A string }\nvar k int\n@start\nrule main R = {k := k + 1} x:item {text($x) == \"a\" || k > 1} $$ => R{A: text($x)}\n" +
		"rule item string = x:\"a\" => text($x)\n")
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
	runs := map[string]int{}
	w := &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if e.Step.Name != actionlower.PassLower {
			return
		}
		runs[e.Label.String()]++
		funcs, _ := action.SiteFuncs(r.Unit.(code.At[code.Action]).Module())
		if n := r.Counters[actionlower.CounterSites]; n != sites || len(funcs) != sites {
			t.Errorf("%s: actionlower counts %d sites and gives %d site functions, want %d", e.Label, n, len(funcs), sites)
		}
	}}
	gen := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibModule: "./lib", LibGo: libsrc.FS, TSLib: tslib.FS, Types: true}
	targets := []driver.Target{{Backend: result.Go, Gen: gen}, {Backend: result.TypeScript, Gen: gen}, {Backend: result.VM}, {Backend: result.Closure}}
	if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: flags, Observers: []pipe.Observer{w}}, targets...); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"go/rules": 1, "go/typed": 1, "ts/rules": 1, "vm/rules": 1, "closure/rules": 1}
	if !maps.Equal(runs, want) {
		t.Errorf("actionlower ran %v times per table, want %v", runs, want)
	}
}

// testActionUndeclaredCall is a broken Action pass of the tests: it makes the function of each site
// call an operation that its module does not declare, which the verifier of the Action stage rejects.
var testActionUndeclaredCall = pipe.New(pipe.Def[actionpass.Unit, actionpass.Unit]{
	Decl: pipe.Decl{Name: "test-action-undeclared-call", Test: true, Obligation: "none: it breaks the function"},
	In:   code.ActionStage, Out: code.ActionStage,
	Run: func(_ *pipe.Ctx[actionpass.Unit], u actionpass.Unit) (actionpass.Unit, pass.PreservedSet, error) {
		f := u.Module().Funcs[0]
		f.Body.Stmts = append([]code.Stmt{&code.Do{Call: &code.Call{Callee: "test.undeclared"}}}, f.Body.Stmts...)
		return u, pass.PreserveNone(), nil
	},
})

// The verification and the bisection limit cover the Action segment: under VerifyEach a broken Action
// pass is reported by name, and the bisection limit numbers the runs of the Action passes of each
// site with the others, runs those up to the limit and skips the later ones.
func TestActionSegmentVerifiedAndBisected(t *testing.T) {
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
	target := driver.Target{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Types: true}}

	broken := pipe.Fill(driver.ActionSlot, "", pipe.Seq(code.ActionStage, testActionUndeclaredCall))
	_, err = driver.Compile(context.Background(), p, driver.Options{Settings: flags, Debug: common.Debug{VerifyEach: true}, Edits: []pipe.Edit{broken}}, target)
	if err == nil || !strings.Contains(err.Error(), "test-action-undeclared-call") {
		t.Errorf("VerifyEach: the broken Action pass gives %v, want an error that names it", err)
	}

	var all strings.Builder
	limit := 1 << 20
	if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: flags, Debug: common.Debug{OptBisectLimit: &limit, Output: &all}}, target); err != nil {
		t.Fatal(err)
	}
	var first int
	for _, line := range strings.Split(all.String(), "\n") {
		if _, err := fmt.Sscanf(line, "opt-bisect: %d run "+actionpass.PassTextDispatch+" [typed/site0]", &first); err == nil {
			break
		}
	}
	if first == 0 {
		t.Fatalf("the bisection numbers no run of %s on a site:\n%s", actionpass.PassTextDispatch, all.String())
	}
	var cut strings.Builder
	limit = first - 1
	if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: flags, Debug: common.Debug{OptBisectLimit: &limit, Output: &cut}}, target); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("opt-bisect: %d skip %s [typed/site0]\n", first, actionpass.PassTextDispatch); !strings.Contains(cut.String(), want) {
		t.Errorf("limit %d does not skip the run %d of %s:\n%s", limit, first, actionpass.PassTextDispatch, cut.String())
	}
}
