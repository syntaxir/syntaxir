package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// optimizeGrammar has a choice whose alternatives start with different characters, so the guards
// pass changes the generated parser.
const optimizeGrammar = "@start\nrule main = (word / num / -\" \")* $$\nrule word = \"if\" / \"else\" / [a-z]+\nrule num = [0-9]+\n"

// TestGenOptimize checks the optimization options of sxir gen: speed is the default, an optimization that
// is turned off changes the parser, and every unknown name is a usage error that names the valid ones.
func TestGenOptimize(t *testing.T) {
	g := writeFile(t, "opt.sxir", optimizeGrammar)
	def, err := runGenGo(t, "-g", g, "-types", "-typed-only", "-lib", "import")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := runGenGo(t, "-g", g, "-types", "-typed-only", "-lib", "import", "-optimize", "speed")
	if err != nil || speed != def {
		t.Fatalf("-optimize speed differs from the default: %v", err)
	}
	spaced, err := runGenGo(t, "-g", g, "-types", "-typed-only", "-lib", "import", "-no-opt", "guards, scans")
	if err != nil {
		t.Fatalf("-no-opt with spaces after the comma: %v", err)
	}
	if plain, err := runGenGo(t, "-g", g, "-types", "-typed-only", "-lib", "import", "-no-opt", "guards,scans"); err != nil || plain != spaced {
		t.Errorf("-no-opt \"guards, scans\" differs from \"guards,scans\": %v", err)
	}
	for _, name := range []string{"guards", "scans", "class-runs", "guards,scans"} {
		without, err := runGenGo(t, "-g", g, "-types", "-typed-only", "-lib", "import", "-no-opt", name)
		if err != nil {
			t.Fatalf("-no-opt %s: %v", name, err)
		}
		if name == "guards" && without == def {
			t.Errorf("-no-opt guards generated the same parser")
		}
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-optimize", "nosuch"}, `-optimize: unknown optimization profile "nosuch" (valid: speed, memory, size)`},
		{[]string{"-no-opt", "nosuch"}, `-no-opt: unknown optimization "nosuch" (valid: dispatch, guards, outline, scans, field-compare,`},
		{[]string{"-no-opt", "guards,nosuch"}, `unknown optimization "nosuch"`},
		{[]string{"-print-after", "nosuch"}, `-print-after: unknown pass or step "nosuch" to print after (valid: elaborate, guards, scans, actionlower, text-dispatch, field-compare, go-struct-call, go-in-place, action-finish, dispatch, outline, plan, flowlower, terminals,`},
	} {
		_, err := runGenGo(t, append([]string{"-g", g}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want an error with %q", c.args, err, c.want)
		}
	}
	// The generation options do not apply to the library alone.
	for _, name := range []string{"-optimize", "-no-opt", "-print-after"} {
		_, err := runCLI(t, "", "gen", "-lib", "only", "-o", t.TempDir(), name, "speed")
		if err == nil || !strings.Contains(err.Error(), "cannot be combined with -lib only") {
			t.Errorf("%s with -lib only: %v", name, err)
		}
	}
}

// TestGenPrintAfter checks that -print-after writes the Core IR after the named passes, and only
// those, to the diagnostic stream, once for each Core module of the generation, and the Flow IR of
// each function printed from it after flowlower.
func TestGenPrintAfter(t *testing.T) {
	g := writeFile(t, "opt.sxir", optimizeGrammar)
	// The passes that the lean form lets run are printed with -no-limits. A parser with limit
	// controls, the default, is the same form with a counter of compute units; a
	// Document records the input it examines, which the lean passes do not preserve.
	gen := func(list string, args ...string) string {
		t.Helper()
		var out strings.Builder
		old := genDiagnostics
		genDiagnostics = &out
		defer func() { genDiagnostics = old }()
		if _, err := runGenGo(t, append([]string{"-g", g, "-types", "-lib", "import", "-print-after", list}, args...)...); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	run := func(list string, extra ...string) string {
		t.Helper()
		return gen(list, append([]string{"-typed-only"}, extra...)...)
	}
	sections := func(out string) []string {
		var names []string
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "-- after ") {
				names = append(names, line)
			}
		}
		return names
	}
	all := run("all", "-no-limits")
	for _, want := range []string{"-- after elaborate [typed] --", "-- after guards [typed] --", "-- after scans [typed] --", "-- after class-runs [typed/"} {
		if !strings.Contains(all, want) {
			t.Errorf("-print-after all lacks %q", want)
		}
	}
	if got, want := sections(run("all")), sections(all); !slices.Equal(got, want) {
		t.Errorf("-print-after all by default prints the sections %q, the lean form %q", got, want)
	}
	// A Document reads the input in ways the lean passes do not preserve, so they do not run.
	document := gen("all", "-document")
	if !strings.Contains(document, "-- after scans [") || strings.Contains(document, "-- after terminals [") || strings.Contains(document, "-- after class-runs [") {
		t.Errorf("-print-after all for a Document prints the passes that apply to a lean parser")
	}
	// A pass that does not apply prints nothing, however it is asked for.
	if got := gen("terminals", "-document"); got != "" {
		t.Errorf("-print-after terminals for a Document printed:\n%.300s", got)
	}
	guards := run("guards", "-no-limits")
	if strings.Count(guards, "-- after ") != strings.Count(all, "-- after guards [") || !strings.Contains(guards, "-- after guards [") {
		t.Errorf("-print-after guards prints %d sections:\n%.300s", strings.Count(guards, "-- after "), guards)
	}
	if got := run("elaborate,scans", "-no-limits"); !strings.Contains(got, "-- after elaborate [") || !strings.Contains(got, "-- after scans [") || strings.Contains(got, "-- after guards [") || strings.Contains(got, "\nfunc ") {
		t.Errorf("-print-after elaborate,scans prints the wrong sections")
	}
	// flowlower prints the module of each table that the parser is printed from, as the lowering
	// made it, one function after another, and no Core IR.
	flowIR := run("flowlower", "-no-limits")
	if !strings.Contains(flowIR, "-- after flowlower [typed] --\nfunc ") || strings.Count(flowIR, "-- after ") != strings.Count(flowIR, "-- after flowlower [") {
		t.Errorf("-print-after flowlower prints the wrong sections:\n%.300s", flowIR)
	}
	if n := strings.Count(all, "-- after flowlower ["); n == 0 || n != strings.Count(flowIR, "-- after ") {
		t.Errorf("-print-after all prints %d lowered Flow modules, flowlower %d", n, strings.Count(flowIR, "-- after "))
	}
	// execlower prints the module of each table printed from Exec IR, as lowered from Flow IR, and
	// nothing else; it holds the functions of the Flow module.
	execIR := run("execlower", "-no-limits")
	if !strings.Contains(execIR, "-- after execlower [typed] --\n") || strings.Count(execIR, "-- after ") != strings.Count(execIR, "-- after execlower [") {
		t.Errorf("-print-after execlower prints the wrong sections:\n%.300s", execIR)
	}
	if n := strings.Count(all, "-- after execlower ["); n == 0 || n != strings.Count(execIR, "-- after ") || strings.Count(execIR, "\nfunc ") != strings.Count(flowIR, "\nfunc ") {
		t.Errorf("-print-after all prints %d lowered Exec modules, execlower %d", n, strings.Count(execIR, "-- after "))
	}
}

// TestGenOpt checks -opt: it gives a value to an optimization after the profile and before -no-opt,
// and a name that does not exist, a count that is not a number and a value the optimization does
// not take are usage errors.
func TestGenOpt(t *testing.T) {
	g := writeFile(t, "opt.sxir", optimizeGrammar)
	base := []string{"-g", g, "-types", "-typed-only", "-lib", "import"}
	gen := func(extra ...string) string {
		t.Helper()
		out, err := runGenGo(t, append(append([]string{}, base...), extra...)...)
		if err != nil {
			t.Fatalf("%v: %v", extra, err)
		}
		return out
	}
	def := gen()
	// A value that is the default changes nothing; a different one changes the parser.
	if got := gen("-opt", "class-tables=0, inline-literals=4"); got != def {
		t.Errorf("-opt with the default values changed the parser")
	}
	off := gen("-opt", "inline-literals=0")
	if off == def {
		t.Errorf("-opt inline-literals=0 generated the same parser")
	}
	// -no-opt is applied after -opt and wins.
	if got := gen("-opt", "inline-literals=2", "-no-opt", "inline-literals"); got != off {
		t.Errorf("-no-opt did not win over -opt")
	}
	// A name alone turns an optimization on; the value 0 turns it off, as -no-opt does.
	if got := gen("-opt", "guards=0"); got != gen("-no-opt", "guards") {
		t.Errorf("-opt guards=0 differs from -no-opt guards")
	}
	if got := gen("-opt", "guards"); got != def {
		t.Errorf("-opt guards changed an optimization that is on")
	}

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-opt", "nosuch"}, `-opt: unknown optimization "nosuch" (valid: dispatch, guards, outline, scans, field-compare,`},
		{[]string{"-opt", "guards,nosuch=2"}, `-opt: unknown optimization "nosuch"`},
		{[]string{"-opt", "class-tables=x"}, `-opt:`},
		{[]string{"-opt", "guards=2"}, `-opt: optimization guards does not take the value 2`},
		{[]string{"-opt", "class-tables=-1"}, `-opt: optimization class-tables does not take the value -1`},
		{[]string{"-opt", "guards,guards"}, `-opt:`},
		{[]string{"-opt", "guards,,scans"}, `-opt: empty optimization name`},
	} {
		_, err := runGenGo(t, append([]string{"-g", g}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want an error with %q", c.args, err, c.want)
		}
	}
	_, err := runCLI(t, "", "gen", "-lib", "only", "-o", t.TempDir(), "-opt", "guards")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with -lib only") {
		t.Errorf("-opt with -lib only: %v", err)
	}
}

// TestGenOptTypeScript checks that -opt reaches the TypeScript generator.
func TestGenOptTypeScript(t *testing.T) {
	g := writeFile(t, "lit.sxir", "@start\nrule main = (\"alpha\" / \"beta\" / \"gamma\" / \"delta\" / \"alpha\" \"beta\" / \"gamma\" \"delta\")+ $$\n")
	read := func(extra ...string) string {
		t.Helper()
		dir, err := runGenTS(t, append([]string{"-g", g}, extra...)...)
		if err != nil {
			t.Fatalf("%v: %v", extra, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return readTS(t, dir, names...)
	}
	def := read()
	shared := read("-opt", "ts-literal-sharing")
	if shared == def {
		t.Errorf("-opt ts-literal-sharing generated the same TypeScript parser")
	}
	if got := read("-opt", "ts-literal-sharing=0"); got != def {
		t.Errorf("-opt ts-literal-sharing=0 differs from the default")
	}
	if got := read("-opt", "ts-literal-sharing", "-no-opt", "ts-literal-sharing"); got != def {
		t.Errorf("-no-opt did not win over -opt")
	}
}
