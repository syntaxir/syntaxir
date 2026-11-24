package fmt_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func TestFormatPreservesMinusSemantics(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = - -"a" $$`,
		`rule main = (- -"a" / "b") $$`,
		`rule main = (- #cut "a" "b" / "a" "c") $$`,
		`rule main = "a" {- -1 == 1} $$`,
		"type P = { N int }\nrule main P = \"a\" $$ => P{N: - -1}",
	} {
		t.Run(src, func(t *testing.T) {
			for _, options := range []check.Options{{}, {DisableMemo: true}} {
				before := enginetest.MustCompile(t, src, options)
				after := enginetest.MustCompile(t, fmt.Format(entry.Grammar(before)), options)
				for _, input := range []string{"", "a", "b", "aa", "ab", "ac"} {
					enginetest.CheckBackends(t, before, "main", input)
					enginetest.CheckBackends(t, after, "main", input)
					for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
						for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
							opts := engine.ParseOptions{Backend: backend, Unit: unit}
							n1, err1 := before.ParseWith("main", input, opts)
							n2, err2 := after.ParseWith("main", input, opts)
							if codegentest.ResultJSON(n1, err1) != codegentest.ResultJSON(n2, err2) {
								t.Errorf("%s/%s on %q: formatting changes %s to %s", backend, unit, input, codegentest.ResultJSON(n1, err1), codegentest.ResultJSON(n2, err2))
							}
						}
					}
				}
			}
		})
	}
}

// TestFormatPreservesCorpus checks that formatting each corpus grammar and
// every grammar file in the repository and parsing the result again gives the
// same grammar, apart from source positions, that formatting it again changes
// nothing, and that the corpus grammars parse their inputs the same way.
func TestFormatPreservesCorpus(t *testing.T) {
	t.Parallel()
	type source struct {
		name, src string
		inputs    []string
	}
	var sources []source
	for _, c := range codegentest.GenCorpus(t) {
		sources = append(sources, source{c.Name, c.Src, c.Inputs})
	}
	out, err := exec.Command("git", "-C", "../../..", "ls-files", "*.sxir").Output()
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("git ls-files:", err)
	}
	if err != nil {
		t.Fatal("git ls-files:", err)
	}
	listed := strings.Fields(string(out))
	if len(listed) == 0 {
		t.Fatal("git ls-files listed no grammars of the module")
	}
	for _, name := range listed {
		data, err := os.ReadFile(filepath.Join("../../..", name))
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source{name: name, src: string(data)})
	}
	if len(sources) < 100 {
		t.Fatalf("only %d grammars", len(sources))
	}
	for _, s := range sources {
		t.Run(s.name, func(t *testing.T) {
			before, err := parser.Parse(s.src)
			if err != nil {
				t.Fatal(err)
			}
			formatted := fmt.Format(before)
			after, err := parser.Parse(formatted)
			if err != nil {
				t.Fatalf("%v\n%s", err, formatted)
			}
			if want, got := grammarWithoutSources(t, before), grammarWithoutSources(t, after); got != want {
				t.Fatalf("formatting changes the grammar:\n%s\n%s\n%s", formatted, want, got)
			}
			if again := fmt.Format(after); again != formatted {
				t.Errorf("formatting is not stable:\n%s\n---\n%s", formatted, again)
			}
			if len(s.inputs) == 0 {
				return
			}
			p1, p2 := enginetest.MustCompile(t, s.src, check.Options{NoProjections: true}), enginetest.MustCompile(t, formatted, check.Options{NoProjections: true})
			for _, input := range s.inputs {
				for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					opts := engine.ParseOptions{Unit: unit}
					n1, err1 := p1.ParseWith("main", input, opts)
					n2, err2 := p2.ParseWith("main", input, opts)
					if r1, r2 := codegentest.ResultJSON(n1, err1), codegentest.ResultJSON(n2, err2); r1 != r2 {
						t.Errorf("%v on %q: formatting changes %s to %s", unit, input, r1, r2)
					}
				}
			}
		})
	}
}

// grammarWithoutSources returns the JSON form of g without source ranges
// (source and fields such as skipSource).
func grammarWithoutSources(t *testing.T, g *ast.Grammar) string {
	data, err := ast.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	var strip func(any)
	strip = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if k == "source" || strings.HasSuffix(k, "Source") {
					delete(v, k)
				} else {
					strip(x)
				}
			}
		case []any:
			for _, x := range v {
				strip(x)
			}
		}
	}
	strip(v)
	data, _ = json.Marshal(v)
	return string(data)
}
