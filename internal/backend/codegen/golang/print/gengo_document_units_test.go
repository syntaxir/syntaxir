package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A rule that only a Document memoizes, called again at a position under equal values of its key that
// two assignments of the parse bound, misses the memo of the engine's Document, which looks such a rule
// up by the assignments of the strings of its key, and is evaluated again. A generated Document that
// counts logical compute units looks it up the same way, so its total is the engine's: a lookup by the
// values would hit the entry of the first call and leave out the units of the second evaluation. In
// ActionEnvironmentGrammar, peek and then main call item at 0 under different depths, and both reach
// late at 3 under equal values of last and tag: on "#x:~a" the second call of late advances over "~a"
// again. DocumentAssignmentGrammar binds equal texts of the input, which a lookup by the identity of
// the bytes would find equal.
func TestGeneratedDocumentUnitsAgreeWithEngineOnDocumentMemo(t *testing.T) {
	t.Parallel()
	goBin := skipRunsGoBin(t)
	codegentest.NeedsBudgetControls(t)
	for _, c := range []struct {
		name, src string
		inputs    []string
	}{
		{"action environments", codegentest.ActionEnvironmentGrammar, []string{"#x:~a", "#x:(~ab)", "#x:a", "~a"}},
		{"assigned texts", codegentest.DocumentAssignmentGrammar, []string{"[a]~b", "[ab]~bc!", "[a]~b%"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			documentUnitsAgree(t, goBin, c.src, c.inputs)
		})
	}
}

// documentUnitsAgree builds a Document parser for src and holds the compute units of its first parse
// of each input to the engine's Document.
func documentUnitsAgree(t *testing.T, goBin, src string, inputs []string) {
	g, err := parseTest(src)
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
	write("go.mod", []byte(codegentest.GoMod("docunits")))
	files, err := codegentest.Generate(t, g, common.GenOptions{Module: "docunits/g", Package: "g", Start: "main", Lib: common.LibEmbed, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		write("g/"+f.Path, f.Content)
	}
	write("main.go", []byte(fmt.Sprintf(`package main

import (
	"fmt"

	"docunits/g"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

func main() {
	for _, in := range %#v {
		d, err := g.NewDocument(in)
		if err != nil {
			panic(err)
		}
		var n uint64
		_, err = d.Parse(sxirruntime.WithComputeReport(&n))
		fmt.Println(n, err == nil)
	}
}
`, inputs)))
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("%d lines for %d inputs:\n%s", len(lines), len(inputs), out)
	}
	prog := mustCompile(t, src)
	for i, in := range inputs {
		units, ok, _ := strings.Cut(lines[i], " ")
		got, err := strconv.ParseUint(units, 10, 64)
		if err != nil {
			t.Fatalf("malformed line %q", lines[i])
		}
		d, err := prog.NewDocumentWith("main", in, engine.ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var want uint64
		_, err = d.ParseWith(engine.ParseOptions{ComputeReport: &want})
		if got != want || ok != strconv.FormatBool(err == nil) {
			t.Errorf("%q: the generated Document counted %d units (parsed %s), the engine's %d (parsed %v)", in, got, ok, want, err == nil)
		}
	}
}
