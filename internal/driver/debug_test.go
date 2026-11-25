package driver

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The test passes of the debugging tests: one that reports a change and changes nothing, one that
// renames the rule, and one that counts.
var (
	debugSame = pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "test-debug-same", Test: true, Obligation: "none: it changes nothing"},
		In:   corepass.IR, Out: corepass.IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			return m, pass.PreserveNone(), nil
		}})
	debugRename = pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "test-debug-rename", Test: true, Obligation: "none: it renames the rules"},
		In:   corepass.IR, Out: corepass.IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			for _, r := range m.Rules {
				r.Name += "x"
			}
			return m, pass.PreserveNone(), nil
		}})
	debugBreak = pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "test-debug-break", Test: true, Obligation: "none: it stands for a pass that miscompiles"},
		In:   corepass.IR, Out: corepass.IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			m.Rules[0].Name = "broken"
			return m, pass.PreserveNone(), nil
		}})
	debugCount = pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "test-debug-count", Test: true, Counters: []string{"rules"}, Obligation: "none: it counts the rules"},
		In:   corepass.IR, Out: corepass.IR,
		Run: func(c *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			c.Count("rules", len(m.Rules))
			return m, pass.PreserveAll(), nil
		}})
)

// debugPipeline is the pipeline of the test passes of the debugging tests, which run in place of the
// Core passes.
func debugPipeline() pipe.Pipeline[*core.Module, *core.Module] {
	return pipe.Seq(corepass.IR, debugSame, debugRename, debugCount, debugRename)
}

// runCore runs, with the observers of d, the Core part of the pipeline of a table, elaborate and then
// passes, on each of the tables (the table rules if none is given), as a compilation runs it, and
// returns the module of the first. The build of each table takes three steps.
func runCore(d *debugger, passes pipe.Pipeline[*core.Module, *core.Module], tables ...string) (*core.Module, error) {
	if len(tables) == 0 {
		tables = []string{"rules"}
	}
	ps := make([]testPart[common.TableSource], len(tables))
	for i, l := range tables {
		ps[i] = testPart[common.TableSource]{l, common.TableSource{Build: func(control *cancel.Control, _ bool) (*core.Module, error) {
			for range 3 {
				control.Poll()
			}
			return oneRule()
		}}}
	}
	p := pipe.Each(testCoreTables, pipe.Then(pipe.Of(elaborating), passes))
	out, err := execute(context.Background(), d, &pipe.Run{Observers: d.observers()}, p, ps)
	if len(out) == 0 {
		return nil, err
	}
	return out[0], err
}

// oneRule builds a Core module of one rule r, which matches "a".
func oneRule() (*core.Module, error) {
	m := &core.Module{Exps: []string{`"a"`}}
	m.Rules = []*core.Rule{{Name: "r", Body: &core.Lit{Text: "a"}, Policy: core.Policy{Seen: -1}}}
	core.CallGraph(m)
	return m, nil
}

// PrintChanged dumps the unit after each transform that changed its text, never after one that only
// reported a change or changed nothing, and before the first such dump the unit as that transform
// received it. A step that PrintAfter selects too is dumped once.
func TestDriverPrintChanged(t *testing.T) {
	var dump strings.Builder
	d := &debugger{PrintChanged: true, Output: &dump}
	if _, err := runCore(d, debugPipeline()); err != nil {
		t.Fatal(err)
	}
	got := dump.String()
	if headers := regexp.MustCompile(`(?m)^-- .* --$`).FindAllString(got, -1); strings.Join(headers, "\n") != strings.Join([]string{
		"-- before test-debug-rename [rules] --",
		"-- after test-debug-rename [rules] --",
		"-- after test-debug-rename#2 [rules] --",
	}, "\n") {
		t.Errorf("headers %q in:\n%s", headers, got)
	}
	for _, name := range []string{"rule 0 r\n", "rule 0 rx\n", "rule 0 rxx\n"} {
		if strings.Count(got, name) != 1 {
			t.Errorf("the dumps show %q %d times, want once:\n%s", name, strings.Count(got, name), got)
		}
	}

	dump.Reset()
	d = &debugger{PrintChanged: true, PrintAfter: []string{"test-debug-rename", "test-debug-same"}, Output: &dump}
	if _, err := runCore(d, debugPipeline()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(dump.String(), "-- after test-debug-rename [rules] --"); n != 1 {
		t.Errorf("a pass that both select is dumped %d times:\n%s", n, dump.String())
	}
	if n := strings.Count(dump.String(), "-- after test-debug-same [rules] --"); n != 1 {
		t.Errorf("PrintAfter of a pass that changed nothing is dumped %d times:\n%s", n, dump.String())
	}

	// Without an Output nothing is collected or written.
	d = &debugger{PrintChanged: true, Stats: true, TimePasses: true}
	if _, err := runCore(d, debugPipeline()); err != nil {
		t.Fatal(err)
	}
	d.report()
	if obs := d.observers(); len(obs) != 0 {
		t.Errorf("a debugger without Output observes with %d observers", len(obs))
	}
}

// PrintBefore dumps the unit that each run of a selected pass receives, of every instance of the pass,
// before the dump after it if PrintAfter selects it too; PrintAfter selects every instance as well. A
// required step is no pass to print before.
func TestDriverPrintBefore(t *testing.T) {
	var dump strings.Builder
	d := &debugger{PrintBefore: []string{"test-debug-rename"}, PrintAfter: []string{"test-debug-rename"}, Output: &dump}
	if _, err := runCore(d, debugPipeline()); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"-- before test-debug-rename [rules] --", "rule 0 r",
		"-- after test-debug-rename [rules] --", "rule 0 rx",
		"-- before test-debug-rename#2 [rules] --", "rule 0 rx",
		"-- after test-debug-rename#2 [rules] --", "rule 0 rxx",
	}, "\n")
	var got []string
	for _, l := range strings.Split(dump.String(), "\n") {
		if strings.HasPrefix(l, "-- ") || strings.HasPrefix(l, "rule 0 ") {
			got = append(got, l)
		}
	}
	if strings.Join(got, "\n") != want {
		t.Errorf("dumps:\n%s\nwant the lines:\n%s", dump.String(), want)
	}

	// With PrintChanged too, the input is written once before the first change.
	dump.Reset()
	d = &debugger{PrintBefore: []string{"all"}, PrintChanged: true, Output: &dump}
	if _, err := runCore(d, debugPipeline()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(dump.String(), "-- before test-debug-rename [rules] --"); n != 1 {
		t.Errorf("the input of the first change is written %d times:\n%s", n, dump.String())
	}
	if strings.Contains(dump.String(), "-- before elaborate") {
		t.Errorf("all selected the required step elaborate:\n%s", dump.String())
	}
	for _, name := range []string{"test-debug-same", "test-debug-count", "test-debug-rename#2"} {
		if !strings.Contains(dump.String(), "-- before "+name+" [rules] --") {
			t.Errorf("all did not select %s:\n%s", name, dump.String())
		}
	}

	if err := CheckPrintBefore([]string{"guards", "all"}); err != nil {
		t.Error(err)
	}
	for _, c := range []struct{ name, want string }{
		{"flowlower", "flowlower is a required step, and -print-before takes passes"},
		{"core", `unknown pass "core" to print before`},
		{"nosuch", `unknown pass "nosuch" to print before`},
	} {
		if err := CheckPrintBefore([]string{c.name}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// The report writes the statistics of every pass instance that ran, in the order it ran, over every
// table of the run, and the times of every step instance with a total.
func TestDriverReport(t *testing.T) {
	var out strings.Builder
	d := &debugger{Stats: true, TimePasses: true, Output: &out}
	if _, err := runCore(d, debugPipeline(), "rules", "typed"); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("the reports were written before the report:\n%s", out.String())
	}
	d.report()
	got := out.String()
	stats, times, ok := strings.Cut(got, "-- time-passes --\n")
	if !ok || !strings.HasPrefix(stats, "-- stats --\n") {
		t.Fatalf("report:\n%s", got)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(stats, "\n"), "\n")[1:] {
		lines = append(lines, strings.Join(strings.Fields(l), " "))
	}
	want := []string{
		"elaborate runs 2 changed 2 steps 6",
		"test-debug-same runs 2 changed 2 steps 0",
		"test-debug-rename runs 2 changed 2 steps 0",
		"test-debug-count runs 2 changed 0 steps 0 rules 2",
		"test-debug-rename#2 runs 2 changed 2 steps 0",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("stats:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	var names []string
	timeLine := regexp.MustCompile(`^  (\S+) +\d+\.\d{3}ms +\d+\.\d%$`)
	for _, l := range strings.Split(strings.TrimSuffix(times, "\n"), "\n") {
		m := timeLine.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("time line %q in:\n%s", l, times)
		}
		names = append(names, m[1])
	}
	if strings.Join(names, " ") != "elaborate test-debug-same test-debug-rename test-debug-count test-debug-rename#2 manager total" {
		t.Errorf("time lines %v", names)
	}
}

// A dump of a step in a branch of a Fork names the backend first, since every target writes to the
// one writer of the debugging settings.
func TestDriverDumpNamesTheBackend(t *testing.T) {
	var dump strings.Builder
	d := &debugger{PrintAfter: []string{"guards"}, Output: &dump}
	o := pipeObserver{d: d}
	e := pipe.Event{Step: pipe.StepInfo{Name: "guards", Instance: "guards", In: "core", Out: "core"}, Label: pipe.Label{Backend: "go", Table: "rules", Func: "main"}}
	o.After(e, &pipe.Result{Ran: true, Print: func(w io.Writer) { io.WriteString(w, "unit\n") }})
	if got := dump.String(); got != "-- after guards [go/rules/main] --\nunit\n" {
		t.Errorf("dump %q", got)
	}
}

// OptBisectLimit runs the optional runs numbered up to the limit, over every table of the run,
// skips the later ones, always runs the required steps and writes each decision. Searching for the
// smallest limit whose output differs from that of limit 0 finds the run of the pass that broke it.
func TestDriverOptBisectLimit(t *testing.T) {
	breaking := pipe.Seq(corepass.IR, debugSame, debugCount, debugBreak, debugRename)
	run := func(limit int) (string, string) {
		t.Helper()
		var out strings.Builder
		d := &debugger{OptBisectLimit: &limit, Output: &out}
		m, err := runCore(d, breaking)
		if err != nil {
			t.Fatal(err)
		}
		return m.Rules[0].Name, out.String()
	}
	name, decisions := run(2)
	if name != "r" {
		t.Errorf("limit 2 left the rule %q, want r", name)
	}
	if want := strings.Join([]string{
		"opt-bisect: 1 run test-debug-same [rules]",
		"opt-bisect: 2 run test-debug-count [rules]",
		"opt-bisect: 3 skip test-debug-break [rules]",
		"opt-bisect: 4 skip test-debug-rename [rules]",
	}, "\n") + "\n"; decisions != want {
		t.Errorf("decisions:\n%s\nwant:\n%s", decisions, want)
	}
	if name, _ := run(4); name != "brokenx" {
		t.Errorf("limit 4 left the rule %q, want brokenx", name)
	}

	// The search: the smallest limit whose output differs from limit 0 names the run that broke it.
	good, _ := run(0)
	first := -1
	for n := 1; n <= 4; n++ {
		if got, _ := run(n); got != good {
			first = n
			break
		}
	}
	if _, decisions := run(first); !strings.Contains(decisions, fmt.Sprintf("opt-bisect: %d run test-debug-break [rules]\n", first)) {
		t.Errorf("the first limit that breaks the output is %d, whose run is not test-debug-break:\n%s", first, decisions)
	}

	// The numbers go on over the tables of one run.
	limit := 5
	var out strings.Builder
	d := &debugger{OptBisectLimit: &limit, Output: &out}
	if _, err := runCore(d, breaking, "rules", "typed"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "opt-bisect: 5 run test-debug-same [typed]\nopt-bisect: 6 skip test-debug-count [typed]\n") {
		t.Errorf("the second table is not numbered after the first:\n%s", out.String())
	}
}
