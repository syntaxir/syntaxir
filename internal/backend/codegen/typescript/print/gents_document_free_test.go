package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// documentFreeScript drives the BudgetControl of the library: under a limit of 100, the default or an
// explicit one, 60 counted units, 80 units in a region of Document work and 40 more counted units fit,
// and the next unit is over the limit, which the interruption reports without the units of the region.
// A region opens only when asked for, and not inside another. Without a limit, the report of the units
// leaves the region out as well.
const documentFreeScript = `
{
  const g = mkGrammar(1000, false, false, false);
  const o = resolveOptions(g, {}, encodeUTF8("x"));
  if (o instanceof Error) throw o;
  const report = (f: () => void): string => {
    try {
      f();
      return "ok";
    } catch (x) {
      if (x instanceof Fatal && x.err instanceof ParseInterruptionError) return x.err.kind + " " + x.err.limit + "/" + x.err.used + "/" + x.err.requested;
      throw x;
    }
  };
  const session = (budget: number): string[] => {
    const units = { units: 0 };
    const c = new BudgetControl({ ...o, budget, report: units });
    const out: string[] = [];
    out.push(report(() => c.chargeUnits(60)));
    out.push(String(c.freeBegin(false) === noFreeRegion));
    const m = c.freeBegin(true);
    out.push(String(m === noFreeRegion), String(c.freeBegin(true) === noFreeRegion));
    out.push(report(() => c.chargeUnits(80)));
    c.freeEnd(m);
    out.push(report(() => c.chargeUnits(40)));
    out.push(report(() => c.charge(1)));
    out.push(String(c.used()));
    return out;
  };
  console.log(session(100).join(","));
  console.log(session(0).join(","));
}
`

// TestTSDocumentFreeRegions checks the regions of Document work of the TypeScript library's
// BudgetControl (freeBegin, freeEnd), which leave the work that only a Document parse does out of the
// count against every Compute limit and out of the reported units. As a negative control, the library
// whose region does not lift the limit must fail.
func TestTSDocumentFreeRegions(t *testing.T) {
	g, err := parseTest(`rule main = "a"*`)
	if err != nil {
		t.Fatal(err)
	}
	opts := common.GenOptions{Start: "main", Document: true, NoOpt: []string{common.UsedFeatures}}
	node := tooltest.RequireNode(t)
	run := func(mutate func(string) string) string {
		code, err := codegentest.GenerateTSBundle(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		src := string(code)
		if mutate != nil {
			src = mutate(src)
		}
		dir := t.TempDir()
		for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src + documentFreeScript, "main.ts": `import "./parser.ts";` + "\n"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(node, "main.ts")
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	const want = "ok,true,false,true,ok,ok,compute-limit 100/100/1,100\n" +
		"ok,true,false,true,ok,ok,ok,101\n"
	if got := run(nil); got != want {
		t.Fatalf("got\n%s\nwant\n%s", codegentest.Clip(got, 0), want)
	}
	const lift = "    this.budget = 0;\n"
	if got := run(func(src string) string {
		if strings.Count(src, lift) != 1 {
			t.Fatalf("the library has %d copies of %q, want 1", strings.Count(src, lift), lift)
		}
		return strings.Replace(src, lift, "", 1)
	}); got == want {
		t.Error("the library whose region of Document work does not lift the limit passes")
	}
}
