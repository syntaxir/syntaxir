package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// documentUnitsScript reports, for each input of INPUTS, the compute units of the first parse of a
// Document of the input and whether it succeeded.
const documentUnitsScript = `
{
  const out: string[] = [];
  for (const input of INPUTS) {
    const { document } = createDocument(input);
    const r = { units: 0 };
    const res = document!.parse({ computeReport: r });
    out.push(r.units + "/" + (res.error === null));
  }
  console.log(out.join(" "));
}
`

// TestTSDocumentUnitsAgreeWithEngineOnAssignments checks that a Document parse of the TypeScript library
// looks a rule that only a Document memoizes up by the assignments of the string variables of its key, as
// the engine's Document does, and so counts the engine's compute units. In ActionEnvironmentGrammar, peek
// and then main call item at 0, and both reach late at 3 under equal values of last and tag that two
// assignments made: on "#x:~a" the second call of late misses the memo and advances over "~a" again;
// DocumentAssignmentGrammar binds equal texts of the input. A lookup by the values would hit the entry of the first
// call and leave out the units of the second evaluation. As a negative control, the library whose
// Document keys such a rule by the values must count other units.
func TestTSDocumentUnitsAgreeWithEngineOnAssignments(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	node := tooltest.RequireNode(t)
	for _, c := range []struct {
		name, src string
		inputs    []string
	}{
		{"action environments", codegentest.ActionEnvironmentGrammar, []string{"#x:~a", "#x:(~ab)", "#x:a", "~a", "[a,b]~c"}},
		{"assigned texts", codegentest.DocumentAssignmentGrammar, []string{"[a]~b", "[ab]~bc!", "[a]~b%"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.src)
			if err != nil {
				t.Fatal(err)
			}
			prog := mustCompile(t, c.src)
			var want []string
			for _, in := range c.inputs {
				d, err := prog.NewDocumentWith("main", in, engine.ParseOptions{})
				if err != nil {
					t.Fatal(err)
				}
				var units uint64
				o := d.ParseDefaults()
				o.ComputeReport = &units
				_, err = d.ParseWith(o)
				want = append(want, fmt.Sprintf("%d/%v", units, err == nil))
			}
			quoted, err := json.Marshal(c.inputs)
			if err != nil {
				t.Fatal(err)
			}
			script := strings.Replace(documentUnitsScript, "INPUTS", string(quoted), 1)
			run := func(mutate func(string) string) string {
				code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Document: true})
				if err != nil {
					t.Fatal(err)
				}
				src := string(code)
				if mutate != nil {
					src = mutate(src)
				}
				dir := t.TempDir()
				for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src + script, "main.ts": `import "./parser.ts";` + "\n"} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command(node, "main.ts")
				cmd.Dir = dir
				out, _ := cmd.CombinedOutput()
				return strings.TrimSpace(string(out))
			}
			w := strings.Join(want, " ")
			if got := run(nil); got != w {
				t.Fatalf("Document units of %q: %s, want the engine's %s", c.inputs, codegentest.Clip(got, 0), w)
			}
			t.Logf("units of %q: %s", c.inputs, w)
			if got := run(func(src string) string {
				const key = "  override memoKey(r: Rule): Binding[] | null {\n"
				if !strings.Contains(src, key) {
					t.Fatalf("the library has no %q", key)
				}
				return strings.Replace(src, key, key+"    return super.memoKey(r);\n", 1)
			}); got == w {
				t.Errorf("the library whose Document keys its rules by the values counts the engine's units %s", w)
			}
		})
	}
}
