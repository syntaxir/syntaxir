package print_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// textCommitReader returns at most size bytes per Read.
type textCommitReader struct {
	s    string
	size int
}

func (r *textCommitReader) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := min(len(r.s), r.size, len(b))
	copy(b, r.s[:n])
	r.s = r.s[n:]
	return n, nil
}

// TestGeneratedStreamTextCutAtCommit checks that a generated Go stream cuts a text at the last commit
// point as the engine does: for every chunking of the reader, the elements, the error and the
// compute units of each input of codegentest.StreamTextCommitGrammar equal the engine's.
func TestGeneratedStreamTextCutAtCommit(t *testing.T) {
	t.Parallel()
	goBin := skipRunsGoBin(t)
	codegentest.NeedsBudgetControls(t)
	g, err := parseTest(codegentest.StreamTextCommitGrammar)
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, codegentest.StreamTextCommitGrammar, check.Options{NoProjections: true})
	var want []string
	for _, in := range codegentest.StreamTextCommitInputs {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			for _, size := range codegentest.StreamTextCommitChunks {
				var units uint64
				n := 0
				err := prog.ParseStreamWith("main", &textCommitReader{s: in, size: size}, func(*value.Node) error {
					n++
					return nil
				}, engine.ParseOptions{Unit: unit, Expectations: true, ComputeReport: &units})
				want = append(want, fmt.Sprintf("%.20q %d %d: %d elements, units %d, error %v", in, unit, size, n, units, err))
			}
		}
	}
	dir := t.TempDir()
	inputs, _ := json.Marshal(codegentest.StreamTextCommitInputs)
	chunks, _ := json.Marshal(codegentest.StreamTextCommitChunks)
	write := func(name string, content []byte) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The parser imports the library, whose copy a mutant changes (codegentest.GoLibPatchEnv).
	codegentest.GeneratePackageIn(t, dir, "textcommit", "g", g, common.GenOptions{Start: "main", Stream: true})
	write("go.mod", []byte(codegentest.GoModAt("textcommit", codegentest.PatchedLib(t))))
	write("main.go", []byte(streamTextCommitConsumer))
	write("inputs.json", inputs)
	write("chunks.json", chunks)
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("generated %s\n engine    %s", got[k], want[k])
		}
	}
}

const streamTextCommitConsumer = `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"textcommit/g"
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
	var inputs []string
	var chunks []int
	for name, v := range map[string]any{"inputs.json": &inputs, "chunks.json": &chunks} {
		data, err := os.ReadFile(name)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			panic(err)
		}
	}
	for _, in := range inputs {
		for _, unit := range []g.PositionUnit{g.CodePoints, g.Bytes} {
			for _, size := range chunks {
				var units uint64
				n := 0
				err := g.ParseStream(&chunkReader{s: in, size: size}, func(*g.Node) error {
					n++
					return nil
				}, g.WithUnit(unit), sxirruntime.WithComputeReport(&units))
				fmt.Printf("%.20q %d %d: %d elements, units %d, error %v\n", in, unit, size, n, units, err)
			}
		}
	}
}
`
