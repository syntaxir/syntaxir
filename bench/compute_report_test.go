package bench

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gcalc "syntaxir.dev/sxir/internal/bench/parsers/calc"
	gcalclr "syntaxir.dev/sxir/internal/bench/parsers/calclr"
	gcsv "syntaxir.dev/sxir/internal/bench/parsers/csv"
	gcsvstream "syntaxir.dev/sxir/internal/bench/parsers/csvstream"
	gjson "syntaxir.dev/sxir/internal/bench/parsers/json"
	gjsonplain "syntaxir.dev/sxir/internal/bench/parsers/jsonplain"
	gminilangdoc "syntaxir.dev/sxir/internal/bench/parsers/minilangdoc"
	gxml "syntaxir.dev/sxir/internal/bench/parsers/xml"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// skipWithoutEngineControls skips a test that reads the engine's compute report: the reference build of
// the budget controls leaves the controls out of the engine, which then refuses the report.
func skipWithoutEngineControls(t *testing.T) {
	t.Helper()
	if !state.BudgetControlsEnabled {
		t.Skip("reads the engine's compute report, which the budget-control reference build leaves out")
	}
}

// WithComputeReport on the engine (on every backend) and on generated Go parsers with limit
// controls: the same units on success, on failure and when a limit stops the parse; a Compute
// limit of the report accepts the parse and one below it stops it, with the report equal to the
// units used when it stopped.

type reportCase struct {
	name    string
	grammar string
	good    string
	bad     string // an input that fails to parse
	gen     func(string, ...sxirruntime.ParseOption) error
}

func reportCases() []reportCase {
	return []reportCase{
		{"JSON", "../testdata/grammars/json/json.sxir", inputs.JSONInput(4 << 10), `{"a": [1, 2,, 3]}`,
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gjson.Parse(s, o...); return err }},
		{"CSV", "../testdata/grammars/csv/csv.sxir", inputs.CSVInput(40), "a,b\n\"c,d\n",
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gcsv.Parse(s, o...); return err }},
		{"XML", "../testdata/grammars/xml/xml.sxir", inputs.XMLInput(4 << 10), "<a><b></a>",
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gxml.Parse(s, o...); return err }},
		{"Arith_OperatorsRule", "../examples/calculator/calc.sxir", inputs.ArithInput(200), "1 + * 2",
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gcalc.Parse(s, o...); return err }},
		{"Arith_LeftRec", "../examples/calculator/calc_lr.sxir", inputs.ArithInput(200), "1 + * 2",
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gcalclr.Parse(s, o...); return err }},
	}
}

func spent(f func(string, ...sxirruntime.ParseOption) error, in string, o ...sxirruntime.ParseOption) (uint64, error) {
	var n uint64
	err := f(in, append([]sxirruntime.ParseOption{sxirruntime.WithComputeReport(&n)}, o...)...)
	return n, err
}

// reportFuncs are the entry points of one grammar: the generated parser and each backend of the
// engine.
func (c reportCase) funcs(t *testing.T) map[string]func(string, ...sxirruntime.ParseOption) error {
	p := load(t, c.grammar)
	fs := map[string]func(string, ...sxirruntime.ParseOption) error{"generated": c.gen}
	for _, be := range backends {
		fs["engine "+be.name] = func(s string, o ...sxirruntime.ParseOption) error {
			_, err := p.Parse(s, engine.WithBackend(be.b), engine.Use(o...))
			return err
		}
	}
	return fs
}

// Each parser reports what its own Compute limit counts: it reports on a success, on a failure and
// when a limit stops it, and a limit of its report accepts the parse while one below it stops the
// parse with the report equal to the units used at the stop. The parsers do not yet report the same
// number for the same input: the engine's backends and the generated parsers charge different
// events, so the numbers are logged, not compared. Units are identical on every backend by decision
// (design record 076), so the #310 commit that moves the VM to the logical units turns this check
// into a cross-backend equality check of the reports.
func TestComputeReport(t *testing.T) {
	skipWithoutEngineControls(t)
	for _, c := range reportCases() {
		for name, f := range c.funcs(t) {
			for un, unit := range logicalUnitsOf {
				for _, in := range []struct {
					kind  string
					text  string
					valid bool
				}{{"good", c.good, true}, {"bad", c.bad, false}} {
					label := c.name + "/" + name + "/" + un + "/" + in.kind
					r, err := spent(f, in.text, sxirruntime.WithUnit(unit))
					if (err == nil) != in.valid {
						t.Fatalf("%s: %v", label, err)
					}
					if r == 0 {
						t.Fatalf("%s: no units reported", label)
					}
					t.Logf("%s: %d units", label, r)
					if !in.valid {
						continue
					}
					if err := f(in.text, sxirruntime.WithUnit(unit), sxirruntime.WithLimits(sxirruntime.Limits{Compute: r})); err != nil {
						t.Errorf("%s: rejected at its report %d: %v", label, r, err)
					}
					for _, limit := range []uint64{r - 1, r / 2, 1} {
						got, err := spent(f, in.text, sxirruntime.WithUnit(unit), sxirruntime.WithLimits(sxirruntime.Limits{Compute: limit}))
						var le *sxirruntime.LimitError
						if !errors.As(err, &le) || le.Kind != sxirruntime.LimitCompute {
							t.Errorf("%s: not stopped by a Compute limit of %d: %v", label, limit, err)
							continue
						}
						if got != le.Used {
							t.Errorf("%s: with limit %d reported %d units, the error says %d", label, limit, got, le.Used)
						}
					}
				}
			}
		}
	}
}

// A parse with no limit and no context counts only because of the report, and a report does not
// change the result.
func TestComputeReportChangesNothing(t *testing.T) {
	in := inputs.JSONInput(4 << 10)
	want, err := gjson.ParseValue(in)
	if err != nil {
		t.Fatal(err)
	}
	var n uint64
	got, err := gjson.ParseValue(in, sxirruntime.WithComputeReport(&n), sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
	if err != nil || n == 0 || !sameJSON(t, got, want) {
		t.Fatalf("ParseValue with the report: units %d, err %v", n, err)
	}
	// Spent units are reset for the next parse, and a parse that never starts reports zero.
	n = 7
	if _, err := gjson.Parse(in, sxirruntime.WithComputeReport(&n), sxirruntime.WithLimits(sxirruntime.Limits{Input: 3})); !errors.Is(err, sxirruntime.ErrLimit) || n != 0 {
		t.Fatalf("input over the limit: units %d, err %v", n, err)
	}
	// A parser without limit controls cannot count.
	if _, err := gjsonplain.Parse(in, sxirruntime.WithComputeReport(&n)); err == nil || !strings.Contains(err.Error(), "without limit controls") {
		t.Fatalf("parser without limit controls: %v", err)
	}
}

// A stream and a Document report the units of their parse, on the engine and in generated parsers; a
// Document reports the same units on both.
func TestComputeReportStreamAndDocument(t *testing.T) {
	skipWithoutEngineControls(t)
	csvIn := inputs.CSVInput(40)
	var gs, es uint64
	err := gcsvstream.ParseStream(strings.NewReader(csvIn), func(*value.Node) error { return nil }, sxirruntime.WithComputeReport(&gs))
	if err != nil {
		t.Fatal(err)
	}
	p := load(t, "../testdata/grammars/csv/csv.sxir")
	if err := p.ParseStream(strings.NewReader(csvIn), func(*value.Node) error { return nil }, engine.Use(sxirruntime.WithComputeReport(&es))); err != nil {
		t.Fatal(err)
	}
	if gs == 0 || es == 0 {
		t.Errorf("stream: generated parser %d units, engine %d", gs, es)
	}
	t.Logf("stream: generated parser %d units, engine %d", gs, es)
	mini := load(t, "../examples/minilang/minilang.sxir")
	text := inputs.MinilangInput(5, 0)
	var gd, ed uint64
	gdoc, err := gminilangdoc.NewDocument(text)
	if err != nil {
		t.Fatal(err)
	}
	edoc, err := mini.NewDocument(text)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 { // the second parse reuses the first
		if _, err := gdoc.Parse(sxirruntime.WithComputeReport(&gd)); err != nil {
			t.Fatal(err)
		}
		if _, err := edoc.Parse(engine.Use(sxirruntime.WithComputeReport(&ed))); err != nil {
			t.Fatal(err)
		}
		// A Document parse counts the same logical units on every backend, a reusing one too.
		if gd == 0 || gd != ed {
			t.Errorf("Document parse %d: generated parser %d units, engine %d", i, gd, ed)
		}
	}
}

// A build with Documents runs the same rule functions for a plain parse and a Document parse, and a
// guard whose skip passes over a call of a rule that a Document memoizes is taken only outside
// Documents (flow.inDocument): each kind of parse counts the units of the engine's, on a valid input
// and an invalid one.
func TestComputeReportDocumentBuildAgreesWithEngine(t *testing.T) {
	skipWithoutEngineControls(t)
	src, err := os.ReadFile("../internal/bench/parsers/minilangdoc/parser.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "!(p.Document != nil) && ") {
		t.Fatal("the generated parser takes no guard only outside Documents: the check is void")
	}
	mini := load(t, "../examples/minilang/minilang.sxir")
	for _, text := range []string{inputs.MinilangInput(5, 0), inputs.MinilangInput(5, 0) + " fn ( {"} {
		var gp, ep uint64
		_, gerr := gminilangdoc.Parse(text, sxirruntime.WithComputeReport(&gp))
		_, eerr := mini.Parse(text, engine.Use(sxirruntime.WithComputeReport(&ep)))
		if (gerr == nil) != (eerr == nil) || gp == 0 || gp != ep {
			t.Errorf("plain parse: generated parser %d units (%v), engine %d (%v)", gp, gerr, ep, eerr)
		}
		gdoc, err := gminilangdoc.NewDocument(text)
		if err != nil {
			t.Fatal(err)
		}
		edoc, err := mini.NewDocument(text)
		if err != nil {
			t.Fatal(err)
		}
		var gd, ed uint64
		_, gerr = gdoc.Parse(sxirruntime.WithComputeReport(&gd))
		_, eerr = edoc.Parse(engine.Use(sxirruntime.WithComputeReport(&ed)))
		if (gerr == nil) != (eerr == nil) || gd == 0 || gd != ed {
			t.Errorf("Document parse: generated parser %d units (%v), engine %d (%v)", gd, gerr, ed, eerr)
		}
		t.Logf("valid %v: plain %d units, Document %d", gerr == nil, gp, gd)
	}
}

// WithContext, given after or before WithComputeReport, does not drop the report: the report is a
// field of the options, not something a context carries.
func TestComputeReportSurvivesContext(t *testing.T) {
	skipWithoutEngineControls(t)
	in := inputs.JSONInput(1 << 10)
	for _, c := range reportCases()[:1] {
		for name, f := range c.funcs(t) {
			for order, mk := range map[string]func(*uint64) []sxirruntime.ParseOption{
				"context after report": func(n *uint64) []sxirruntime.ParseOption {
					return []sxirruntime.ParseOption{sxirruntime.WithComputeReport(n), sxirruntime.WithContext(context.Background())}
				},
				"context before report": func(n *uint64) []sxirruntime.ParseOption {
					return []sxirruntime.ParseOption{sxirruntime.WithContext(context.Background()), sxirruntime.WithComputeReport(n)}
				},
			} {
				var n uint64
				if err := f(in, mk(&n)...); err != nil {
					t.Fatalf("%s/%s: %v", name, order, err)
				}
				if n == 0 {
					t.Errorf("%s/%s: the report was dropped", name, order)
				}
			}
		}
	}
}

// The memo report is a hook of the support library for tests: no exported name of the public runtime
// packages mentions it.
func TestMemoReportNotPublic(t *testing.T) {
	for _, dir := range []string{"../pkg/runtime", "../pkg/runtime/input", "../pkg/runtime/value"} {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range pkgs {
			for _, f := range pkg.Files {
				for _, d := range f.Decls {
					ast.Inspect(d, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && id.IsExported() && strings.Contains(id.Name, "MemoReport") {
							t.Errorf("%s: exported %s", fset.Position(id.Pos()), id.Name)
						}
						return true
					})
				}
			}
		}
	}
}

// A Document reparse after more edits than a memo entry may lag behind (pendingEditLimit is 128):
// its own work (entry advances and moves) is no event, so a Compute limit does not count it, yet the
// report is the units used, as for every parse. A limit at the report accepts the reparse, and a
// limit that stops it gives an error whose Used equals the report. Each reparse runs on a Document of
// its own, after the same edits: a reparse without edits since the last would reuse its whole result.
func TestComputeReportDocumentManyEdits(t *testing.T) {
	skipWithoutEngineControls(t)
	mini := load(t, "../examples/minilang/minilang.sxir")
	text := inputs.MinilangInput(5, 0)
	type doc struct {
		edit  func(int, int, string) error
		parse func(...sxirruntime.ParseOption) error
	}
	newDoc := func(name string) doc {
		if name == "generated" {
			gd, err := gminilangdoc.NewDocument(text)
			if err != nil {
				t.Fatal(err)
			}
			return doc{gd.Edit, func(o ...sxirruntime.ParseOption) error { _, err := gd.Parse(o...); return err }}
		}
		ed, err := mini.NewDocument(text)
		if err != nil {
			t.Fatal(err)
		}
		return doc{ed.Edit, func(o ...sxirruntime.ParseOption) error {
			_, err := ed.Parse(engine.Use(o...))
			return err
		}}
	}
	// reparse parses a fresh Document of text, edits it 300 times and parses it again with o, so that
	// every check is of the same reparse: a parse of a Document that has not changed since its last
	// parse reuses its result and counts only the call of the start rule.
	reparse := func(name string, o ...sxirruntime.ParseOption) error {
		d := newDoc(name)
		if err := d.parse(); err != nil {
			t.Fatal(name, err)
		}
		for i := range 300 {
			if err := d.edit(0, 0, " "); err != nil {
				t.Fatal(name, i, err)
			}
		}
		return d.parse(o...)
	}
	units := map[string]uint64{}
	for _, name := range []string{"generated", "engine"} {
		var n uint64
		if err := reparse(name, sxirruntime.WithComputeReport(&n)); err != nil {
			t.Fatal(name, err)
		}
		if n < 2 {
			t.Fatalf("%s: %d units reported", name, n)
		}
		units[name] = n
		t.Logf("%s: %d units after 300 edits", name, n)
		if err := reparse(name, sxirruntime.WithLimits(sxirruntime.Limits{Compute: n})); err != nil {
			t.Errorf("%s: a reparse is rejected at its report %d: %v", name, n, err)
		}
		var m uint64
		err := reparse(name, sxirruntime.WithComputeReport(&m), sxirruntime.WithLimits(sxirruntime.Limits{Compute: 1}))
		var le *sxirruntime.LimitError
		if !errors.As(err, &le) || le.Kind != sxirruntime.LimitCompute {
			t.Fatalf("%s: not stopped by a Compute limit of 1: %v", name, err)
		}
		if m != le.Used {
			t.Errorf("%s: reported %d units, the error says %d", name, m, le.Used)
		}
	}
	if units["generated"] != units["engine"] {
		t.Errorf("the generated parser counts %d units after the edits, the engine %d", units["generated"], units["engine"])
	}
}

// Iterate reports when the iteration ends, whether the caller drained it or stopped it early, and a
// Snapshot of a Document reports the units of its parse and capture. Both pass the option to the
// parse they run, so it is honoured, not rejected.
func TestComputeReportIterateAndSnapshot(t *testing.T) {
	skipWithoutEngineControls(t)
	csvIn := inputs.CSVInput(40)
	p := load(t, "../testdata/grammars/csv/csv.sxir")
	iterators := map[string]func(...sxirruntime.ParseOption) (*sxirruntime.StreamIterator, error){
		"generated": func(o ...sxirruntime.ParseOption) (*sxirruntime.StreamIterator, error) {
			return gcsvstream.Iterate(strings.NewReader(csvIn), o...)
		},
		"engine": func(o ...sxirruntime.ParseOption) (*sxirruntime.StreamIterator, error) {
			return p.Iterate(strings.NewReader(csvIn), engine.Use(o...))
		},
	}
	for name, mk := range iterators {
		var all, early uint64
		it, err := mk(sxirruntime.WithComputeReport(&all))
		if err != nil {
			t.Fatal(name, err)
		}
		for {
			_, ok, err := it.Next()
			if err != nil {
				t.Fatal(name, err)
			}
			if !ok {
				break
			}
		}
		if all == 0 {
			t.Errorf("%s: a drained iteration reported no units", name)
		}
		it, err = mk(sxirruntime.WithComputeReport(&early))
		if err != nil {
			t.Fatal(name, err)
		}
		if _, ok, err := it.Next(); err != nil || !ok {
			t.Fatal(name, ok, err)
		}
		it.Close()
		if early == 0 || early > all {
			t.Errorf("%s: an iteration stopped after one element reported %d units, a full one %d", name, early, all)
		}
		t.Logf("%s: iteration %d units, stopped early %d", name, all, early)
	}
	mini := load(t, "../examples/minilang/minilang.sxir")
	doc, err := mini.NewDocument(inputs.MinilangInput(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	var n uint64
	if _, err := doc.Snapshot(engine.Use(sxirruntime.WithComputeReport(&n))); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("a Snapshot reported no units")
	}
	t.Logf("snapshot: %d units", n)
}
