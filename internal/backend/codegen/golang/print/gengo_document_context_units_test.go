package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// documentContextGrammar grows b at the start of a in the context of a's growth, so that the steps
// of b's growth are kept and compared by value to give equal results one identity. num ends in an
// empty run of blanks, whose node a Document copies when it moves a reused num and builds anew when
// it evaluates one, so that steps compare equal trees made in different parses.
const documentContextGrammar = `rule main = a !.
rule a = b "+" num / num
rule b = a "-" num / num
rule num = [0-9]+ blanks
rule blanks = " "*`

// documentContextEdits edits the text of documentContextGrammar before, inside and after the
// growths, with and without changing its length, and back.
var documentContextEdits = []struct {
	start, end int
	text       string
}{
	{0, 1, "7"}, {8, 9, "6"}, {4, 4, "8"}, {0, 0, "9"}, {2, 2, " "}, {5, 6, ""}, {10, 11, "-3"},
	{3, 3, "x"}, {3, 4, ""}, {0, 2, "1"}, {6, 6, "+2-2"},
}

// ruleName matches the rule name of a node in the JSON form of a tree.
var ruleName = regexp.MustCompile(`,"rule":"[a-z]+"`)

// TestGeneratedDocumentContextUnitsAgreeWithEngine holds a generated Go Document whose results grow
// in the contexts of other growths to the engine's Documents over a series of edits: each parse
// counts the same compute units, evaluates and reuses as many results, and returns the same tree.
// The units of a parse depend on which step results the comparison finds equal, which takes their
// values, positions included, and nothing else of a node: not how many edits the node has moved
// with.
func TestGeneratedDocumentContextUnitsAgreeWithEngine(t *testing.T) {
	t.Parallel()
	goBin := skipRunsGoBin(t)
	codegentest.NeedsBudgetControls(t)
	const text = "1+2-3+4-5+6"
	g, err := parseTest(documentContextGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, content []byte) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(codegentest.GoMod("docctx")))
	files, err := codegentest.Generate(t, g, common.GenOptions{Module: "docctx/g", Package: "g", Start: "main", Lib: common.LibEmbed, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		write("g/"+f.Path, f.Content)
	}
	var edits strings.Builder
	for _, e := range documentContextEdits {
		fmt.Fprintf(&edits, "{%d, %d, %q}, ", e.start, e.end, e.text)
	}
	write("main.go", []byte(fmt.Sprintf(`package main

import (
	"encoding/json"
	"fmt"

	"docctx/g"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

func main() {
	d, err := g.NewDocument(%q)
	if err != nil {
		panic(err)
	}
	parse := func() {
		var n uint64
		v, err := d.Parse(sxirruntime.WithComputeReport(&n))
		tree, jerr := json.Marshal(v)
		if jerr != nil {
			panic(jerr)
		}
		s := d.Stats()
		fmt.Printf("%%d %%v %%d %%d %%s\n", n, err == nil, s.Evaluated, s.Reused, tree)
	}
	parse()
	for _, e := range []struct {
		start, end int
		text       string
	}{%s} {
		if err := d.Edit(e.start, e.end, e.text); err != nil {
			panic(err)
		}
		parse()
	}
}
`, text, edits.String())))
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(documentContextEdits)+1 {
		t.Fatalf("%d lines for %d parses:\n%s", len(got), len(documentContextEdits)+1, out)
	}
	prog := mustCompile(t, documentContextGrammar)
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		d, err := prog.NewDocumentWith("main", text, engine.ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		for i := range got {
			if i > 0 {
				e := documentContextEdits[i-1]
				if err := d.Edit(e.start, e.end, e.text); err != nil {
					t.Fatal(err)
				}
			}
			var n uint64
			v, err := d.ParseWith(engine.ParseOptions{Backend: b, ComputeReport: &n})
			tree, jerr := json.Marshal(v)
			if jerr != nil {
				t.Fatal(jerr)
			}
			s := d.Stats()
			// A generated Go node names no rule of its own; the trees compare without them.
			tree = ruleName.ReplaceAll(tree, nil)
			want := fmt.Sprintf("%d %v %d %d %s", n, err == nil, s.Evaluated, s.Reused, tree)
			if got[i] != want {
				t.Errorf("%s, parse %d of %q:\ngenerated %s\nengine    %s", b, i, d.Text(), got[i], want)
			}
		}
	}
}
