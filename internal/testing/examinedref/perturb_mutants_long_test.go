//go:build sxir_long

package examinedref_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// producerEdit replaces the only copy of old in file (slash-separated, from the module root).
type producerEdit struct{ file, old, new string }

// producerMutant breaks the examined end in a copy of source files, which `go test -overlay` builds in
// place of the originals, in the copy of the library that generated Go parsers import (goLib, as
// codegentest.GoLibPatchEnv describes it), or in the library of the generated TypeScript parsers (tsLib,
// as codegentest.TSLibPatchEnv describes it), which reaches them embedded in the generator. Each test of
// tests runs in the package pkg (this one if empty); each of catchers must fail, and when backends is not
// empty, each of its messages must name each backend (the engine's code is shared, so a mutant of it must
// show on every backend). The tests that are not catchers are run for their verdict, which is logged.
type producerMutant struct {
	name     string
	edits    []producerEdit
	goLib    *producerEdit
	tsLib    *producerEdit
	pkg      string
	tests    []string
	catchers []string
	backends []string
}

const (
	mutantStateInput   = "internal/runtime/state/input.go"
	mutantStateRuntime = "internal/runtime/state/runtime.go"
	mutantPerturb      = "internal/testing/examinedref/perturb.go"
	mutantHighWater    = "internal/testing/examinedref/highwater.go"
	mutantGoLib        = "pkg/runtime/libgo/lib.go"
	mutantTSLib        = "internal/backend/codegen/typescript/lib/lib.ts"
)

var producerMutants = []producerMutant{
	// The engine: an examined end that is too small, one that is too large, and reads of a stream that no
	// examined end records.
	{name: "a rejected literal leaves out the character that differs (examined end too small)",
		edits: []producerEdit{{mutantStateInput, "\t\tp.touch(p.Pos + k + 1)\n\t\tp.Pos += k\n\t\treturn false\n", "\t\tp.touch(p.Pos + k)\n\t\tp.Pos += k\n\t\treturn false\n"}},
		tests: []string{"TestEnginePerturbation"}, catchers: []string{"TestEnginePerturbation"}, backends: producerBackends},
	// Perturbation cannot see an examined end that is too large: a call that reports more than it read
	// keeps its trace under every change beyond what it read, which is what the check requires. It runs for
	// the record of that verdict.
	{name: "every examination counts one more slot (examined end too large)",
		edits: []producerEdit{{mutantStateRuntime, "\tif n > p.Hw {\n\t\tp.Hw = n\n\t}\n", "\tif n+1 > p.Hw {\n\t\tp.Hw = n + 1\n\t}\n"}},
		tests: []string{"TestEnginePerturbation", "TestEngineStreamHighWater"}, catchers: []string{"TestEngineStreamHighWater"}, backends: producerBackends},
	{name: "a literal compared with the input of a stream records nothing (unrecorded stream read)",
		edits: []producerEdit{
			{mutantStateInput, "\t\t\tp.touch(p.Pos + 1)\n\t\t\tc, ok := p.byteAt(p.Pos)\n", "\t\t\tc, ok := p.byteAt(p.Pos)\n"},
			{mutantStateInput, "\t\tp.touch(p.Pos + 1)\n\t\tif p.Pos < p.Base || !p.Fill(p.Pos+1) || p.In[p.Pos-p.Base] != r {\n",
				"\t\tif p.Pos < p.Base || !p.Fill(p.Pos+1) || p.In[p.Pos-p.Base] != r {\n"},
		},
		tests: []string{"TestEngineStreamHighWater"}, catchers: []string{"TestEngineStreamHighWater"}, backends: producerBackends},
	{name: "a stream reads one unit beyond what the parse asks for (unrecorded stream read)",
		edits: []producerEdit{{mutantStateInput, "\tok := in.fill(n)\n", "\tin.fill(n + 1)\n\tok := in.Loaded() >= n\n"}},
		tests: []string{"TestEngineStreamHighWater"}, catchers: []string{"TestEngineStreamHighWater"}, backends: producerBackends},
	// The generated Go parsers, whose library records the examined ends of their own: an examined end that
	// is too small, which perturbation shows, and one that is too large, which the stream's high-water mark
	// shows.
	{name: "a generated rejected literal leaves out the character that differs (examined end too small)",
		goLib: &producerEdit{mutantGoLib, "\tp.documentTouch(p.Pos + k + 1)\n", "\tp.documentTouch(p.Pos + k)\n"},
		tests: []string{"TestGeneratedPerturbationAndHighWater"}, catchers: []string{"TestGeneratedPerturbationAndHighWater"}},
	{name: "every generated examination counts one more slot (examined end too large)",
		goLib: &producerEdit{mutantGoLib, "\tif p.track && end > p.docExamined {\n\t\tp.docExamined = end\n",
			"\tif p.track && end+1 > p.docExamined {\n\t\tp.docExamined = end + 1\n"},
		tests: []string{"TestGeneratedPerturbationAndHighWater"}, catchers: []string{"TestGeneratedPerturbationAndHighWater"}},
	{name: "a hot generated scan walk drops what the rest of it examined (examined end too small)",
		goLib: &producerEdit{"pkg/runtime/libgo/scan_sites.go", "\tp.docExamined = max(p.scanWalk.savedExamined, x)\n", "\tp.docExamined = p.scanWalk.savedExamined\n"},
		tests: []string{"TestGeneratedPerturbationAndHighWater"}, catchers: []string{"TestGeneratedPerturbationAndHighWater"}},
	// The generated TypeScript parsers, whose library records the examined ends of their own. They have no
	// stream, so the comparison with the reference (part 1 of the oracle) is what shows an examined end
	// that is too large.
	{name: "every TypeScript examination counts one slot less (examined end too small)",
		tsLib: &producerEdit{mutantTSLib, "if (end > this.docExamined) this.docExamined = end;", "if (end - 1 > this.docExamined) this.docExamined = end - 1;"},
		tests: []string{"TestTSPerturbation"}, catchers: []string{"TestTSPerturbation"}, backends: []string{"typescript"}},
	{name: "every TypeScript examination counts one more slot (examined end too large)",
		tsLib: &producerEdit{mutantTSLib, "if (end > this.docExamined) this.docExamined = end;", "if (end + 1 > this.docExamined) this.docExamined = end + 1;"},
		pkg:   "syntaxir.dev/sxir/internal/backend/codegen/typescript/print",
		tests: []string{"TestTSObservedExaminedEndsAreExact"}, catchers: []string{"TestTSObservedExaminedEndsAreExact"}},

	// The harness: checks that pass whatever the producer does.
	{name: "perturbations that change nothing",
		edits: []producerEdit{
			{mutantPerturb, "\t\t\tadd(Perturbation{Substitute, c, b.String()})\n", "\t\t\tadd(Perturbation{Substitute, c, in})\n"},
			{mutantPerturb, "\t\tadd(Perturbation{Truncate, c, prefix})\n", "\t\tadd(Perturbation{Truncate, c, in})\n"},
			{mutantPerturb, "\t\t\tadd(Perturbation{Extend, n, in + x})\n", "\t\t\tadd(Perturbation{Extend, n, in})\n"},
			{mutantPerturb, "\t\tadd(Perturbation{Extend, n, in + in})\n", "\t\tadd(Perturbation{Extend, n, in})\n"},
		},
		tests: []string{"TestPerturbations", "TestEnginePerturbation"}, catchers: []string{"TestPerturbations", "TestEnginePerturbation"}},
	{name: "a lockstep comparison that blames no running call",
		edits: []producerEdit{{mutantPerturb, "if x := run.tr[j]; x.Examined <= change {", "if x := run.tr[j]; x.Examined < 0 {"}},
		tests: []string{"TestCheckLockstep"}, catchers: []string{"TestCheckLockstep"}},
	{name: "a lockstep comparison that checks only the innermost running call",
		edits: []producerEdit{{mutantPerturb, "\tfor n, i := range stack {\n\t\tif dep.Variables", "\tfor n, i := range stack[max(len(stack)-1, 0):] {\n\t\tif dep.Variables"}},
		tests: []string{"TestCheckLockstep"}, catchers: []string{"TestCheckLockstep"}},
	{name: "a growth exemption that ignores the position",
		edits: []producerEdit{{mutantPerturb, "d == c && tr[j].Pos == tr[i].Pos {", "d == c {"}},
		tests: []string{"TestCheckLockstep"}, catchers: []string{"TestCheckLockstep"}},
	{name: "a growth exemption of calls on no cycle",
		edits: []producerEdit{{mutantPerturb, "\tc, ok := cycles[tr[i].Rule]\n\tif !ok {\n\t\treturn false\n\t}\n", "\tc := cycles[tr[i].Rule]\n"}},
		tests: []string{"TestCheckLockstep"}, catchers: []string{"TestCheckLockstep"}},
	{name: "a high-water check that accepts reads beyond the examined end",
		edits: []producerEdit{{mutantHighWater, "\tif !ok || need != final {\n", "\tif !ok || need.Delivered > final.Delivered {\n"}},
		tests: []string{"TestCheckHighWater"}, catchers: []string{"TestCheckHighWater"}},
	{name: "a decoding that reads only the bytes of its character",
		edits: []producerEdit{{mutantHighWater, "\t\tfor off+k <= len(in) && !utf8.FullRuneInString(in[off:off+k]) {\n",
			"\t\tfor _, size := utf8.DecodeRuneInString(in[off:]); k < size; {\n"}},
		tests: []string{"TestNeeded"}, catchers: []string{"TestNeeded"}},
}

var producerBackends = []string{"closure", "bytecode", "bytecode-iterative"}

// TestEngineExaminedMutants is the negative control of the perturbation and high-water tests: each mutant
// of the engine must make the test that is to catch it fail on every backend, and each mutant of the checks
// must make their own tests fail.
func TestEngineExaminedMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	for i, m := range producerMutants {
		// One mutant at a time: each runs a go test of its own, which builds the package and runs the corpus.
		t.Run(m.name, func(t *testing.T) {
			dir := t.TempDir()
			texts := map[string]string{}
			for _, e := range m.edits {
				file, src := mutanttest.Source(t, e.file)
				if _, ok := texts[file]; !ok {
					texts[file] = string(src)
				}
				if n := strings.Count(texts[file], e.old); n != 1 {
					t.Fatalf("%s has %d copies of %q, not one", e.file, n, e.old)
				}
				texts[file] = strings.Replace(texts[file], e.old, e.new, 1)
			}
			replace := map[string]string{}
			j := 0
			for file, text := range texts {
				mutant := filepath.Join(dir, fmt.Sprintf("mutant_%d_%d.go", i, j))
				j++
				if err := os.WriteFile(mutant, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
				replace[file] = mutant
			}
			overlay, err := json.Marshal(map[string]map[string]string{"Replace": replace})
			if err != nil {
				t.Fatal(err)
			}
			ov := filepath.Join(dir, "overlay.json")
			if err := os.WriteFile(ov, overlay, 0o644); err != nil {
				t.Fatal(err)
			}
			// -vet=off: the verdict reads what the tests report, and vet would only analyze the mutated code again.
			cmd := exec.Command("go", "test", "-count=1", "-vet=off", "-tags", "sxir_long", "-overlay", ov,
				"-run", "^("+strings.Join(m.tests, "|")+")$", cmp.Or(m.pkg, "."))
			cmd.Env = append(os.Environ(), "GOFLAGS=-trimpath")
			if m.goLib != nil {
				patch, err := json.Marshal(struct{ File, Old, New string }{m.goLib.file, m.goLib.old, m.goLib.new})
				if err != nil {
					t.Fatal(err)
				}
				cmd.Env = append(cmd.Env, codegentest.GoLibPatchEnv+"="+string(patch))
			}
			if m.tsLib != nil {
				patch, err := json.Marshal(struct{ File, Old, New string }{m.tsLib.file, m.tsLib.old, m.tsLib.new})
				if err != nil {
					t.Fatal(err)
				}
				cmd.Env = append(cmd.Env, codegentest.TSLibPatchEnv+"="+string(patch))
			}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the tests passed with the mutant:\n%s", out)
			}
			for _, test := range m.tests {
				failed := strings.Contains(string(out), "--- FAIL: "+test+" ")
				t.Logf("%s failed: %v", test, failed)
				if !failed && slices.Contains(m.catchers, test) {
					t.Errorf("%s did not fail:\n%s", test, outputTail(out))
				}
			}
			for _, b := range m.backends {
				line := regexp.MustCompile(`_test\.go:\d+: ` + regexp.QuoteMeta(b) + ` (bytes|code-points)[ :]`)
				if !line.Match(out) {
					t.Errorf("no failure on %s:\n%s", b, outputTail(out))
				}
			}
		})
	}
}

// outputTail returns the end of a long output.
func outputTail(out []byte) string {
	const max = 8 << 10
	if len(out) > max {
		return "...\n" + string(out[len(out)-max:])
	}
	return string(out)
}
