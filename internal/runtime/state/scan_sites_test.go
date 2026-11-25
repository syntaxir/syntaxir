package state_test

import (
	"fmt"
	"io"
	"maps"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// scanSiteGrammars are the grammars with scan sites of the tests of all targets.
var scanSiteGrammars = codegentest.ScanSiteGrammars

// scanSiteInputs are random inputs over alphabet and inputs with long runs that the walks restart at.
func scanSiteInputs(alphabet string, rng *rand.Rand) []string {
	return codegentest.ScanSiteInputs(alphabet, 300, rng)
}

// TestScanSitesMatchParsesWithoutRecords checks that the records of scan sites change no result:
// values and syntax errors with their expectations, against walks without records and a program
// compiled without scan sites, and that the calls reach the records.
func TestScanSitesMatchParsesWithoutRecords(t *testing.T) {
	t.Parallel()
	statetest.AtHotSpans(t, scanSitesMatchWithoutRecords)
}

func scanSitesMatchWithoutRecords(t *testing.T, tune state.Tuning) {
	rng := rand.New(rand.NewSource(131))
	for _, g := range scanSiteGrammars {
		prog := enginetest.MustCompile(t, g.Src)
		if prog.NumScanSites == 0 {
			t.Errorf("%s: no scan sites", g.Name)
		}
		plain := compileWithoutScanSites(t, g.Src)
		inputs := scanSiteInputs(g.Alphabet, rng)
		hits := tune.Stats.ScanHits.Load()
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			hitsB := tune.Stats.ScanHits.Load()
			if diffs := skipRunMismatches(t, prog, inputs, b, tune); len(diffs) > 0 {
				t.Errorf("%s, %s: %d parses differ without records, first:\n%s", g.Name, b, len(diffs), diffs[0])
			}
			for _, in := range inputs {
				for _, exp := range []bool{true, false} {
					o := engine.ParseOptions{Backend: b, Expectations: exp, Tune: tune}
					n, err := prog.ParseWith("main", in, o)
					wn, werr := plain.ParseWith("main", in, o)
					if got, want := skipDump(t, n, err), skipDump(t, wn, werr); got != want {
						t.Fatalf("%s, %s: input %q\n got  %s\n want %s", g.Name, b, in, got, want)
					}
				}
			}
			if tune.ScanHotSpan() <= 2 && tune.Stats.ScanHits.Load() == hitsB {
				t.Errorf("%s, %s: no call was answered from a record", g.Name, b)
			}
		}
		if tune.ScanHotSpan() <= 2 && tune.Stats.ScanHits.Load() == hits {
			t.Errorf("%s: no call was answered from a record", g.Name)
		}
	}
}

// TestScanSitesAreMarked checks which repetitions are scan sites: those of one character without a
// maximum in the rules that call no rule, whatever the rule does with their values.
func TestScanSitesAreMarked(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, src string
		sites     int
	}{
		{"a pure rule called twice", "rule main = r \"a\" / r \"b\"\nrule r = [x]*", 1},
		{"a pure rule called once", "rule main = r \"a\"\nrule r = [x]*", 1},
		{"a rule that calls a rule", "rule main = r \"a\" / r \"b\"\nrule r = s*\nrule s = \"x\"", 0},
		{"a rule with a capture", "rule main = r \"a\" / r \"b\"\nrule r = c:[x]*", 1},
		{"a rule with an action", "rule main = r \"a\" / r \"b\"\nrule r = [x]* => 1", 1},
		{"a rule with a predicate", "var n int\nrule main = r \"a\" / r \"b\"\nrule r = ([x] { n < 3 })*", 0},
		{"a rule with a lookahead", "rule main = r \"a\" / r \"b\"\nrule r = (![y] [x])*", 0},
		{"a rule with a pragma", "rule main = r \"a\" / r \"b\"\nrule r = [x]* #atomic", 1},
		{"repetitions in the calling rule", "rule main = [x]* \"a\" [y]* / [x]+ \"b\"", 3},
		{"a repetition of a literal", "rule main = r \"a\"\nrule r = \"x\"*", 0},
		{"a repetition with a maximum", "rule main = r \"a\"\nrule r = [x]#repeat(0,9)", 0},
		{"a repetition of a choice", "rule main = r \"a\"\nrule r = (\"x\" / \"yz\")*", 0},
		{"a repetition in a stream", "rule main = [x]* #stream", 0},
	} {
		if n := enginetest.MustCompile(t, c.src).NumScanSites; n != c.sites {
			t.Errorf("%s: %d scan sites, want %d", c.name, n, c.sites)
		}
	}
}

// TestScanSitesReuseRecords checks that a parse that restarts a scan at one position in each level
// of a recursion fits a budget that is linear in the input with records, and exceeds it without them.
func TestScanSitesReuseRecords(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	prog := enginetest.MustCompile(t, scanSiteGrammars[2].Src)
	in := strings.Repeat("(", 1000) + strings.Repeat(" ", 1000)
	o := engine.ParseOptions{Backend: engine.Closure, Limits: runtime.Limits{Compute: 200_000}}
	if _, err := prog.ParseWith("main", in, o); err != nil {
		t.Errorf("the parse with records: %v", err)
	}
	o.Tune.SkipOff = true
	if _, err := prog.ParseWith("main", in, o); err == nil {
		t.Errorf("the parse without records fits the budget")
	}
}

// TestScanSitesInDocuments checks that a Document parse after edits inside and next to a run that a
// record covers gives the tree and errors of a fresh parse of the text.
func TestScanSitesInDocuments(t *testing.T) {
	t.Parallel()
	stats := new(state.HotStats)
	for _, g := range scanSiteGrammars {
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			if d := scanDocumentMismatch(t, g.Src, g.Alphabet, b, stats); d != "" {
				t.Errorf("%s, %s: %s", g.Name, b, d)
			}
		}
	}
	if stats.ScanHits.Load() == 0 {
		t.Errorf("no call was answered from a record")
	}
}

// scanDocumentMismatch edits Documents of the grammar src, whose scan sites turn hot at two
// characters, and returns the first difference from a fresh parse of the edited text.
func scanDocumentMismatch(t *testing.T, src, alphabet string, b engine.Backend, stats *state.HotStats) string {
	t.Helper()
	rng := rand.New(rand.NewSource(133))
	prog := enginetest.MustCompile(t, src)
	var pool []string
	for _, in := range scanSiteInputs(alphabet, rng) {
		if len(in) >= 40 {
			pool = append(pool, in)
		}
	}
	for i := 0; i < 40; i++ {
		o := engine.ParseOptions{Backend: b, Expectations: true, Tune: state.Tuning{ScanHot: 2, Stats: stats}}
		d, err := prog.NewDocumentWith("main", pool[rng.Intn(len(pool))], o)
		if err != nil {
			t.Fatal(err)
		}
		d.Parse()
		for j := 0; j < 6; j++ {
			cur := d.Text()
			a := rng.Intn(len(cur) + 1)
			b, ins := a, ""
			switch rng.Intn(3) {
			case 0:
				b = min(len(cur), a+1+rng.Intn(3))
			case 1:
				ins = string(alphabet[rng.Intn(len(alphabet))])
			default:
				b = min(len(cur), a+rng.Intn(3))
				ins = string(alphabet[rng.Intn(len(alphabet))])
			}
			if err := d.Edit(a, b, ins); err != nil {
				t.Fatalf("edit: %v", err)
			}
			n, err := d.Parse()
			wn, werr := prog.ParseWith("main", d.Text(), o)
			if got, want := skipDump(t, n, err), skipDump(t, wn, werr); got != want {
				return "text " + d.Text() + "\n got  " + got + "\n want " + want
			}
		}
	}
	return ""
}

// TestScanSitesNegativeControls checks that the budget test sees a record that is not reused: one
// that keeps the position of its walk and one that is never made. The record is a shortcut that the
// walk completes: the iteration after the jump fails at the end and examines the input there, so a
// record without its end only costs the walk again, and one without its examined range examines the
// same input. Neither changes a result, and the budget is where they show.
func TestScanSitesNegativeControls(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	prog := enginetest.MustCompile(t, scanSiteGrammars[2].Src)
	in := strings.Repeat("(", 1000) + strings.Repeat(" ", 1000)
	for _, m := range []struct {
		name string
		mut  int
	}{{"end", state.SkipKeepPos}, {"making", state.SkipNeverRecord}} {
		o := engine.ParseOptions{Backend: engine.Closure, Limits: runtime.Limits{Compute: 200_000}, Tune: state.Tuning{SkipMut: m.mut}}
		if _, err := prog.ParseWith("main", in, o); err == nil {
			t.Errorf("a record without its %s goes unseen", m.name)
		}
	}
}

// TestScanSitesInStreams checks that a stream that delivers a run of a scan site in pieces, so
// that a call ends where the available input does, emits what a parse of the whole text builds.
func TestScanSitesInStreams(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = row* #stream $$
rule row = ws "x" ws ";" / ws "x" ws "!" / ws "y" ws ";"
rule ws = [ ]*`)
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		scanSitesInStreams(t, prog, b)
	}
}

// scanSitesInStreams parses streams of random rows split by readers that deliver the text in small
// pieces and compares them with the unrecorded parse of the stream, on the backend b, with scan
// sites that turn hot at two characters. A walk that
// reaches the end of the loaded input must not record, and the others may, so some parse takes a
// record. On the bytecode backends the walks of ws build lists, which the valued ITER serves.
func scanSitesInStreams(t *testing.T, prog *engine.Program, b engine.Backend) {
	t.Helper()
	stats := new(state.HotStats)
	defer func() {
		if stats.ScanHits.Load() == 0 {
			t.Errorf("%s: no stream parse was answered from a record", b)
		}
	}()
	rng := rand.New(rand.NewSource(135))
	for i := 0; i < 200; i++ {
		var sb strings.Builder
		for j := 0; j < 1+rng.Intn(5); j++ {
			sb.WriteString(strings.Repeat(" ", rng.Intn(50)))
			sb.WriteString(string("xy"[rng.Intn(2)]))
			sb.WriteString(strings.Repeat(" ", rng.Intn(50)))
			sb.WriteString(string(";!"[rng.Intn(2)]))
		}
		text := sb.String()
		var want []string
		o := engine.ParseOptions{Backend: b, Expectations: true, Tune: state.Tuning{ScanHot: 2, Stats: stats}}
		off := o
		off.Tune.SkipOff = true
		werr := prog.ParseStreamWith("main", strings.NewReader(text), func(n *value.Node) error { want = append(want, skipDump(t, n, nil)); return nil }, off)
		for _, mk := range []func(io.Reader) io.Reader{iotest.OneByteReader, iotest.HalfReader, iotest.DataErrReader} {
			var got []string
			err := prog.ParseStreamWith("main", mk(strings.NewReader(text)), func(n *value.Node) error { got = append(got, skipDump(t, n, nil)); return nil }, o)
			if (err == nil) != (werr == nil) || strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("%s: text %q: got %v (%v), want %v (%v)", b, text, got, err, want, werr)
			}
		}
	}
}

// scanSteps returns the compute steps that the parse of in with the test-only settings tune charges.
func scanSteps(t *testing.T, prog *engine.Program, in string, b engine.Backend, tune state.Tuning) uint64 {
	t.Helper()
	var steps uint64
	o := engine.ParseOptions{Backend: b, Limits: runtime.Limits{Compute: 1 << 40}, Tune: tune, Trace: func(e engine.TraceEvent) {
		if e.Kind == engine.TraceExit && e.Rule == "main" {
			steps = state.TraceParser(e).Control.Used
		}
	}}
	prog.ParseWith("main", in, o)
	return steps
}

// TestScanSitesChargeBounds checks the cost that a scan site adds to a parse. The charge of a parse
// is the same each time it is made, and a site adds to the charge of the walks without it at most two
// steps of each walk that reaches the hot span (the lookup and the slot, or the lookup and the
// record), which is
// the number of its steps divided by the span at most. Reuse only lowers the charge.
func TestScanSitesChargeBounds(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	for _, span := range []int{64, 1} {
		func() {
			tune := state.Tuning{ScanHot: span}
			rng := rand.New(rand.NewSource(137))
			for _, g := range scanSiteGrammars {
				prog := enginetest.MustCompile(t, g.Src)
				plain := compileWithoutScanSites(t, g.Src)
				for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
					for _, in := range scanSiteInputs(g.Alphabet, rng) {
						got, again, base := scanSteps(t, prog, in, b, tune), scanSteps(t, prog, in, b, tune), scanSteps(t, plain, in, b, tune)
						if got == 0 || got != again {
							t.Fatalf("%s, %s, span %d: input %q charged %d and %d steps", g.Name, b, span, in, got, again)
						}
						if bound := base + 2*(base/uint64(span)) + 2; got > bound {
							t.Fatalf("%s, %s, span %d: input %q charged %d steps, %d without scan sites (bound %d)", g.Name, b, span, in, got, base, bound)
						}
					}
				}
			}
		}()
	}
}

// TestScanSitesCompileToInstructions checks that the Flow bodies of the scan sites hold the
// instructions that serve them, are accepted by the module validator and are rejected when they name a
// scan site that the module cannot hold.
func TestScanSitesCompileToInstructions(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = (w / r "?" / [ab ])* $$
rule w = (r "!" / r ".") #atomic
rule r = [ab]*`)
	m := prog.Module()
	if err := program.ValidateModule(m); err != nil {
		t.Fatalf("the module of scan sites is rejected: %v", err)
	}
	ops := map[bytecode.FlowOp]int{}
	for rule, f := range m.Flows {
		for pc, in := range f.Code {
			if s, _ := in.Op.Shape(); s.Imm != bytecode.FlowImmScanSite {
				continue
			}
			ops[in.Op]++
			bad := *m
			bad.Flows = maps.Clone(m.Flows)
			g := &bytecode.Flow{Regs: f.Regs, Code: slices.Clone(f.Code)}
			g.Code[pc].Imm = 0
			bad.Flows[rule] = g
			if err := program.ValidateModule(&bad); err == nil {
				t.Errorf("rule %d: instruction %d (%v) with an invalid scan site is accepted", rule, pc, in.Op)
			}
		}
	}
	if ops[bytecode.FlowScanReachAt] == 0 || ops[bytecode.FlowScanEndAt] == 0 {
		t.Fatalf("scan site instructions %v, want SCANREACHAT and SCANENDAT", ops)
	}
}

// TestScanSitesWithBoundsBeyondInstructions checks the repetitions whose minimum does not fit an
// instruction operand: they are walked without a scan site and give what the program without
// scan sites gives.
func TestScanSitesWithBoundsBeyondInstructions(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = (w / r "?" / [ab ])* $$
rule w = (r "!" / r ".") #atomic
rule r = [ab] #repeat(5000000000,)`,
		`rule main = (r "!" / r "?" / [ab ])* $$
rule r = [ab] #repeat(5000000000,)`,
	} {
		prog := enginetest.MustCompile(t, src)
		plain := compileWithoutScanSites(t, src)
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			for _, in := range []string{"", "ab!", strings.Repeat("a", 200) + "!", strings.Repeat("ab", 90) + "?"} {
				o := engine.ParseOptions{Backend: b, Expectations: true}
				n, err := prog.ParseWith("main", in, o)
				wn, werr := plain.ParseWith("main", in, o)
				if got, want := skipDump(t, n, err), skipDump(t, wn, werr); got != want {
					t.Fatalf("%s: input %q\n got  %s\n want %s", b, in, got, want)
				}
			}
		}
	}
}

// compileWithoutScanSites compiles src with the marking of scan sites off (see compileWithout).
func compileWithoutScanSites(t *testing.T, src string) *engine.Program {
	t.Helper()
	return compileWithout(t, src, check.Options{NoScanSites: true})
}

// compileWithout compiles src with the options opts, which switch off the numbering of run or scan
// sites (check.Options.NoRunSites, NoScanSites). The programs derived from it (the recognizer, the
// controlled graph of the closure backend) carry the options with it, so the program is plain on every
// backend, and no other test sees the switch.
func compileWithout(t *testing.T, src string, opts check.Options) *engine.Program {
	t.Helper()
	prog := enginetest.MustCompile(t, src, opts)
	if opts.NoRunSites && prog.NumRunSites != 0 {
		t.Fatalf("the plain program numbers %d run sites, want none", prog.NumRunSites)
	}
	if (opts.NoRunSites || opts.NoScanSites) && prog.NumScanSites != 0 {
		t.Fatalf("the plain program numbers %d scan sites, want none", prog.NumScanSites)
	}
	return prog
}

// scanSpanProbe restarts the walk of ws at the start of each run of spaces in five alternatives.
const scanSpanProbe = `rule main = (ws "a" / ws "b" / ws "c" / ws "d" / ws "e")* $$
rule ws = [ ]*`

// TestScanSitesHitAtTheirOwnHotSpan checks that walks longer than the hot span of scan sites and
// shorter than that of run sites are answered from records on every backend: the hot span of scan
// sites is its own (Tuning.ScanHotSpan), not that of run sites.
func TestScanSitesHitAtTheirOwnHotSpan(t *testing.T) {
	t.Parallel()
	scanSpan, runSpan := new(state.Tuning).ScanHotSpan(), new(state.Tuning).RunHotSpan()
	if scanSpan >= runSpan {
		t.Fatalf("the hot span of scan sites (%d) is not below that of run sites (%d)", scanSpan, runSpan)
	}
	tune := state.Tuning{Stats: new(state.HotStats)}
	prog := enginetest.MustCompile(t, scanSpanProbe)
	rec, err := prog.Recognizer()
	if err != nil {
		t.Fatal(err)
	}
	// Parsing walks the scan sites that build values and recognition those that do not.
	for name, p := range map[string]*engine.Program{"parse": prog, "recognize": rec} {
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			for _, n := range []int{scanSpan + 4, 40, runSpan - 1} {
				in := strings.Repeat(" ", n) + "e"
				before := tune.Stats.ScanHits.Load()
				if _, err := p.ParseWith("main", in, engine.ParseOptions{Backend: b, Tune: tune}); err != nil {
					t.Fatalf("%s, %s: run of %d: %v", name, b, n, err)
				}
				if tune.Stats.ScanHits.Load() == before {
					t.Errorf("%s, %s: a run of %d spaces was not answered from a record", name, b, n)
				}
			}
		}
	}
}

// TestScanSitesInConcurrentParses runs parses that reach records on one program from many goroutines
// (it is meaningful under -race): the counters that tests read are atomic and parses share no
// other state.
func TestScanSitesInConcurrentParses(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, scanSpanProbe)
	lim := runtime.Limits{Compute: 1 << 40}
	if !state.BudgetControlsEnabled {
		// The reference build has no budget controls; the parses still share the counters.
		lim = runtime.Limits{}
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
					in := strings.Repeat(" ", 20+(g+i)%50) + "e"
					if _, err := prog.ParseWith("main", in, engine.ParseOptions{Backend: b, Limits: lim}); err != nil {
						t.Errorf("%s: %v", b, err)
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestScanSitesChargeIsPinned pins the units that parses of scanSpanProbe count with and without
// scan sites, for a run of spaces as long as the hot span and one four characters longer. The
// lookups and records of a scan site count nothing, nor does the input a record skips (see "Compute
// Units" in spec/runtime/parsing.md), so the parse with a record counts less than the one without;
// counting any of them changes these numbers, which are the same on every backend.
func TestScanSitesChargeIsPinned(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	if span := new(state.Tuning).ScanHotSpan(); span != 16 {
		t.Fatalf("the hot span of scan sites is %d, not the 16 the pinned charges assume", span)
	}
	prog, plain := enginetest.MustCompile(t, scanSpanProbe), compileWithoutScanSites(t, scanSpanProbe)
	type steps struct{ scan, plain uint64 }
	want := map[int]steps{16: {92, 92}, 20: {104, 112}}
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for n, w := range want {
			in := strings.Repeat(" ", n) + "e"
			if got := (steps{scanSteps(t, prog, in, b, state.Tuning{}), scanSteps(t, plain, in, b, state.Tuning{})}); got != w {
				t.Errorf("%s, run of %d: charged %d steps with scan sites and %d without, want %d and %d", b, n, got.scan, got.plain, w.scan, w.plain)
			}
		}
	}
}

// scanExamined returns the rule, the position and the examined range (the high-water mark) at each
// exit of a rule in the parse of in, and its result.
func scanExamined(t *testing.T, prog *engine.Program, in string, o engine.ParseOptions) (out []string, tree string) {
	t.Helper()
	o.Trace = func(e engine.TraceEvent) {
		if e.Kind == engine.TraceExit {
			out = append(out, fmt.Sprintf("%s@%d hw=%d", e.Rule, e.Pos, state.TraceParser(e).Hw))
		}
	}
	n, err := prog.ParseWith("main", in, o)
	return out, skipDump(t, n, err)
}

// scanLookahead examines further than its scan sites do before they are walked, so a walk that
// does not hand the examined range back to its caller shortens the range the rules report. The first
// alternative of r reads past the end of the walk that follows the second, and the lookahead of main
// does so before the walks of ws.
const scanLookahead = `rule main = &(ws "z") (ws "a" / ws "b" / ws "c" / ws "d" / ws "z")* $$
rule ws = [ ]*`

// scanInvalidBytes walks over invalid UTF-8. Decoding a truncated prefix examines the byte that
// makes it invalid, or the end of the input, so that the walk examines past the character that
// ends it.
const scanInvalidBytes = `rule main = (r "?" / r "." / r ":" / r ";" / r "!")* $$
rule r = [^!]*`

const scanEarlierExamined = `rule main = (r "!" / r "?" / r "." / r ":" / r ";")* $$
rule r = ("xxxxxxxxxxxxxxxxxxxxyzzzzzB" / "x") [x]*`

// TestScanSitesKeepTheExaminedRange checks that the range of input that rules report to have
// examined (the high-water mark that the memo and the incremental parse rely on) is the same with
// and without scan sites, for walks that look up a record, find one and store one.
func TestScanSitesKeepTheExaminedRange(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(139))
	grammars := append([]struct{ Name, Src, Alphabet string }{
		{"a lookahead that examines further", scanLookahead, " abdz"},
		{"a rule that examined further before the walk", scanEarlierExamined, "xyz!?.:;"},
		{"invalid bytes examined up to the byte that invalidates them", scanInvalidBytes, "\xffz!?é"},
	}, scanSiteGrammars...)
	for _, span := range []int{16, 2} {
		for _, g := range grammars {
			prog, plain := enginetest.MustCompile(t, g.Src), compileWithoutScanSites(t, g.Src)
			inputs := append([]string{strings.Repeat(" ", 30) + "z", strings.Repeat(" ", 20) + "z ", strings.Repeat("x", 20) + "yzzzzzA!", strings.Repeat("x", 20) + "yzzzzzA?", strings.Repeat("\xff", 20) + "!zzz", strings.Repeat("\xff", 20) + "!z", strings.Repeat("é", 20) + "!zzz", strings.Repeat("é", 20) + "?", strings.Repeat("\xe2\x82", 10) + "!zzz", strings.Repeat("\xe2\x82!", 10) + "\xe2\x82"}, scanSiteInputs(g.Alphabet, rng)...)
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
					o := engine.ParseOptions{Backend: b, Unit: unit, Tune: state.Tuning{ScanHot: span}}
					for _, in := range inputs {
						got, gotTree := scanExamined(t, prog, in, o)
						want, wantTree := scanExamined(t, plain, in, o)
						if !slices.Equal(got, want) || gotTree != wantTree {
							t.Fatalf("%s, %s, %v, span %d: input %q\n got  %v %s\n want %v %s", g.Name, b, unit, span, in, got, gotTree, want, wantTree)
						}
					}
				}
			}
		}
	}
}

// scanSharedTable is a grammar whose walks of ws are, in order: two in lookaheads, which turn the site
// hot there; three outside, which turn it hot there and store a record at the third; and a lookahead
// walk and a walk outside that follow.
const scanSharedTable = `rule main = (&(ws "x") ws "a" / &(ws "y") ws "b" / ws "c" / ws "d" / ws "f" / &(ws "e") ws "e")* $$
rule ws = [ ]*`

// scanSharedTableFromLookahead walks ws three times in lookaheads, the third of which stores a record,
// and then four times outside, the third and fourth of which find it.
const scanSharedTableFromLookahead = `rule main = (&(ws "p") ws "a" / &(ws "q") ws "b" / &(ws "r") ws "c" / ws "d" / ws "f" / ws "g" / ws "e")* $$
rule ws = [ ]*`

// TestScanSitesShareRecordsWithLookaheads checks that, in recognition, a walk in a lookahead is
// answered from the record of a walk outside it: a site has one table for both, as their records
// are the same (see scanSlots in internal/runtime/state/scan_sites.go). A parse that builds values
// walks lookaheads with a site of its own (the one without values), so they share nothing there;
// the results are the same either way.
func TestScanSitesShareRecordsWithLookaheads(t *testing.T) {
	t.Parallel()
	tune := state.Tuning{Stats: new(state.HotStats)}
	for _, src := range []string{scanSharedTable, scanSharedTableFromLookahead} {
		prog, plain := enginetest.MustCompile(t, src), compileWithoutScanSites(t, src)
		rec, err := prog.Recognizer()
		if err != nil {
			t.Fatal(err)
		}
		in := strings.Repeat(" ", 30) + "e"
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			n, err := prog.ParseWith("main", in, engine.ParseOptions{Backend: b})
			wn, werr := plain.ParseWith("main", in, engine.ParseOptions{Backend: b})
			if skipDump(t, n, err) != skipDump(t, wn, werr) {
				t.Errorf("%s: the results differ", b)
			}
			before := tune.Stats.ScanHits.Load()
			if _, err := rec.ParseWith("main", in, engine.ParseOptions{Backend: b, Tune: tune}); err != nil {
				t.Fatal(err)
			}
			if hits := tune.Stats.ScanHits.Load() - before; hits != 2 {
				t.Errorf("%s: %d walks were answered from a record, want 2", b, hits)
			}
		}
	}
}

// documentSteps returns the steps that the first parse of a Document of text and the parse after
// replacing text[a:b] with ins charge.
func documentSteps(t *testing.T, prog *engine.Program, b engine.Backend, text string, a, e int, ins string) (first, edit uint64) {
	t.Helper()
	var steps uint64
	o := engine.ParseOptions{Backend: b, Limits: runtime.Limits{Compute: 1 << 40}, Trace: func(ev engine.TraceEvent) {
		if ev.Kind == engine.TraceExit && ev.Rule == "main" {
			steps = state.TraceParser(ev).Control.Used
		}
	}}
	d, err := prog.NewDocumentWith("main", text, o)
	if err != nil {
		t.Fatal(err)
	}
	d.Parse()
	first = steps
	if err := d.Edit(a, e, ins); err != nil {
		t.Fatal(err)
	}
	d.Parse()
	return first, steps
}

// TestScanSitesResumeInDocuments checks that a Document resumes the repetition of a scan site that is
// also a resumable run, as it does one that is not a scan site: the parse after an edit charges the
// steps it charges without scan sites. The first edit is at the
// end of a long run, where the whole run before the edit is resumed (and the parse charges less than
// the first), and the second inside the first sixteen characters, where the walk that follows the
// resumed part goes on past the hot span.
func TestScanSitesResumeInDocuments(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	const src = `rule main = (ws "a" / ws "b" / ws "c" / ws "d" / ws "e")* $$
rule ws = [ ]*`
	prog, plain := enginetest.MustCompile(t, src), compileWithoutScanSites(t, src)
	cases := []struct {
		name          string
		text          string
		a, e          int
		ins           string
		lessThanFirst bool
	}{
		{"an edit at the end of a long run", strings.Repeat(" ", 3000) + "a", 3000, 3001, "b", true},
		{"an edit inside the hot span", strings.Repeat(" ", 40) + "e", 3, 3, "  ", false},
	}
	// Edits at every position of a run of 40 blanks: some leave a resumed prefix of exactly the hot
	// span, after which the walk goes on.
	for a := 1; a < 36; a++ {
		cases = append(cases, struct {
			name          string
			text          string
			a, e          int
			ins           string
			lessThanFirst bool
		}{fmt.Sprintf("an edit at %d of a run of 40", a), strings.Repeat(" ", 40) + "e", a, a, " ", false})
	}
	for _, c := range cases {
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			first, edit := documentSteps(t, prog, b, c.text, c.a, c.e, c.ins)
			_, want := documentSteps(t, plain, b, c.text, c.a, c.e, c.ins)
			if edit != want || (c.lessThanFirst && edit >= first) {
				t.Errorf("%s, %s: the parse after the edit charged %d steps, %d without scan sites, and the first parse %d", c.name, b, edit, want, first)
			}
		}
	}
}
