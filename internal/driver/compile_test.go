package driver

import (
	"bytes"
	"context"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass"
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

// Compile fails before any step for a target whose options disagree with the program or the call,
// naming the target among several, and takes at least one target.
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
		{"no target", nil, "at least one target"},
		{"two targets", []Target{{Backend: result.TypeScript}, {Backend: result.Go}}, "target go: module path is required"},
		{"second of a backend", []Target{{Backend: result.TypeScript}, {Backend: result.TypeScript, Gen: common.GenOptions{TypedOnly: true}}}, "target ts#2: typed-only output needs typed output (-types)"},
		{"start", []Target{{Backend: result.Go, Gen: common.GenOptions{Start: "B"}}}, "target go: Start"},
		{"source info", []Target{{Backend: result.Go, Gen: common.GenOptions{SourceInfo: true}}}, "target go: SourceInfo"},
		{"verify", []Target{{Backend: result.Go, Gen: common.GenOptions{Debug: common.Debug{VerifyEach: true}}}}, "target go: Debug"},
		{"settings", []Target{{Backend: result.Go, Gen: common.GenOptions{Optimize: "size"}}}, "target go: Optimize"},
		{"unknown profile", []Target{{Backend: result.Go, Gen: common.GenOptions{Optimize: "nope"}}}, "nope"},
		{"no pipeline", []Target{{Backend: result.Backend(9)}}, "no pipeline is assembled for the backend Backend(9)"},
		{"generation options of the closures", []Target{{Backend: result.Closure, Gen: common.GenOptions{Package: "p"}}}, "target closure: Gen sets an option of a generating backend"},
		{"generation options of the VM", []Target{{Backend: result.VM, Gen: common.GenOptions{Package: "p"}}}, "target vm: Gen sets an option of a generating backend"},
		{"hooks of Go", []Target{{Backend: result.TypeScript, GoHooks: &goprint.Hooks{}}}, "GoHooks are set for a target of another backend"},
		{"hooks of TypeScript", []Target{{Backend: result.Go, TSHooks: &tsprint.Hooks{}}}, "target go: TSHooks are set for a target of another backend"},
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
	if outs, err := Compile(context.Background(), p, o, equal); err != nil || outs[0].Backend() != result.VM {
		t.Errorf("equal fields: err = %v", err)
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
		d := newDebugger(Options{Debug: common.Debug{PrintAfter: names, Output: &w}})
		obs := d.observers()
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

// A Go target compiles to the files of its parser through one run: every table of the parser is
// lowered to the Exec stage before print-go, which an observer sees in the order of the split.
func TestCompileGo(t *testing.T) {
	t.Parallel()
	p, err := checkSource(t, "@start\nrule A = \"a\" B\nrule B = \"b\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	watch := &stepNames{seen: &steps}
	gen := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Recognize: true, Observability: true}
	out, err := Compile(context.Background(), p, Options{Settings: speed, Observers: []pipe.Observer{watch}}, Target{Backend: result.Go, Gen: gen})
	if err != nil {
		t.Fatal(err)
	}
	files := out[0].(*result.Files)
	if files.Backend() != result.Go || len(files.Files) == 0 || files.Files[0].Path != "parser.go" {
		t.Fatalf("output = %+v", files)
	}
	want := []string{"rules", "recRules", "rulesTraced", "recRulesTraced"}
	var lowered []string
	for _, s := range steps {
		if table, ok := strings.CutPrefix(s, "execlower "); ok {
			lowered = append(lowered, table)
		}
	}
	if !slices.Equal(lowered, want) || steps[len(steps)-1] != "print-go " {
		t.Errorf("steps = %q, want the tables %q lowered before print-go", steps, want)
	}
}

// TestCompileReplansTheTypeScriptTarget checks the fallback of planning on a TypeScript target: the
// printed code of main leaves out the call of B after a lookahead that always fails, which the Core IR
// keeps, so print-ts returns the reach of the printed code and Compile runs the pipeline again with it.
// The parser of the second run leaves B out, the observers of the call see both runs, and the dumps
// of the debugging settings are those of the second run only. A lookahead that the printed code keeps
// the call after gives one run.
func TestCompileReplansTheTypeScriptTarget(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		src   string
		runs  int
		keepB bool
	}{
		{"@start\nrule main = !\"\"* B / \"x\"\nrule B = \"b\"\n", 2, false},
		{"@start\nrule main = !\"\" B / \"x\"\nrule B = \"b\"\n", 1, true},
	} {
		p, err := checkSource(t, c.src, common.CompileOptions{}, "")
		if err != nil {
			t.Fatal(err)
		}
		var steps []string
		var dump bytes.Buffer
		gen := common.GenOptions{Lib: common.LibImport, LibModule: "./lib", TSLib: tslib.FS}
		o := Options{Settings: speed, Debug: common.Debug{PrintAfter: []string{"execlower"}, Output: &dump}, Observers: []pipe.Observer{&stepNames{seen: &steps}}}
		out, err := Compile(context.Background(), p, o, Target{Backend: result.TypeScript, Gen: gen})
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		runs, last := 0, 0
		for i, s := range steps {
			if strings.HasPrefix(s, "print-ts ") {
				runs++
				if runs < c.runs {
					last = i + 1
				}
			}
		}
		if runs != c.runs {
			t.Errorf("%q: print-ts ran %d times, want %d: %q", c.src, runs, c.runs, steps)
		}
		lowered := 0
		for _, s := range steps[last:] {
			if strings.HasPrefix(s, "execlower ") {
				lowered++
			}
		}
		if got := strings.Count(dump.String(), "-- after execlower ["); got != lowered || lowered == 0 {
			t.Errorf("%q: the dumps hold %d tables after execlower, want the %d of the last run", c.src, got, lowered)
		}
		files := out[0].(*result.Files)
		if got := strings.Contains(string(files.Files[0].Content), `"B", `); got != c.keepB {
			t.Errorf("%q: the parser has the rule B %v, want %v", c.src, got, c.keepB)
		}
	}
}

// TestCompileNumbersTheUnreachableRules checks that the Go pipeline lowers the rules that the plan
// leaves out of the Node and recognizer tables once, in parts of their own (rulesUnreachable,
// recRulesUnreachable), and numbers the expectation table from every rule: the parser prints the
// table of the parser that keeps the unreachable rules, with the expectations that only the
// unreachable rule records.
func TestCompileNumbersTheUnreachableRules(t *testing.T) {
	t.Parallel()
	p, err := checkSource(t, "@start\nrule A = \"a\" B\nrule B = \"b\"\nrule C = \"c\" / [x-z]\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(keep bool) (descs string, funcs map[string]map[string]bool) {
		t.Helper()
		seen := funcsSeen{}
		gen := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Recognize: true, Expectations: true, KeepUnreachable: keep}
		out, err := Compile(context.Background(), p, Options{Settings: speed, Observers: []pipe.Observer{seen}}, Target{Backend: result.Go, Gen: gen})
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.Lines(string(out[0].(*result.Files).Files[0].Content)) {
			if strings.Contains(line, "sxirGrammar.Descs = ") {
				return line, seen
			}
		}
		t.Fatal("the parser has no expectation table")
		return "", nil
	}
	pruned, prunedFuncs := compile(false)
	kept, keptFuncs := compile(true)
	if pruned != kept {
		t.Errorf("the expectation table is\n%s\nwith the unreachable rules left out, and\n%s\nwith them kept", pruned, kept)
	}
	if !strings.Contains(pruned, `"\"c\""`) {
		t.Errorf("the expectation table %s lacks the expectation of the unreachable rule", pruned)
	}
	for _, table := range []string{"rules", "recRules"} {
		part := prunedFuncs[table+flowplan.UnreachableSuffix]
		if !part["C"] {
			t.Errorf("the part %s%s lowers %v, not the unreachable rule C", table, flowplan.UnreachableSuffix, slices.Sorted(maps.Keys(part)))
		}
		for f := range part {
			if prunedFuncs[table][f] {
				t.Errorf("the function %s is lowered in both %s and its unreachable part", f, table)
			}
		}
		if keptFuncs[table+flowplan.UnreachableSuffix] != nil {
			t.Errorf("the table %s has an unreachable part although the parser keeps every rule", table)
		}
	}
}

// TestCompileNumbersAtABisectionLimit checks the expectation table of a parser under a bisection
// limit, which keeps the rules that the plan leaves out (goprint.Validate) where a parser without a
// limit numbers them from the unreachable parts of its tables. With every run of the optional passes
// let through, the table is that of the parser without a limit; with every run vetoed, that of the
// parser with every optional pass off: the parts number what the full table numbers, whatever the
// passes did to it. At a limit in the middle of the runs, the table is the same on every compilation.
// The passes can move an expectation (a guard records the expectation of the alternative it
// inlines), so the table at that limit may lie between the two.
func TestCompileNumbersAtABisectionLimit(t *testing.T) {
	t.Parallel()
	p, err := checkSource(t, "@start\nrule A = \"a\" B / C\nrule U = \"u\" (\"x\" / \"y\")*\nrule B = \"b\" [0-9]+\nrule C = \"c\" (\"p\" \"q\")*\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range pass.Flags() {
		names = append(names, f.Name)
	}
	off, err := speed.Without(names...)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(s pass.Settings, limit *int) (descs string, runs int) {
		t.Helper()
		var w bytes.Buffer
		gen := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Recognize: true, Expectations: true}
		o := Options{Settings: s, Debug: common.Debug{OptBisectLimit: limit, Output: &w}}
		out, err := Compile(context.Background(), p, o, Target{Backend: result.Go, Gen: gen})
		if err != nil {
			t.Fatal(err)
		}
		runs = strings.Count(w.String(), "opt-bisect: ")
		for line := range strings.Lines(string(out[0].(*result.Files).Files[0].Content)) {
			if strings.Contains(line, "sxirGrammar.Descs = ") {
				return line, runs
			}
		}
		t.Fatal("the parser has no expectation table")
		return "", 0
	}
	all, _ := compile(speed, nil)
	none, _ := compile(off, nil)
	_, runs := compile(speed, new(int))
	if runs < 2 {
		t.Fatalf("the parser has %d runs of optional passes to bisect", runs)
	}
	for _, c := range []struct {
		limit int
		want  string
	}{{0, none}, {runs, all}} {
		if got, _ := compile(speed, &c.limit); got != c.want {
			t.Errorf("the expectation table is\n%s\nat the bisection limit %d of %d runs, not\n%s", got, c.limit, runs, c.want)
		}
	}
	mid := runs / 2
	first, _ := compile(speed, &mid)
	if again, _ := compile(speed, &mid); again != first {
		t.Errorf("the expectation table at the bisection limit %d is\n%s\nand then\n%s", mid, first, again)
	}
}

// funcsSeen records the functions that the passes of each table run on, by table.
type funcsSeen map[string]map[string]bool

func (funcsSeen) Needs() pipe.Needs                       { return pipe.Needs{} }
func (funcsSeen) Before(pipe.Event, func(io.Writer)) bool { return true }
func (s funcsSeen) After(e pipe.Event, r *pipe.Result) {
	if e.Label.Func != "" {
		if s[e.Label.Table] == nil {
			s[e.Label.Table] = map[string]bool{}
		}
		s[e.Label.Table][e.Label.Func] = true
	}
}

// stepNames records the instance and the table of every step that runs.
type stepNames struct{ seen *[]string }

func (stepNames) Needs() pipe.Needs                       { return pipe.Needs{} }
func (stepNames) Before(pipe.Event, func(io.Writer)) bool { return true }
func (s stepNames) After(e pipe.Event, r *pipe.Result) {
	if r.Ran && !e.Replayed {
		*s.seen = append(*s.seen, e.Step.Instance+" "+e.Label.Table)
	}
}
