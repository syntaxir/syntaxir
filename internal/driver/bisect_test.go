package driver

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/pass"
)

// bisectGrammars are a grammar with every kind of decision of the printing steps and the passes with
// a count (equal bodies, equal literals, short literals, classes of several ranges, an unreachable
// rule), and one whose TypeScript parser falls back to the reach of its printed code.
var bisectGrammars = []string{
	"@start\nrule A = \"ab\" B (\"ab\" / C / E)* D\nrule B = [a-cx-z0-9]+\nrule C = \"c\" [a-cx-z0-9]\nrule E = \"c\" [a-cx-z0-9]\nrule D = (\"d\" / \"e\") (\"d\" / \"e\")\nrule U = \"u\" B\n",
	forkGrammars[1],
}

// bisectTargets are the targets whose decisions the bisection tests cover: a typed Go parser and a
// TypeScript parser, both with their library embedded, so that its pruning is decided too.
func bisectTargets() []Target {
	return []Target{
		{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/p", Lib: common.LibEmbed, LibGo: libsrc.FS, Types: true}},
		{Backend: result.TypeScript, Gen: common.GenOptions{Lib: common.LibEmbed, TSLib: tslib.FS, Types: true}},
	}
}

// bisectCompile generates the files of the target from p with the settings s and the bisection limit
// limit (none if nil) and returns them as one text, with the decision log.
func bisectCompile(t *testing.T, p *Program, s pass.Settings, limit *int, tg Target) (files, log string) {
	t.Helper()
	var w strings.Builder
	out, err := Compile(context.Background(), p, Options{Settings: s, Debug: common.Debug{OptBisectLimit: limit, Output: &w}}, tg)
	if err != nil {
		t.Fatalf("%s at the limit %v: %v", tg.Backend, limitText(limit), err)
	}
	return filesText(t, out[0]), w.String()
}

func limitText(limit *int) any {
	if limit == nil {
		return "none"
	}
	return *limit
}

// The invariants that make the bisection usable (record 080, Bisection), within the bisection mode:
// under a limit a build generates once, so that one sequence numbers every decision (common.Debug.
// OptBisectLimit). A limit of 0 generates the parser of every setting off, a limit beyond the last
// decision the parser of the settings, and the same limit twice the same parser and the same decision
// log. The settings are those of speed and those with the opt-ins that have decisions of their own on
// too (class-tables, ts-literal-sharing), so that every decision point is asked. Limit 0 skips every
// application of canonicalize too, which every setting off still runs: the parsers agree because
// canonicalize changes no parser (optimization record 142).
func TestBisectionInvariants(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	optIns, err := speed.With("class-tables", 2)
	if err == nil {
		optIns, err = optIns.With("ts-literal-sharing", 1)
	}
	if err != nil {
		t.Fatal(err)
	}
	off := everySettingOff(t)
	zero, beyond := 0, 1<<30
	asked := map[string]bool{}
	for _, src := range bisectGrammars {
		p, err := checkSource(t, src, common.CompileOptions{}, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range bisectTargets() {
			none, _ := bisectCompile(t, p, off, &beyond, tg)
			for _, s := range []pass.Settings{speed, optIns} {
				all, log := bisectCompile(t, p, s, &beyond, tg)
				for _, line := range strings.Split(log, "\n") {
					if f := strings.Fields(line); len(f) >= 4 && f[0] == "opt-bisect:" {
						asked[tg.Backend.String()+" "+f[3]] = true
					}
				}
				runs := strings.Count(log, "opt-bisect: ")
				if got, _ := bisectCompile(t, p, s, &zero, tg); got != none {
					t.Errorf("%s: the parser at the limit 0 is not that of every setting off:\n%s", tg.Backend, firstDiff(got, none))
				}
				if got, _ := bisectCompile(t, p, s, &runs, tg); got != all {
					t.Errorf("%s: the parser at the limit %d, the last decision, is not that of the settings:\n%s", tg.Backend, runs, firstDiff(got, all))
				}
				mid := runs / 2
				first, firstLog := bisectCompile(t, p, s, &mid, tg)
				if again, againLog := bisectCompile(t, p, s, &mid, tg); again != first || againLog != firstLog {
					t.Errorf("%s: the limit %d gives another parser or decision log the second time:\n%s", tg.Backend, mid, firstDiff(again+againLog, first+firstLog))
				}
			}
		}
	}
	for _, want := range []string{
		"go print-go/method-sharing", "go print-go/literal-sharing", "go print-go/used-features",
		"go print-go/typed-object-slabs", "go print-go/typed-list-slabs", "go print-go/typed-output-bootstrap", "go print-go/typed-chunk-growth",
		"ts print-ts/method-sharing", "ts print-ts/literal-sharing", "ts print-ts/used-features", "ts print-ts/sparse-memo", "ts ts-library/used-features",
	} {
		if !asked[want] {
			t.Errorf("no decision %s was asked", want)
		}
	}
	for _, pattern := range []string{"inline-literals/inline-literals", "class-tables/class-tables"} {
		found := false
		for k := range asked {
			found = found || strings.HasSuffix(k, pattern)
		}
		if !found {
			t.Errorf("no decision %s was asked", pattern)
		}
	}
}

var updateBisect = flag.Bool("update-bisect", false, "rewrite the golden decision logs")

// The decision log of the first bisection grammar on each target, with the opt-ins on, is pinned:
// a change in what is decided, in which order or how often shows as a diff of the golden log
// (testdata/bisect-<backend>.txt; -update-bisect rewrites it). That each decision is asked once for
// its site is checked by the manager in every observed run (pipe.Ctx.Allow).
func TestBisectionDecisionLogs(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	optIns, err := speed.With("class-tables", 2)
	if err == nil {
		optIns, err = optIns.With("ts-literal-sharing", 1)
	}
	if err != nil {
		t.Fatal(err)
	}
	p, err := checkSource(t, bisectGrammars[0], common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	beyond := 1 << 30
	for _, tg := range bisectTargets() {
		_, log := bisectCompile(t, p, optIns, &beyond, tg)
		path := filepath.Join("testdata", "bisect-"+tg.Backend.String()+".txt")
		if *updateBisect {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if log != string(want) {
			t.Errorf("%s: the decision log differs from %s (-update-bisect rewrites it):\n%s", tg.Backend, path, firstDiff(log, string(want)))
		}
	}
}

// everySettingOff returns the settings with every optimization off.
func everySettingOff(t *testing.T) pass.Settings {
	t.Helper()
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
	return off
}

// Canonicalization changes no parser (optimization record 142). With every setting off, the only
// decisions are the applications of the canonicalize patterns, so the limit 0 skips each of them and
// a limit beyond the last runs each. The first alternative of the grammar's start rule cannot match
// (!""* fails), so the code after its predicate is unreachable, which drop-after-exit removes: the
// printers must form nothing for such code, or the names of the tables after it would shift.
func TestCanonicalizationChangesNoParser(t *testing.T) {
	t.Parallel()
	off := everySettingOff(t)
	zero, beyond := 0, 1<<30
	p, err := checkSource(t, "@start\nrule main = !\"\"* B / \"x\"\nrule B = \"b\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range bisectTargets() {
		with, log := bisectCompile(t, p, off, &beyond, tg)
		for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
			if f := strings.Fields(line); len(f) < 4 || f[2] != "run" || !strings.HasPrefix(f[3], "canonicalize#") {
				t.Fatalf("%s: with every setting off, a decision that is not a canonicalize pattern was asked: %q", tg.Backend, line)
			}
		}
		if !strings.Contains(log, "canonicalize#2/drop-after-exit") {
			t.Fatalf("%s: drop-after-exit applied nowhere at the Exec stage, so the test is empty:\n%s", tg.Backend, log)
		}
		if without, _ := bisectCompile(t, p, off, &zero, tg); without != with {
			t.Errorf("%s: the parser without the canonicalize patterns differs from the one with them:\n%s", tg.Backend, firstDiff(without, with))
		}
	}
}

// The bisection mode differs from a build without a limit only in the rules that the code never
// enters and in what they use (the features of the library, helpers and literal tables): the Go
// parser keeps every rule (common.GenOptions.KeepUnreachable), and the TypeScript parser keeps the
// rules of the plan's reach without asking for the reach of its printed code
// (tsprint.Hooks.KeepPlanReach). So the parser at a limit beyond the last decision is the one built
// without a limit in that way; and that one differs from the default for these grammars, which have
// rules that the code does not enter, so the comparison is not empty. That the TypeScript parser so
// built behaves as the default one is checked by running both (tsprint: TestBisectionModeParserBehaves).
func TestBisectionModeKeepsOnlyUnenteredRules(t *testing.T) {
	t.Parallel()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	beyond := 1 << 30
	for i, src := range bisectGrammars {
		p, err := checkSource(t, src, common.CompileOptions{}, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range bisectTargets() {
			limited, _ := bisectCompile(t, p, speed, &beyond, tg)
			def, _ := bisectCompile(t, p, speed, nil, tg)
			keep := tg
			if tg.Backend == result.Go {
				keep.Gen.KeepUnreachable = true
			} else {
				keep.TSHooks = &tsprint.Hooks{KeepPlanReach: true}
			}
			kept, _ := bisectCompile(t, p, speed, nil, keep)
			if limited != kept {
				t.Errorf("grammar %d, %s: the parser at a limit beyond the last decision is not the one that keeps the unentered rules:\n%s", i, tg.Backend, firstDiff(limited, kept))
			}
			// The first grammar has a rule that no entry point reaches, which the Go parser keeps; the
			// second one a rule that only an unprinted call reaches, which the TypeScript parser keeps.
			if (i == 0) == (tg.Backend == result.Go) && kept == def {
				t.Errorf("grammar %d, %s: keeping the unentered rules does not change the parser", i, tg.Backend)
			}
		}
	}
}

// firstDiff returns the lines around the first line where a and b differ.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return "line " + strconv.Itoa(i+1) + ":\n- " + x + "\n+ " + y
		}
	}
	return "(equal)"
}
