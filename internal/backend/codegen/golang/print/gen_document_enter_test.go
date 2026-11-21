package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// documentEnterSrc has an empty match at the start of the input: pad matches "" at 0 and examines
// nothing, so its examined range is empty and an insertion at 0 leaves it valid.
const documentEnterSrc = "rule main = pad \"a\"+\nrule pad = \"\""

// documentEnterForm is one generated Go Document: untyped or typed, with the library imported
// from pkg/runtime/libgo or embedded in the generated package.
type documentEnterForm struct {
	pkg   string
	typed bool
	lib   common.LibMode
}

var documentEnterForms = []documentEnterForm{
	{"untypedimport", false, common.LibImport},
	{"typedimport", true, common.LibImport},
	{"untypedembed", false, common.LibEmbed},
	{"typedembed", true, common.LibEmbed},
}

// documentEnterProgram parses "aa" with a Document, inserts "a" at 0, parses again and prints the
// exits of both parses, the reuse count of the second and its result.
const documentEnterProgram = `package %[1]s

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

func TestEnter(t *testing.T) {
	trace := func(parse string) ParseOption {
		return WithTrace(func(e instrument.TraceEvent) {
			if e.Kind == instrument.TraceExit {
				fmt.Printf("%[1]s EXIT %%s %%s %%d %%d %%d %%v\n", parse, e.Rule, e.Pos, e.End, e.Examined, e.Matched)
			}
		})
	}
	d, err := NewDocument("aa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.%[2]s(trace("before")); err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(0, 0, "a"); err != nil {
		t.Fatal(err)
	}
	n, err := d.%[2]s(trace("after"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%[1]s REUSED %%d\n", d.Stats().Reused)
	if %[3]v {
		fmt.Printf("%[1]s NODE %%v\n", n)
	}
}
`

// documentEnterRun is what one generated Document printed.
type documentEnterRun struct {
	exits  map[string][]examinedref.Exit // by parse: "before" and "after"
	reused int
	node   string
}

// TestDocumentEmptyEnterReuse checks that a generated Go Document starts the examined range of a
// call empty (spec/runtime/documents.md, Reuse Model), as the engine does. Every exit of the two
// parses has the examined end of the independent reference (examinedref), so the empty match pad
// examines nothing; the edit at its start therefore keeps its result, and the Document reuses as
// many results as the engine and returns the same tree. It covers the untyped and the typed
// Document, with the library imported (pkg/runtime/libgo) and embedded in the generated package.
// A mutant library that starts the range one unit past the call must fail the same checks in
// every form.
func TestDocumentEmptyEnterReuse(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	g, err := enginetest.ParseGrammar(documentEnterSrc)
	if err != nil {
		t.Fatal(err)
	}
	p, err := enginetest.Compile(starttest.MarkAll(g), "main")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.NewDocument("aa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit(0, 0, "a"); err != nil {
		t.Fatal(err)
	}
	engineNode, err := doc.Parse()
	if err != nil {
		t.Fatal(err)
	}
	engineReused := int(doc.Stats().Reused)

	ref := map[string][]examinedref.Exit{}
	for parse, in := range map[string]string{"before": "aa", "after": "aaa"} {
		if _, ref[parse], err = examinedref.Evaluate(g, "main", in); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.ContainsFunc(ref["before"], func(x examinedref.Exit) bool {
		return x.Rule == "pad" && x.Pos == 0 && x.Matched && x.Examined == 0
	}) {
		t.Fatalf("the reference does not give pad at 0 an empty examined range: %v", ref["before"])
	}

	pg, err := parseTest(documentEnterSrc)
	if err != nil {
		t.Fatal(err)
	}
	check := func(form documentEnterForm, run documentEnterRun) []string {
		var diffs []string
		for _, parse := range []string{"before", "after"} {
			diffs = append(diffs, documentEnterExitDiffs(parse, run.exits[parse], ref[parse])...)
		}
		if run.reused != engineReused {
			diffs = append(diffs, fmt.Sprintf("reused %d; the engine reused %d", run.reused, engineReused))
		}
		if !form.typed && run.node != engineNode.String() {
			diffs = append(diffs, fmt.Sprintf("result %s; the engine returned %v", run.node, engineNode))
		}
		return diffs
	}

	runs := runDocumentEnter(t, pg, nil)
	for _, form := range documentEnterForms {
		for _, d := range check(form, runs[form.pkg]) {
			t.Errorf("%s: %s", form.pkg, d)
		}
	}

	// The negative control: a library that starts the examined range one unit past the call.
	mutants := runDocumentEnter(t, pg, oneUnitEnter)
	for _, form := range documentEnterForms {
		diffs := check(form, mutants[form.pkg])
		if len(diffs) == 0 {
			t.Errorf("%s: the Document of a library that starts the examined range at one unit passes the checks", form.pkg)
		}
		t.Logf("%s with a one-unit start: %s", form.pkg, strings.Join(diffs, "; "))
	}
}

// oneUnitEnter patches the library so that a Document call starts its examined range one unit past
// its start, as generated Go once did. It returns the number of replacements it made.
func oneUnitEnter(src string) (string, int) {
	n := 0
	for _, start := range []string{"p.Pos", "start"} {
		old := "p.docExamined, p.docLow = " + start + ", " + start
		n += strings.Count(src, old)
		src = strings.ReplaceAll(src, old, "p.docExamined, p.docLow = "+start+"+1, "+start)
	}
	return src, n
}

// runDocumentEnter generates every form of documentEnterSrc into one module, runs their programs,
// and returns what each printed. With patch, the library of every form is patched with it: the
// copy that the imported forms build against and the copy that the embedded forms hold; the
// patch must apply to both.
func runDocumentEnter(t *testing.T, g *ast.Grammar, patch func(string) (string, int)) map[string]documentEnterRun {
	t.Helper()
	const root = "example.com/enter"
	dir := t.TempDir()
	lib := codegentest.LibCopy()
	if patch != nil {
		patched := 0
		lib = codegentest.LibCopyWith(t, func(_, src string) string {
			src, n := patch(src)
			patched += n
			return src
		}, nil)
		if patched == 0 {
			t.Fatal("the patch does not apply to pkg/runtime/libgo")
		}
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "go.mod"), []byte(codegentest.GoModAt(root, lib)))
	for _, form := range documentEnterForms {
		files, err := codegentest.Generate(t, g, common.GenOptions{
			Package: form.pkg, Module: root + "/" + form.pkg, Start: "main", Lib: form.lib,
			Document: true, Observability: true, Types: form.typed,
			LimitControls: codegentest.FixtureBudgetControls(t),
		})
		if err != nil {
			t.Fatal(err)
		}
		patched := 0
		for _, f := range files {
			data := f.Content
			if patch != nil && form.lib == common.LibEmbed && strings.HasPrefix(f.Path, "internal/libgo/") {
				src, n := patch(string(data))
				data, patched = []byte(src), patched+n
			}
			write(filepath.Join(dir, form.pkg, filepath.FromSlash(f.Path)), data)
		}
		if patch != nil && form.lib == common.LibEmbed && patched == 0 {
			t.Fatalf("%s: the patch does not apply to the embedded library", form.pkg)
		}
		parse := "Parse"
		if form.typed {
			parse = "ParseValue"
		}
		write(filepath.Join(dir, form.pkg, "enter_test.go"), fmt.Appendf(nil, documentEnterProgram, form.pkg, parse, !form.typed))
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "TestEnter", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", codegentest.GoFlagsEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Documents: %v\n%s", err, out)
	}
	runs := map[string]documentEnterRun{}
	line := regexp.MustCompile(`(?m)^(\w+) (EXIT (\w+) (\w+) (\d+) (\d+) (\d+) (true|false)|REUSED (\d+)|NODE (.*))$`)
	for _, m := range line.FindAllStringSubmatch(string(out), -1) {
		run := runs[m[1]]
		if run.exits == nil {
			run.exits = map[string][]examinedref.Exit{}
		}
		atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
		switch {
		case m[3] != "":
			run.exits[m[3]] = append(run.exits[m[3]], examinedref.Exit{Rule: m[4], Pos: atoi(m[5]), End: atoi(m[6]), Examined: atoi(m[7]), Matched: m[8] == "true"})
		case m[9] != "":
			run.reused = atoi(m[9])
		default:
			run.node = m[10]
		}
		runs[m[1]] = run
	}
	for _, form := range documentEnterForms {
		if len(runs[form.pkg].exits["after"]) == 0 {
			t.Fatalf("%s printed no exits of the second parse:\n%s", form.pkg, out)
		}
	}
	return runs
}

// documentEnterExitDiffs compares the exits of a parse of a generated Document with those of the
// reference: every exit must be one that the reference makes, with its examined end.
func documentEnterExitDiffs(parse string, got, ref []examinedref.Exit) []string {
	var diffs []string
	for _, x := range got {
		i := slices.IndexFunc(ref, func(r examinedref.Exit) bool {
			return r.Rule == x.Rule && r.Pos == x.Pos && r.End == x.End && r.Matched == x.Matched
		})
		switch {
		case i < 0:
			diffs = append(diffs, fmt.Sprintf("%s: the reference makes no exit %s@%d (end %d, matched %v)", parse, x.Rule, x.Pos, x.End, x.Matched))
		case ref[i].Examined != x.Examined:
			diffs = append(diffs, fmt.Sprintf("%s: examined end of %s@%d is %d; the reference gives %d", parse, x.Rule, x.Pos, x.Examined, ref[i].Examined))
		}
	}
	return diffs
}
