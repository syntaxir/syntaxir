package engine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// The rest of the sparse memo tests drive the memo table directly and live in the state package;
// this one asks the engine for tracing and work counts on every backend.
func TestSparseMemoParseControls(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../../testdata/grammars/typescript/typescript.sxir")
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseTest(string(src))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, checkpkg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prog.NSeen <= 64 {
		t.Fatal("control no longer exercises sparse rule count")
	}
	for i, input := range []string{"const 日本語 = \"😀\";\nconst v = [1, 2].map(x => x + 1);\n", "function f( { return 1;", strings.Repeat("const 日本語 = 1 + 2;\n", 100), strings.Repeat("const a = 1;\n", 100) + "function f( { return 1;"} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			for _, recognize := range []bool{false, true} {
				checkTrace(t, prog, "main", input, ParseOptions{Unit: unit, Recognize: recognize})
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					spent, result := parseSpend(prog, "main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
					t.Logf("semantic=%d/%s/%s/%t result=%x spent=%x", i, backend, unit, recognize, sha256.Sum256([]byte(result)), sha256.Sum256([]byte(fmt.Sprintf("%+v", spent))))
				}
			}
		}
	}
}
