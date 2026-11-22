//go:build sxir_long

package examinedref_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// referenceMutant breaks one rule of the reference or of its comparison: in file, it replaces the
// only copy of old with new. The test run must then fail: run names the test that must notice it.
type referenceMutant struct {
	name, file, old, new, run string
}

// The rules of the reference fail TestExaminedOracle on the shapes, where the engine disagrees with the
// broken rule; the rules of the comparison fail its own tests, which give it reported calls it must
// reject.
var referenceMutants = []referenceMutant{
	{name: "a matched character of a literal examines nothing", file: "evaluate.go", run: "oracle",
		old: "\t\th = maxInt(h, ch)\n\t\tif !ok {\n\t\t\treturn failed(st, h, false)",
		new: "\t\tif !ok {\n\t\t\th = maxInt(h, ch)\n\t\t\treturn failed(st, h, false)"},
	{name: "the end of the input examines no slot", file: "evaluate.go", run: "oracle",
		old: "\tcase *ast.EndInput:\n\t\th := examine(pos, pos)", new: "\tcase *ast.EndInput:\n\t\th := pos"},
	{name: "a positive lookahead forgets what it examined", file: "evaluate.go", run: "oracle",
		old: "return outcome{matched: true, end: pos, h: o.h, vars: o.vars", new: "return outcome{matched: true, end: pos, h: pos, vars: o.vars"},
	{name: "reading a growth examines nothing", file: "evaluate.go", run: "oracle",
		old: "o := outcome{matched: g.matched, end: pos, h: g.examined,", new: "o := outcome{matched: g.matched, end: pos, h: pos,"},
	{name: "a growth stops after its first step", file: "evaluate.go", run: "oracle",
		old: "\t\tif !g.reentered {\n", new: "\t\tif true {\n"},
	{name: "a choice ignores a cut", file: "evaluate.go", run: "oracle",
		old: "\t\t\tif o.cut {\n\t\t\t\tbreak\n\t\t\t}\n\t\t}\n\t\treturn failed(st, h, false)",
		new: "\t\t}\n\t\treturn failed(st, h, false)"},
	{name: "a recovery forgets what its skip examined", file: "evaluate.go", run: "oracle",
		old: "h := maxInt(o.h, s.h)", new: "h := o.h"},
	{name: "a token is inserted inside a lookahead", file: "evaluate.go", run: "oracle",
		old: "!ev.opts.MissingTokens || st.look ||", new: "!ev.opts.MissingTokens ||"},
	{name: "decoding a byte examines only that byte", file: "input.go", run: "oracle",
		old: "\t\t\tif c < lo || c > hi {\n\t\t\t\treturn utf8.RuneError, pos + 1, i + 1, true",
		new: "\t\t\tif c < lo || c > hi {\n\t\t\t\treturn utf8.RuneError, pos + 1, pos + 1, true"},
	{name: "the first operator part that matches is selected", file: "operators.go", run: "oracle",
		old: "if matched && (best == nil || o.end > best.after.pos) {", new: "if matched && best == nil {"},
	// Associativity and the bound of the last step have no mutant here: the right operand's own loop
	// applies every operator that binds tighter before the outer loop sees it, so breaking either
	// changes the shape of the value, which no reported exit carries. Every input of up to four tokens
	// of the operator shapes gives the same exits with either rule broken.
	{name: "a non-associative operator chains", file: "operators.go", run: "oracle",
		old: "a.part.op.Assoc == ast.AssocNone && level == lastNone", new: "false && level == lastNone"},
	{name: "the last rule of a cycle leads it", file: "facts.go", run: "oracle",
		old: "\t\t\tif onCycle[name] {\n\t\t\t\tcandidate = name\n\t\t\t\tbreak\n\t\t\t}",
		new: "\t\t\tif onCycle[name] {\n\t\t\t\tcandidate = name\n\t\t\t}"},
	{name: "the comparison ignores the examined end", file: "compare.go", run: "compare",
		old: "case !want[outcomeOf{o.Matched, o.End, o.Examined}]:", new: "case !want[outcomeOf{o.Matched, o.End, 0}] && false:"},
	{name: "the comparison accepts calls the reference does not make", file: "compare.go", run: "compare",
		old: "\t\tcase !ok:\n\t\t\tdiffs = append(", new: "\t\tcase !ok:\n\t\t\tcontinue\n\t\t\tdiffs = append("},
	{name: "the comparison accepts any skipped call", file: "compare.go", run: "compare",
		old: "\t\t\tif !failing {", new: "\t\t\tif false {"},
	{name: "the start call is not compared", file: "compare.go", run: "compare",
		old: "if o.Matched != res.Matched || o.End != res.End || o.Examined != res.Examined {", new: "if false {"},
}

// TestReferenceMutants checks that the oracle notices a broken rule of the reference, and that the
// comparison's tests notice a comparison that lets differences through: each mutant, applied with an
// overlay, must make its test run fail. A mutant of the reference must fail a shape that the oracle
// passes without it, so that a shape on which a producer still differs does not hide a mutant.
func TestReferenceMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the oracle once per mutant")
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	baseOut, err := runMutantTests(dir, "", "^TestExaminedOracle$")
	baseline := failedOracleCases(baseOut)
	if err != nil && len(baseline) == 0 {
		t.Fatalf("the oracle fails to run without a mutant:\n%s", mutantTail(baseOut))
	}
	for _, m := range referenceMutants {
		t.Run(m.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(dir, m.file))
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(src), m.old); n != 1 {
				t.Fatalf("%d copies of the mutated code in %s, want 1", n, m.file)
			}
			tmp := t.TempDir()
			mutated := filepath.Join(tmp, m.file)
			if err := os.WriteFile(mutated, []byte(strings.Replace(string(src), m.old, m.new, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(dir, m.file): mutated}})
			overlayFile := filepath.Join(tmp, "overlay.json")
			if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
				t.Fatal(err)
			}
			if m.run == "compare" {
				out, err := runMutantTests(dir, overlayFile, "^TestCompareCalls$")
				if err == nil || !strings.Contains(string(out), "--- FAIL: TestCompareCalls") {
					t.Fatalf("TestCompareCalls does not fail:\n%s", mutantTail(out))
				}
				return
			}
			out, _ := runMutantTests(dir, overlayFile, "^TestExaminedOracle$")
			for name := range failedOracleCases(out) {
				if !baseline[name] {
					return
				}
			}
			t.Fatalf("no shape fails that passes without the mutant:\n%s", mutantTail(out))
		})
	}
}

// runMutantTests runs the tests that match pattern on the shapes, with the overlay if it is not empty.
func runMutantTests(dir, overlay, pattern string) ([]byte, error) {
	args := []string{"test", "-count=1", "-tags", "sxir_long", "-run", pattern}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), shapesOnly+"=1", "GOTOOLCHAIN=local")
	return cmd.CombinedOutput()
}

// failedOracleCases are the shapes that a run of TestExaminedOracle reports as failing.
func failedOracleCases(out []byte) map[string]bool {
	const prefix = "--- FAIL: TestExaminedOracle/cases/"
	failed := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if _, rest, ok := strings.Cut(line, prefix); ok {
			name, _, _ := strings.Cut(rest, " ")
			failed[name] = true
		}
	}
	return failed
}

// mutantTail is the end of a test run's output.
func mutantTail(out []byte) string {
	s := string(out)
	if len(s) > 4000 {
		s = "..." + s[len(s)-4000:]
	}
	return s
}
