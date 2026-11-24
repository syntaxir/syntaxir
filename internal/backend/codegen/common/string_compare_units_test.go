package common_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// stringCompareGrammar compares strings in predicates and in the value of the start rule, under
// every relation, on operands that share their bytes in Go and on equal operands that do not:
//
//   - same compares the variable v with itself, and lit compares it with the literal it was set
//     from, which the Go linker gives the same bytes;
//   - span compares the text of a capture with itself, which Go takes as the same slice of the input;
//   - built compares the text of a capture with v, equal bytes in other memory;
//   - order compares texts of other lengths, and multi texts of multi-byte characters.
const stringCompareGrammar = `type R = { N int; Last string; Eq bool }
var v string
var last string
rule main R = {v := "word"} xs:(item " "?)* $$ => R{N: len($xs), Last: last, Eq: last == v}
rule item = same / lit / span / built / order / multi
rule same = "S" w:word {v == v} {last := text($w)}
rule lit = "L" w:word {v == "word" && "word" <= v} {last := text($w)}
rule span = "P" w:word {text($w) == text($w) && !(text($w) != text($w))} {last := text($w)}
rule built = "B" w:word {text($w) == v || text($w) > v} {last := text($w)}
rule order = "O" w:word {text($w) < "m" || text($w) >= "zz"} {last := text($w)}
rule multi = "M" w:[^ ]+ #atomic {text($w) != "é" && text($w) >= "é"} {last := text($w)}
rule word = [a-z]+ #atomic`

var stringCompareInputs = []string{
	"Sword", "Sab", "Lword", "Lwordy", "Pab", "Pwordwordword", "Bword", "Bwore", "Bwor", "Bzz",
	"Oab", "Ozzz", "Om", "Mé", "Méé", "Mz", "Mée", "Sword Lword Pword Bword Oa Méé", "Bwo Sword", "",
}

// stringCompareScript prints the units of the node parse and of the typed parse of each input of
// INPUTS, as "node/typed" separated by spaces.
const stringCompareScript = `
{
  const units = (f: (r: { units: number }) => void): number => {
    const r = { units: 0 };
    f(r);
    return r.units;
  };
  const out: string[] = [];
  for (const input of INPUTS) {
    out.push(units(r => parseWithOptions(input, { computeReport: r })) + "/" + units(r => parseValueWithOptions(input, { computeReport: r })));
  }
  console.log(out.join(" "));
}
`

// TestStringComparisonUnitsAgree checks that a comparison of strings counts the same compute units in
// the engine, generated Go and generated TypeScript, node and typed parses alike: the length in bytes
// of the shorter operand (spec/runtime/parsing.md, Cost), whether or not the operands share memory.
// Go shares the bytes of equal literals and of slices of the input, the engine's values share them
// differently, and TypeScript strings have no identity at all, so a count that depended on sharing
// would differ between the targets on the inputs of same, lit and span.
func TestStringComparisonUnitsAgree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated Go and TypeScript parsers")
	}
	if !state.BudgetControlsEnabled {
		t.Skip("the reference build counts no compute units")
	}
	codegentest.NeedsBudgetControls(t)
	g, err := parseTest(stringCompareGrammar)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, in := range stringCompareInputs {
		var used uint64
		_, _ = prog.ParseWith("main", in, engine.ParseOptions{ComputeReport: &used})
		want = append(want, fmt.Sprintf("%d/%d", used, used))
	}
	wantLine := strings.Join(want, " ")

	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", codegentest.GoMod("strcmp"))
	files, err := codegentest.Generate(t, g, common.GenOptions{Module: "strcmp/g", Package: "g", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		write("g/"+f.Path, string(f.Content))
	}
	write("main.go", fmt.Sprintf(`package main

import (
	"fmt"
	"strings"

	"strcmp/g"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

func main() {
	var out []string
	for _, in := range %#v {
		var n, v uint64
		_, _ = g.Parse(in, sxirruntime.WithComputeReport(&n))
		_, _ = g.ParseValue(in, sxirruntime.WithComputeReport(&v))
		out = append(out, fmt.Sprintf("%%d/%%d", n, v))
	}
	fmt.Println(strings.Join(out, " "))
}
`, stringCompareInputs))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, codegentest.Clip(string(out), 0))
	}
	if got := strings.TrimSpace(string(out)); got != wantLine {
		t.Errorf("units of each input in generated Go, node/typed:\n %s\nwant the engine's\n %s", got, wantLine)
	}

	node := tooltest.RequireNode(t)
	code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	js, _ := json.Marshal(stringCompareInputs)
	tsDir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": string(code) + strings.ReplaceAll(stringCompareScript, "INPUTS", string(js)), "main.ts": `import "./parser.ts";` + "\n"} {
		if err := os.WriteFile(filepath.Join(tsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd = exec.Command(node, "main.ts")
	cmd.Dir = tsDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, codegentest.Clip(string(out), 0))
	}
	if got := strings.TrimSpace(string(out)); got != wantLine {
		t.Errorf("units of each input in generated TypeScript, node/typed:\n %s\nwant the engine's\n %s", got, wantLine)
	}
}
