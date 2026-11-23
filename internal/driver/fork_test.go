package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// forkGrammars are a grammar that every target plans once, and one whose TypeScript parser falls
// back to the reach of its printed code (TestCompileReplansTheTypeScriptTarget).
var forkGrammars = []string{
	"@start\nrule A = \"a\" B / C\nrule B = \"b\"*\nrule C = [c-e]+ B\n",
	"@start\nrule main = !\"\"* B / \"x\"\nrule B = \"b\"\n",
}

func goOut(gen common.GenOptions) Target {
	gen.Module, gen.Lib, gen.LibGo = "example.com/p", common.LibImport, libsrc.FS
	return Target{Backend: result.Go, Gen: gen}
}

func tsOut(gen common.GenOptions) Target {
	gen.Lib, gen.LibModule, gen.TSLib = common.LibImport, "./lib", tslib.FS
	return Target{Backend: result.TypeScript, Gen: gen}
}

// filesText returns the files of an output as one text, path by path.
func filesText(t *testing.T, o Output) string {
	t.Helper()
	f, ok := o.(*result.Files)
	if !ok {
		t.Fatalf("output %T, want files", o)
	}
	var b strings.Builder
	for _, x := range f.Files {
		fmt.Fprintf(&b, "== %s\n%s", x.Path, x.Content)
	}
	return b.String()
}

// Several targets compile in one call as the branches of a Fork, and each gives the files it gives
// when it is compiled alone, also with the sharing of results off and when a TypeScript target falls
// back to the reach of its printed code.
func TestCompileForkGivesTheOutputsOfEachTargetAlone(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sets := map[string][]Target{
		"go,ts":  {goOut(common.GenOptions{}), tsOut(common.GenOptions{})},
		"ts,go":  {tsOut(common.GenOptions{Types: true}), goOut(common.GenOptions{Types: true, Recognize: true})},
		"go,go":  {goOut(common.GenOptions{}), goOut(common.GenOptions{Types: true, Observability: true})},
		"ts,ts":  {tsOut(common.GenOptions{NoLimitControls: true}), tsOut(common.GenOptions{})},
		"3 ways": {goOut(common.GenOptions{Document: true}), tsOut(common.GenOptions{}), goOut(common.GenOptions{})},
	}
	for _, src := range forkGrammars {
		p, err := checkSource(t, src, common.CompileOptions{}, "")
		if err != nil {
			t.Fatal(err)
		}
		for name, targets := range sets {
			alone := make([]string, len(targets))
			for i, x := range targets {
				out, err := Compile(context.Background(), p, Options{Settings: speed}, x)
				if err != nil {
					t.Fatalf("%s %d alone: %v", name, i, err)
				}
				alone[i] = filesText(t, out[0])
			}
			for _, noShare := range []bool{false, true} {
				o := Options{Settings: speed}
				if noShare {
					o.Observers = []pipe.Observer{&pipetest.Watch{Units: true}} // units turn sharing off
				}
				outs, err := Compile(context.Background(), p, o, targets...)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if len(outs) != len(targets) {
					t.Fatalf("%s: %d outputs for %d targets", name, len(outs), len(targets))
				}
				for i, out := range outs {
					if out.Backend() != targets[i].Backend || filesText(t, out) != alone[i] {
						t.Errorf("%q %s (no sharing %v): target %d differs from the target compiled alone", src, name, noShare, i)
					}
				}
			}
		}
	}
}

// forkEvents records the steps that ran, with their labels and whether they were replayed.
type forkEvents struct{ seen *[]string }

func (forkEvents) Needs() pipe.Needs                       { return pipe.Needs{} }
func (forkEvents) Before(pipe.Event, func(io.Writer)) bool { return true }
func (f forkEvents) After(e pipe.Event, r *pipe.Result) {
	if r.Ran {
		s := e.Label.Backend + "/" + e.Label.Table + " " + e.Step.Instance
		if e.Replayed {
			s += " replayed"
		}
		*f.seen = append(*f.seen, s)
	}
}

// The branches of a Fork are labeled with their backends, a second target of one backend with its
// number, and the tables of the targets that build the same Core IR share it: the later branch
// replays the elaboration and the Core passes of the earlier one.
func TestCompileForkSharesTheCoreIR(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := checkSource(t, forkGrammars[0], common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	o := Options{Settings: speed, Observers: []pipe.Observer{forkEvents{&seen}}}
	if _, err := Compile(context.Background(), p, o, goOut(common.GenOptions{}), tsOut(common.GenOptions{}), goOut(common.GenOptions{Recognize: true})); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(seen, "\n")
	for _, want := range []string{
		"go/rules elaborate\n", "go/rules guards\n", "go/ print-go\n",
		"ts/rules elaborate replayed\n", "ts/rules guards replayed\n", "ts/ outline\n", "ts/ print-ts\n",
		"go#2/rules elaborate replayed\n", "go#2/recRules elaborate\n", "go#2/ print-go\n",
	} {
		if !strings.Contains(text+"\n", want) {
			t.Errorf("the events have no %q:\n%s", want, text)
		}
	}
}

// When the TypeScript target of a Fork falls back to the reach of its printed code, the Fork runs
// again with that reach in the branch of that target only: the Go target plans as it did, and the
// dumps of the debugging settings are those of the second run, grouped by target and table.
func TestCompileForkReplansOneBranch(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := checkSource(t, forkGrammars[1], common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	var dump bytes.Buffer
	o := Options{Settings: speed, Debug: common.Debug{PrintAfter: []string{"plan"}, Output: &dump}, Observers: []pipe.Observer{forkEvents{&seen}}}
	outs, err := Compile(context.Background(), p, o, goOut(common.GenOptions{}), tsOut(common.GenOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	prints := map[string]int{}
	for _, s := range seen {
		if b, ok := strings.CutSuffix(s, " print-go"); ok {
			prints[b]++
		}
		if b, ok := strings.CutSuffix(s, " print-ts"); ok {
			prints[b]++
		}
	}
	if prints["go/"] != 2 || prints["ts/"] != 2 {
		t.Errorf("printers ran %v, want each twice: the Fork runs again", prints)
	}
	got := dump.String()
	if strings.Count(got, "-- after plan [go/program] --") != 1 || strings.Count(got, "-- after plan [ts/program] --") != 1 {
		t.Errorf("the dumps are not those of one run:\n%s", got)
	}
	goPlan, tsPlan, _ := strings.Cut(got, "-- after plan [ts/program] --")
	if !strings.Contains(goPlan, "rule 1 B call=wrapper reach=wrapper") || !strings.Contains(tsPlan, "rule 1 B call=wrapper reach=none") {
		t.Errorf("the Go plan reaches B and the TypeScript plan of the second run does not:\n%s", got)
	}
	if strings.Contains(filesText(t, outs[1]), `"B", `) {
		t.Errorf("the TypeScript parser keeps the rule B")
	}
}

// A failure of one target of a Fork reports that target, and the failures of every target are
// joined in target order; the targets that do not fail still run.
func TestCompileForkJoinsTheErrorsOfItsTargets(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := checkSource(t, forkGrammars[0], common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	noTSLib, noLibGo := tsOut(common.GenOptions{}), goOut(common.GenOptions{})
	noTSLib.Gen.TSLib, noLibGo.Gen.LibGo = nil, nil
	var seen []string
	o := Options{Settings: speed, Observers: []pipe.Observer{forkEvents{&seen}}}
	_, err = Compile(context.Background(), p, o, goOut(common.GenOptions{}), noTSLib, noLibGo)
	if err == nil {
		t.Fatal("no error")
	}
	var j interface{ Unwrap() []error }
	if !errors.As(err, &j) || len(j.Unwrap()) != 2 {
		t.Fatalf("err = %v, want two joined errors", err)
	}
	es := j.Unwrap()
	if !strings.HasPrefix(es[0].Error(), "target ts: ") || !strings.Contains(es[0].Error(), "set GenOptions.TSLib") ||
		!strings.HasPrefix(es[1].Error(), "target go#2: ") || !strings.Contains(es[1].Error(), "set GenOptions.LibGo") {
		t.Errorf("err = %v, want the failures of ts and go#2 in that order", err)
	}
	if !slices.Contains(seen, "go/ print-go") {
		t.Errorf("the first Go target did not print:\n%s", strings.Join(seen, "\n"))
	}
}

// dumpSections splits a dump into its sections, each from its "-- " header line, and groups them by
// the backend of backends that prefixes the label of the header ("" for none), which it removes; the
// lines of the stats block go to the backends that prefix them, each as a stats block of its own
// with the spaces of its columns collapsed.
func dumpSections(dump string, backends ...string) map[string][]string {
	by := map[string][]string{}
	stats := map[string]string{}
	var backend string
	var cur strings.Builder
	inStats := false
	end := func() {
		if cur.Len() > 0 {
			by[backend] = append(by[backend], cur.String())
			cur.Reset()
		}
	}
	strip := func(s string) (string, string) {
		for _, b := range backends {
			if rest, ok := strings.CutPrefix(s, b+"/"); ok {
				return b, rest
			}
		}
		return "", s
	}
	for _, line := range strings.SplitAfter(dump, "\n") {
		if strings.HasPrefix(line, "-- ") {
			end()
			backend, inStats = "", line == "-- stats --\n"
			if open := strings.Index(line, " ["); open >= 0 {
				var rest string
				backend, rest = strip(line[open+2:])
				line = line[:open+2] + rest
			}
			if inStats {
				continue
			}
		}
		if inStats {
			b, rest := strip(strings.TrimSpace(line))
			if rest != "" {
				stats[b] += strings.Join(strings.Fields(rest), " ") + "\n"
			}
			continue
		}
		cur.WriteString(line)
	}
	end()
	for b, s := range stats {
		by[b] = append(by[b], "-- stats --\n"+s)
	}
	return by
}

// The dumps of the branches of a Fork are those of their targets compiled alone, with the backend of
// the branch in the label of each: the programs first, then the tables, each group in target order.
func TestCompileForkDumpsEachTargetAsAlone(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := checkSource(t, forkGrammars[0], common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	debug := func(w io.Writer) common.Debug {
		return common.Debug{PrintAfter: []string{"elaborate", "plan", "outline", "execlower", "ts-merged-records"}, Output: w, Stats: true}
	}
	targets := []Target{goOut(common.GenOptions{}), tsOut(common.GenOptions{}), goOut(common.GenOptions{Recognize: true})}
	var fork bytes.Buffer
	if _, err := Compile(context.Background(), p, Options{Settings: speed, Debug: debug(&fork)}, targets...); err != nil {
		t.Fatal(err)
	}
	got := dumpSections(fork.String(), "go", "ts", "go#2")
	var headers []string
	for _, line := range strings.Split(fork.String(), "\n") {
		if strings.HasPrefix(line, "-- ") {
			headers = append(headers, line)
		}
	}
	programs := []string{"-- after plan [go/program] --", "-- after outline [ts/program] --", "-- after plan [ts/program] --", "-- after plan [go#2/program] --"}
	if len(headers) < 4 || !slices.Equal(headers[:4], programs) {
		t.Errorf("the dump does not start with the programs of the targets in order: %q", headers)
	}
	for i, b := range []string{"go", "ts", "go#2"} {
		var alone bytes.Buffer
		if _, err := Compile(context.Background(), p, Options{Settings: speed, Debug: debug(&alone)}, targets[i]); err != nil {
			t.Fatal(err)
		}
		want := dumpSections(alone.String())[""]
		if len(want) < 4 {
			t.Fatalf("the target %s alone dumps %d sections", b, len(want))
		}
		if strings.Join(got[b], "") != strings.Join(want, "") {
			t.Errorf("the dumps of %s differ from those of the target alone:\n%s\nwant:\n%s", b, strings.Join(got[b], ""), strings.Join(want, ""))
		}
	}
	if len(got[""]) != 0 {
		t.Errorf("sections without a backend: %q", got[""])
	}
}

// A compilation decides once whether its Core modules record the origin map, which only the printers
// of the generating backends read: a run-time compilation builds them without it, and one with a
// generating target builds the module that its run-time targets share with it.
func TestCompileDecidesTheOriginMapOnce(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := checkSource(t, forkGrammars[0], common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	elaborated := func(targets ...Target) map[string]*core.Module {
		t.Helper()
		mods := map[string]*core.Module{}
		watch := &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
			if m, ok := r.Unit.(*core.Module); ok && r.Ran && e.Step.Instance == Elaborate && e.Label.Table == vmTable {
				mods[e.Label.Backend] = m
			}
		}}
		if _, err := Compile(context.Background(), p, Options{Settings: speed, Observers: []pipe.Observer{watch}}, targets...); err != nil {
			t.Fatal(err)
		}
		return mods
	}
	for b, m := range elaborated(Target{Backend: result.VM}, Target{Backend: result.Closure}) {
		if len(m.Origin.Exprs) != 0 || len(m.Origin.ActionExprs) != 0 {
			t.Errorf("the run-time compilation built the module of %s with an origin map", b)
		}
	}
	mods := elaborated(Target{Backend: result.VM}, goOut(common.GenOptions{}))
	if len(mods) != 2 {
		t.Fatalf("modules of %d targets, want 2", len(mods))
	}
	for b, m := range mods {
		if len(m.Origin.Exprs) == 0 {
			t.Errorf("the compilation with a Go target built the module of %s without the origin map", b)
		}
	}
	var seen []string
	if _, err := Compile(context.Background(), p, Options{Settings: speed, Observers: []pipe.Observer{forkEvents{&seen}}}, Target{Backend: result.VM}, goOut(common.GenOptions{})); err != nil {
		t.Fatal(err)
	}
	if text := strings.Join(seen, "\n") + "\n"; !strings.Contains(text, "vm/rules elaborate\n") || !strings.Contains(text, "go/rules elaborate replayed\n") {
		t.Errorf("the VM and the Go target do not share one build:\n%s", text)
	}
}
