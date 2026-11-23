package driver_test

import (
	"context"
	"fmt"
	"slices"
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
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// actionTarget is a target of the comparison of the Action segment with the lowering of each action
// expression as its printers and compilers made it before the segment existed.
type actionTarget struct {
	name   string
	target driver.Target
	// noOpt are the Action passes that the settings turn off.
	noOpt []string
	// reference returns the options of that lowering of the site s of the table t, inPlace for the
	// variant that Go prints in place of a flat rule's action.
	reference func(t *plandata.TableIR, s ir.ActionSite, inPlace bool, flags pass.Settings) actionlower.Options
}

// referenceProj returns the projections of the rule of a site, which the Go and TypeScript printers
// pass for every site of a rule of a Node table and none in the recognizer (common.Generator.Projections).
func referenceProj(t *plandata.TableIR, s ir.ActionSite) map[string]string {
	if !t.Values {
		return nil
	}
	var proj map[string]string
	ir.Walk(t.M.Rules[s.Rule].Body, func(n ir.Node) {
		if c, ok := n.(*ir.Capture); ok && c.Project != "" {
			if proj == nil {
				proj = map[string]string{}
			}
			proj[c.Name] = c.Project
		}
	})
	return proj
}

var actionTargets = func() []actionTarget {
	goGen := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Types: true, Recognize: true}
	goRef := func(t *plandata.TableIR, s ir.ActionSite, inPlace bool, flags pass.Settings) actionlower.Options {
		typed := !t.Opts.Node
		o := actionlower.Options{StructCall: typed, Visits: t.Opts.Logical, Proj: referenceProj(t, s),
			FieldCompare: typed && flags.On(gopass.PassFieldCompare)}
		if inPlace {
			o.Result = gopass.InPlaceResult
		} else {
			o.TextDispatch = s.Kind == ir.SitePred && flags.On(actionpass.PassTextDispatch)
		}
		return o
	}
	tsRef := func(t *plandata.TableIR, s ir.ActionSite, _ bool, flags pass.Settings) actionlower.Options {
		return actionlower.Options{Visits: t.Opts.Logical, Proj: referenceProj(t, s),
			TextDispatch: s.Kind == ir.SitePred && flags.On(actionpass.PassTextDispatch)}
	}
	// The VM and the closures pass the projections of a rule to its action alone.
	vmRef := func(t *plandata.TableIR, s ir.ActionSite, _ bool, _ pass.Settings) actionlower.Options {
		o := actionlower.Options{Visits: true}
		if s.Kind == ir.SiteRule {
			o.Proj = referenceProj(t, s)
		}
		return o
	}
	tsGen := common.GenOptions{Lib: common.LibImport, LibModule: "./lib", TSLib: tslib.FS}
	off := []string{gopass.PassFieldCompare, actionpass.PassTextDispatch}
	return []actionTarget{
		{"go", driver.Target{Backend: result.Go, Gen: goGen}, nil, goRef},
		{"go without Action passes", driver.Target{Backend: result.Go, Gen: goGen}, off, goRef},
		{"lean go", driver.Target{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Types: true, TypedOnly: true, NoLimitControls: true}}, nil, goRef},
		{"ts", driver.Target{Backend: result.TypeScript, Gen: tsGen}, nil, tsRef},
		{"ts without Action passes", driver.Target{Backend: result.TypeScript, Gen: tsGen}, off, tsRef},
		{"vm", driver.Target{Backend: result.VM}, nil, vmRef},
		{"closure", driver.Target{Backend: result.Closure}, nil, vmRef},
	}
}()

// actionCorpus returns the generated and the fixed action corpora, of which the default tier runs the
// representative cases (codegentest.Representative) and the long tier all.
func actionCorpus() []codegentest.ActionCase {
	all := actionCases()
	plain := make([]codegentest.Case, len(all))
	for i, c := range all {
		plain[i] = c.Case
	}
	keep := map[string]bool{}
	for _, c := range codegentest.Representative(plain) {
		keep[c.Name] = true
	}
	var out []codegentest.ActionCase
	for _, c := range all {
		if keep[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

// actionCases returns the generated and the fixed action corpora.
func actionCases() []codegentest.ActionCase {
	var cases []codegentest.ActionCase
	cases = append(cases, codegentest.ActionCases(1, 40, false)...)
	for seed := uint64(1); seed <= 6; seed++ {
		cases = append(cases, codegentest.ActionCasesWide(seed, 20, seed > 4)...)
	}
	for _, f := range []func() []codegentest.ActionCase{codegentest.MemberActionCases, codegentest.ConvertActionCases,
		codegentest.StructActionCases, codegentest.CastActionCases, codegentest.ErrorCastActionCases,
		codegentest.RecoveredActionCases, codegentest.MissingActionCases, codegentest.MissingFieldActionCases,
		codegentest.HullActionCases, codegentest.IterActionCases, codegentest.PushActionCases, codegentest.ScalarActionCases,
		func() []codegentest.ActionCase { return codegentest.RecoveredStructActionCases(false) },
		func() []codegentest.ActionCase { return codegentest.RecoveredStructActionCases(true) }} {
		cases = append(cases, f()...)
	}
	return cases
}

// actionTables returns an observer that records the tables of a compilation with their Action modules:
// those of the input of the planning (plandata.Program.Cores), or, for the closures, which plan
// nothing, the Core module of each table with the functions that action-finish ended.
func actionTables(tables *[]*plandata.TableIR, closures bool) pipe.Observer {
	cores := map[string]*plandata.TableIR{}
	return &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if r.Err != nil || !r.Ran {
			return
		}
		switch u := r.Unit.(type) {
		case *plandata.Program:
			for _, x := range u.Cores.Units {
				if !slices.Contains(*tables, x.IR) {
					*tables = append(*tables, x.IR)
				}
			}
		case *ir.Module:
			if !closures {
				return
			}
			// The closures run the VM's table, a Node table; the last Core step gives its module.
			cores[e.Label.Table] = &plandata.TableIR{M: u, Values: true}
		case code.At[code.Action]:
			tb := cores[e.Label.Table]
			if e.Step.Name != actionpass.PassFinish || tb == nil {
				return
			}
			if tb.Actions.Module() == nil {
				tb.Actions = code.EnterAction(&code.Module{})
				*tables = append(*tables, tb)
			}
			m := tb.Actions.Module()
			m.Funcs = append(m.Funcs, u.Module().Funcs[0])
			for _, o := range u.Module().Ops {
				if m.Op(o.Name) == nil {
					m.AddOp(o)
				}
			}
		}
	}}
}

// The Action segment of every target lowers each action site of each table to the function that the
// target's printer or compiler lowered the site's action expression to before the segment existed:
// the same statements, locals and operations, for the generated and the fixed action corpora.
func TestActionSegmentEqualsTheLowering(t *testing.T) {
	t.Parallel()
	cases := actionCorpus()
	for _, at := range actionTargets {
		t.Run(at.name, func(t *testing.T) {
			t.Parallel()
			flags, err := driver.GenSettings(common.GenOptions{NoOpt: at.noOpt})
			if err != nil {
				t.Fatal(err)
			}
			sites, compiled := 0, 0
			for _, c := range cases {
				g, err := parser.Parse(c.Src)
				if err != nil {
					t.Fatalf("%s: %v", c.Name, err)
				}
				p, err := driver.Check(g, common.CompileOptions{}, "")
				if err != nil {
					continue // a generated grammar that the compiler rejects
				}
				var tables []*plandata.TableIR
				if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: flags, Observers: []pipe.Observer{actionTables(&tables, at.target.Backend == result.Closure)}}, at.target); err != nil {
					t.Fatalf("%s: %v", c.Name, err)
				}
				compiled++
				if len(tables) == 0 {
					t.Fatalf("%s: no table with its Action module", c.Name)
				}
				for _, tb := range tables {
					sites += compareSites(t, c.Name, at, tb, flags)
				}
			}
			if compiled == 0 || sites == 0 {
				t.Fatalf("compared %d sites of %d grammars", sites, compiled)
			}
			t.Logf("compared %d sites of %d of %d grammars", sites, compiled, len(cases))
		})
	}
}

// compareSites compares the function of each site of the table tb with the reference lowering of its
// action expression and returns the number of sites.
func compareSites(t *testing.T, name string, at actionTarget, tb *plandata.TableIR, flags pass.Settings) int {
	t.Helper()
	m := tb.Actions.Module()
	if m == nil {
		t.Fatalf("%s: a table without its Action module", name)
	}
	sites := ir.ActionSitesOf(tb.M).Sites
	for i, s := range sites {
		variants := []bool{false}
		if at.target.Backend == result.Go && s.Kind == ir.SiteRule {
			variants = append(variants, true)
		}
		for _, inPlace := range variants {
			fn := actionlower.SiteFunc(i)
			if inPlace {
				fn = gopass.InPlaceFunc(fn)
			}
			f := m.Func(fn)
			if f == nil {
				t.Errorf("%s: no function %s", name, fn)
				continue
			}
			want, err := actionlower.Lower(s.Action, at.reference(tb, s, inPlace, flags))
			if err != nil {
				t.Fatalf("%s: site %d: %v", name, i, err)
			}
			g := *f
			g.Name = want.Func.Name
			if got, want := code.FuncStringIn(m, &g), code.FuncStringIn(want.Module, want.Func); got != want {
				t.Errorf("%s: %s of the %s differs from the lowering of %s:\n%s", name, fn, tableKind(tb), ir.ActionExprString(s.Action), firstDifference(got, want))
			}
		}
	}
	return len(sites)
}

// firstDifference returns the first line in which got and want differ, with the lines around it.
func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	i := 0
	for i < len(g) && i < len(w) && g[i] == w[i] {
		i++
	}
	window := func(l []string) string {
		return strings.Join(l[max(0, i-3):min(len(l), i+4)], "\n")
	}
	return fmt.Sprintf("line %d; got\n%s\nwant\n%s", i+1, window(g), window(w))
}

// tableKind names the kind of the table t.
func tableKind(t *plandata.TableIR) string {
	switch {
	case !t.Values:
		return "recognizer"
	case !t.Opts.Node:
		return "typed table"
	}
	return "Node table"
}

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
	if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: flags, Debug: debug}, actionTargets[0].target); err != nil {
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
