package state_test

import (
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func TestSparseMemoInputSelection(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../testdata/grammars/typescript/typescript.sxir")
	if err != nil {
		t.Fatal(err)
	}
	g, err := enginetest.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"/*" + strings.Repeat("a", 1020) + "*/",
		"/*" + strings.Repeat("a", 1021) + "*/",
		"/*" + strings.Repeat("é", 512) + "*/",
		"/*" + strings.Repeat("a", 1020) + "*/",
	} {
		for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				p := state.NewParser(prog.Shared(), text, unit, true)
				p.DeferMemo = true
				if _, err := prog.Run(p, backend, "main"); err != nil {
					t.Fatal(err)
				}
				want := len([]rune(text)) > 1024
				if unit == sxirinput.Bytes {
					want = len(text) > 1024
				}
				want = want && state.SparseMemo
				if p.Memo.SparseFirstCalls() != want {
					t.Fatalf("%s/%s: sparse=%t want=%t", backend, unit, p.Memo.SparseFirstCalls(), want)
				}
				if _, directories, _ := p.Memo.FirstCallRecord(); !want && directories != 0 {
					t.Fatal("short input allocated sparse directory")
				}
			}
		}
	}
}
