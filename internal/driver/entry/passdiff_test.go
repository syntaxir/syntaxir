package entry_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// The engine runs a subset of the registered optimizations: the passes that its backends run (their
// pass statistics list them). A pass that is on or off must give the same trees, the same errors and
// the same exact compute units (spec/runtime/parsing.md, Cost) on every backend that runs it, so that no
// pass changes what a parse means or what it costs. TestPassesOffAgree switches each engine pass off on its
// own over the generation corpus and compares the engine with the pass off against the engine with the default
// settings, backend by backend. The optimizations that no engine backend runs are checked in generated code,
// in the tests that generatedOnlyFlags names.

var (
	allEngineBackends = []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative}
	bytecodeBackends  = []engine.Backend{engine.Bytecode, engine.BytecodeIterative}
)

// engineFlags lists the optimizations that the engine runs, and the engine backends that run each.
// listsFlag reports whether a pipeline text lists the pass or pattern of a flag: as a step, or inside the
// brackets of the canonicalize pass.
func listsFlag(text, name string) bool {
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune(" \t\n,[]#0123456789", r) }) {
		if w == name {
			return true
		}
	}
	return false
}

// TestDescribeListsWhatRuns checks that the pipeline text of each run-time backend lists exactly the passes
// and patterns that the backend runs (that its pass statistics list), for every registered optimization.
func TestDescribeListsWhatRuns(t *testing.T) {
	ran := ranPasses(t)
	g, err := enginetest.Parse("rule main = \"a\"\n")
	if err != nil {
		t.Fatal(err)
	}
	prog, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		backend result.Backend
		engine  engine.Backend
	}{{result.VM, engine.Bytecode}, {result.Closure, engine.Closure}} {
		text, err := checked.Describe(entry.Source(prog), common.Debug{}, c.backend)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range pass.Names() {
			if got, want := listsFlag(text, name), ran[c.engine][name]; got != want {
				t.Errorf("%s pipeline text lists %s = %v, the backend runs it = %v", c.backend, name, got, want)
			}
		}
	}
}

// TestPassFlagClasses checks the lists against the passes that the backends run.
var engineFlags = []struct {
	name     string
	backends []engine.Backend
}{
	{"guards", allEngineBackends},
	{"scans", allEngineBackends},
	{"dispatch", bytecodeBackends},
	{"canonicalize", bytecodeBackends},
	{"drop-after-exit", bytecodeBackends},
}

// generatedOnlyFlags says for each optimization that no engine backend runs the test that checks a
// parser generated without it against one generated with it (or against the engine), as a test
// function and the directory of the package that holds it. The Flow passes terminals, guarded-classes,
// class-runs, class-tables and inline-literals are among them, since the VM's flow target
// (flowpass.Bytecode) prints none of their forms, and so are the canonicalize patterns of the Exec stage
// (fold-constants, fold-branches, drop-unused-labels, splice-blocks), which the VM never reaches: its Flow
// stage has only drop-after-exit. The pipeline text lists exactly the passes that a backend runs
// (TestDescribeListsWhatRuns).
var generatedOnlyFlags = map[string]struct{ test, dir string }{
	"fold-constants":         {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"fold-branches":          {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"drop-unused-labels":     {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"splice-blocks":          {"TestCanonicalizePatternOff", "internal/middleend/codepass"},
	"terminals":              {"TestTerminalProgramDisabledParity", "internal/backend/codegen/golang/print"},
	"guarded-classes":        {"TestGuardedClassDisabledParity", "internal/backend/codegen/golang/print"},
	"class-runs":             {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"class-tables":           {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"inline-literals":        {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
	"text-dispatch":          {"TestLiteralTextPredicateDispatchDisabledParity", "internal/backend/codegen/golang/print"},
	"field-compare":          {"TestGeneratedTextFieldComparisons", "internal/backend/codegen/golang/print"},
	"outline":                {"TestOptimizationDisabledTSParity", "internal/backend/codegen/typescript/print"},
	"method-sharing":         {"TestGeneratedUnsharedParsersMatchEngine", "internal/backend/codegen/golang/print"},
	"literal-sharing":        {"TestGeneratedUnsharedParsersMatchEngine", "internal/backend/codegen/golang/print"},
	"ts-literal-sharing":     {"TestGeneratedSharingIdentity", "internal/backend/codegen/common"},
	"typed-object-slabs":     {"TestGeneratedTypedOutputRetentionFixture", "internal/backend/codegen/golang/print"},
	"typed-list-slabs":       {"TestGeneratedTypedOutputRetentionFixture", "internal/backend/codegen/golang/print"},
	"typed-output-bootstrap": {"TestGeneratedTypedOutputRetentionFixture", "internal/backend/codegen/golang/print"},
	"sparse-memo":            {"TestGeneratedTSSparseMemoControls", "internal/backend/codegen/typescript/print"},
	"ts-merged-records":      {"TestTSExecPassesKeepResultsAndSpends", "internal/backend/codegen/typescript/print"},
	"ts-tested-inits":        {"TestTSExecPassesKeepResultsAndSpends", "internal/backend/codegen/typescript/print"},
	"used-features":          {"TestOptimizationDisabledParity", "internal/backend/codegen/golang/print"},
}

// ranPasses compiles a grammar with the default settings, parses with each engine backend, and returns
// for each the names that its pass statistics list: the passes that ran, and the patterns that the
// canonicalize pass reports.
func ranPasses(t *testing.T) map[engine.Backend]map[string]bool {
	t.Helper()
	g, err := enginetest.Parse("rule main = \"a\"+ [b-c]* ([d] / \"e\") $$\n")
	if err != nil {
		t.Fatal(err)
	}
	out := map[engine.Backend]map[string]bool{}
	for _, b := range allEngineBackends {
		// Each backend gets a program of its own, so the statistics are its own: the closure backend's pipeline
		// runs only the passes of the closure build, the bytecode backends' that of the VM.
		var buf strings.Builder
		prog, err := entry.Compile(g, entry.Options{Debug: common.Debug{Stats: true, Output: &buf}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := prog.ParseWith("main", "aabd", engine.ParseOptions{Backend: b}); err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		prefix := "vm/"
		if b == engine.Closure {
			prefix = "closure/"
		}
		for _, w := range strings.Fields(buf.String()) {
			if rest, ok := strings.CutPrefix(w, prefix); ok {
				names[rest] = true
			}
		}
		// The patterns are listed as "<name> applied <n>" after the pass that applies them.
		if b != engine.Closure {
			f := strings.Fields(buf.String())
			for i, w := range f {
				if w == "applied" && i > 0 {
					names[f[i-1]] = true
				}
			}
		}
		out[b] = names
	}
	return out
}

// TestPassFlagClasses checks that each registered optimization is in exactly one class: an engine
// optimization that the differential test switches off, or one that no engine backend runs, which names the
// test of its disabled form in generated code; that the engine class is what the backends run (by their pass
// statistics), each on the backends that the class names and on no other; and that the named tests exist. A
// pass that a backend starts to run, or stops running, fails it until its class is changed.
func TestPassFlagClasses(t *testing.T) {
	ran := ranPasses(t)
	engineNames := map[string]bool{}
	for _, f := range engineFlags {
		engineNames[f.name] = true
		if _, ok := pass.Lookup(f.name); !ok {
			t.Errorf("engine flag %s is not registered", f.name)
		}
		for _, b := range allEngineBackends {
			if got, want := ran[b][f.name], slices.Contains(f.backends, b); got != want {
				t.Errorf("flag %s: the backend %v runs it = %v, the class says %v", f.name, b, got, want)
			}
		}
	}
	for _, name := range pass.Names() {
		g, gen := generatedOnlyFlags[name]
		switch {
		case engineNames[name] && gen:
			t.Errorf("flag %s is in both classes", name)
		case engineNames[name]:
		case !gen:
			t.Errorf("flag %s is in neither class: the engine differential does not switch it off and no generated-code test is named", name)
		default:
			for b, names := range ran {
				if names[name] {
					t.Errorf("flag %s is classed as run by no engine backend, but %v runs it", name, b)
				}
			}
			if !hasTestFunc(t, g.dir, g.test) {
				t.Errorf("flag %s names %s in %s, which does not exist", name, g.test, g.dir)
			}
		}
	}
	for name := range generatedOnlyFlags {
		if _, ok := pass.Lookup(name); !ok {
			t.Errorf("generated-only flag %s is not registered", name)
		}
	}
}

// hasTestFunc reports whether a *_test.go file of dir (relative to the module root) declares the
// function name.
func hasTestFunc(t *testing.T, dir, name string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", dir, "*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test files in %s", dir)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "func "+name+"(") {
			return true
		}
	}
	return false
}

// parseOutcome is what a parse gives: the tree or the error as text, and the compute units.
type parseOutcome struct {
	tree, err string
	units     uint64
}

func (o parseOutcome) String() string {
	return fmt.Sprintf("tree %q err %q units %d", o.tree, o.err, o.units)
}

var units2 = []input.PositionUnit{input.CodePoints, input.Bytes}

// outcomes parses every input of c in both position units on each backend and returns the results in
// the order of backends, inputs and units.
func outcomes(prog *engine.Program, c codegentest.Case, backends []engine.Backend) []parseOutcome {
	var out []parseOutcome
	for _, b := range backends {
		for _, in := range c.Inputs {
			for _, unit := range units2 {
				var o parseOutcome
				n, err := prog.ParseWith("main", in, engine.ParseOptions{Backend: b, Unit: unit, Expectations: true, ComputeReport: &o.units})
				if n != nil {
					o.tree = n.String()
				}
				if err != nil {
					o.err = err.Error()
				}
				out = append(out, o)
			}
		}
	}
	return out
}

// passVariant is one setting of the optimizations that differs from the default, and the backends that run
// what it changes.
type passVariant struct {
	name     string
	settings func(pass.Settings) (pass.Settings, error)
	backends []engine.Backend
}

func passVariants() []passVariant {
	var vs []passVariant
	for _, f := range engineFlags {
		vs = append(vs, passVariant{f.name + " off", func(s pass.Settings) (pass.Settings, error) { return s.Without(f.name) }, f.backends})
	}
	// Every registered optimization off at once, including those that no backend runs.
	vs = append(vs, passVariant{"all optimizations off", func(s pass.Settings) (pass.Settings, error) { return s.Without(pass.Names()...) }, allEngineBackends})
	return vs
}

// TestPassesOffAgree switches each optimization that the engine runs off on its own (and all of them off at
// once) and checks that the engine gives the same trees, errors and compute units, on each backend that runs
// it, as with the default settings.
//
// The default tier runs the representative cases of the corpus (codegentest.Representative), the
// long tier (go test -tags sxir_long) all of them.
func TestPassesOffAgree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compiles the corpus once per setting")
	}
	codegentest.NeedsBudgetControls(t)
	cases := codegentest.Representative(codegentest.GenCorpus(t))
	variants := passVariants()
	for _, c := range cases {
		g, err := enginetest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		compile := func(s pass.Settings) *engine.Program {
			prog, err := entry.Compile(g, entry.Options{Compile: check.Options{NoProjections: true}, Settings: s})
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			return prog
		}
		base := compile(pass.Settings{})
		for _, v := range variants {
			s, err := v.settings(pass.Settings{})
			if err != nil {
				t.Fatalf("%s: %v", v.name, err)
			}
			want, got := outcomes(base, c, v.backends), outcomes(compile(s), c, v.backends)
			per := len(c.Inputs) * len(units2)
			for i := range want {
				if want[i] != got[i] {
					t.Errorf("%s: %s: backend %v, input %.40q, %v:\n  default: %v\n  off:     %v", c.Name, v.name, v.backends[i/per], c.Inputs[i%per/len(units2)], units2[i%len(units2)], want[i], got[i])
				}
			}
		}
	}
}

var appliedRe = regexp.MustCompile(`([a-z-]+) applied (\d+)`)
var scanRe = regexp.MustCompile(`(scanReachAt|scanEndAt|scanClass) (\d+)`)
var guardedRe = regexp.MustCompile(`(?m)^\s*vm/(guards|dispatch)\s.*guarded (\d+)`)

// TestEnginePassesApply checks that each optimization that the engine runs does something on the corpus of the
// tier: the guards and the dispatch guard some alternative, the scans make the VM lower a scan instruction,
// and each canonicalize pattern of the VM applies at least once. Without it a pass could be switched off in TestPassesOffAgree and differ from nothing.
func TestEnginePassesApply(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compiles the corpus")
	}
	codegentest.NeedsBudgetControls(t)
	applied := map[string]int{}
	for _, c := range codegentest.Representative(codegentest.GenCorpus(t)) {
		g, err := enginetest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		var buf strings.Builder
		prog, err := entry.Compile(g, entry.Options{Compile: check.Options{NoProjections: true}, Debug: common.Debug{Stats: true, Output: &buf}})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if _, err := prog.ParseWith("main", c.Inputs[0], engine.ParseOptions{Backend: engine.Bytecode}); err != nil {
			_ = err // an input that is rejected still builds the program
		}
		for _, m := range appliedRe.FindAllStringSubmatch(buf.String(), -1) {
			var n int
			fmt.Sscan(m[2], &n)
			applied[m[1]] += n
		}
		for _, m := range guardedRe.FindAllStringSubmatch(buf.String(), -1) {
			var n int
			fmt.Sscan(m[2], &n)
			applied[m[1]+" guarded"] += n
		}
		// The scans pass shows in what the VM lowers: a scan instruction.
		for _, m := range scanRe.FindAllStringSubmatch(buf.String(), -1) {
			var n int
			fmt.Sscan(m[2], &n)
			applied["scans lowered"] += n
		}
	}
	for _, name := range []string{"guards guarded", "dispatch guarded", "scans lowered"} {
		if applied[name] == 0 {
			t.Errorf("%s is 0 over the corpus: the pass does nothing there, so switching it off proves nothing", name)
		}
	}
	// Every pass of the VM's pipeline applies at least once over the corpus: the patterns by their counts.
	for _, f := range engineFlags {
		if f.name == "drop-after-exit" && applied[f.name] == 0 {
			t.Errorf("the pattern %s never applies over the corpus", f.name)
		}
	}
}
