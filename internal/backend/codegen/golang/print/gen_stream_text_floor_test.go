package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// streamTextFloorGrammar takes, after its #stream repetition, the text of a string node made before
// it, which a stream holds.
const streamTextFloorGrammar = `@start
rule main = h:("h" "h") g:"g" item* #stream { text($g) == "g" } $$
rule item = "a" ";"
`

// TestGeneratedStreamTextBeforeCommitFails checks that a generated Go stream whose predicate after the
// #stream repetition takes the text of a node that begins before the last commit point, which the
// checker rejects in every grammar, returns the internal error from ParseStream, with and without a
// compute report, however the reader splits the input. The parser is that of a grammar that takes the
// text of a string node, changed to read the capture of a tuple node made before the repetition.
// Before the first commit the text is whole, and the predicate fails the parse as a batch parse would.
func TestGeneratedStreamTextBeforeCommitFails(t *testing.T) {
	t.Parallel()
	goBin := skipRunsGoBin(t)
	codegentest.NeedsBudgetControls(t)
	g, err := parseTest(streamTextFloorGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	codegentest.GeneratePackageIn(t, dir, "textfloor", "g", g, common.GenOptions{Start: "main", Stream: true})
	parser := filepath.Join(dir, "g", "parser.go")
	src, err := os.ReadFile(parser)
	if err != nil {
		t.Fatal(err)
	}
	// main captures h and g, in slots 0 and 1; only the predicate reads a capture.
	if n := strings.Count(string(src), "c.Cap(1))"); n == 0 || strings.Contains(string(src), "c.Cap(0))") {
		t.Fatalf("the predicate reads %d captures of slot 1\n%s", n, src)
	}
	if err := os.WriteFile(parser, []byte(strings.ReplaceAll(string(src), "c.Cap(1))", "c.Cap(0))")), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"go.mod":  codegentest.GoModAt("textfloor", codegentest.PatchedLib(t)),
		"main.go": streamTextFloorConsumer,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	var want []string
	for _, c := range []struct{ in, got string }{
		{"hhga;a;a;", "error internal error: the text of [0, 2) begins before the commit point 9"},
		{"hhga;", "error internal error: the text of [0, 2) begins before the commit point 5"},
		{"hhg", "syntax error"},
	} {
		for _, opts := range []string{"plain", "report"} {
			for _, size := range []string{"1", "64"} {
				want = append(want, c.in+" "+opts+" "+size+": "+c.got)
			}
		}
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("got  %s\nwant %s", got[k], want[k])
		}
	}
}

const streamTextFloorConsumer = `package main

import (
	"errors"
	"fmt"
	"io"

	"textfloor/g"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

type chunkReader struct {
	s    string
	size int
}

func (r *chunkReader) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := min(len(r.s), r.size, len(b))
	copy(b, r.s[:n])
	r.s = r.s[n:]
	return n, nil
}

func main() {
	for _, in := range []string{"hhga;a;a;", "hhga;", "hhg"} {
		for _, report := range []bool{false, true} {
			for _, size := range []int{1, 64} {
				var opts []g.ParseOption
				name := "plain"
				if report {
					var units uint64
					opts, name = append(opts, sxirruntime.WithComputeReport(&units)), "report"
				}
				err := g.ParseStream(&chunkReader{s: in, size: size}, func(*g.Node) error { return nil }, opts...)
				var syntax *g.SyntaxError
				switch {
				case errors.As(err, &syntax):
					fmt.Printf("%s %s %d: syntax error\n", in, name, size)
				default:
					fmt.Printf("%s %s %d: error %v\n", in, name, size, err)
				}
			}
		}
	}
}
`
