package state_test

import (
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func TestSparseMemoRuleAndPageBoundaries(t *testing.T) {
	t.Parallel()
	for _, stride := range []int{63, 64, 65, 128, 175} {
		memo := state.MemoTable{Stride: stride}
		positions := []int{0, 1, 63, 64, 1023, 1024, 1025, 65535}
		for _, pos := range positions {
			for _, rule := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, rule) || sparseMemoRecord(&memo, pos, rule) {
					t.Fatalf("stride=%d pos=%d rule=%d: first/repeated mismatch", stride, pos, rule)
				}
			}
		}
		for i := len(positions) - 1; i >= 0; i-- {
			if sparseMemoRecord(&memo, positions[i], stride-1) {
				t.Fatal("returning to an earlier page lost its bit")
			}
		}
		if !memo.Reset() {
			t.Fatal("bounded memo table rejected reuse")
		}
		for _, dir := range memo.Pages {
			if dir.Last != nil {
				t.Fatal("cached page pointer survived reset")
			}
		}
		memo.Stride = stride
		for _, pos := range positions {
			for _, rule := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, rule) {
					t.Fatalf("stride=%d pos=%d rule=%d: stale bit survived reset", stride, pos, rule)
				}
			}
		}
		if state.SparseMemo && stride > 64 && len(memo.Seen) != 0 {
			t.Fatal("sparse path allocated the dense bitset")
		}
	}
}

func sparseMemoRecord(memo *state.MemoTable, pos, rule int) bool {
	if state.SparseMemo && memo.Stride > 64 {
		return memo.MarkSparseSeen(pos, rule)
	}
	return memo.MarkSeen(pos, rule)
}

func TestSparseMemoStrideChanges(t *testing.T) {
	t.Parallel()
	memo := state.MemoTable{}
	for _, stride := range []int{175, 65, 64, 128, 175} {
		memo.Stride = stride
		if !sparseMemoRecord(&memo, 1024, stride-1) || sparseMemoRecord(&memo, 1024, stride-1) {
			t.Fatal("stride change reused a stale bit")
		}
		if !memo.Reset() {
			t.Fatal("bounded table rejected reuse")
		}
	}
}

func TestSparseMemoResetBounds(t *testing.T) {
	t.Parallel()
	for _, memo := range []state.MemoTable{
		{Pages: make([]state.SeenRulePages, 0, state.MaxScratch/5+1)},
		{Pages: make([]state.SeenRulePages, 0, state.MaxScratch/5), BitPages: state.Arena[[16]uint64]{Chunks: []*[64][16]uint64{new([64][16]uint64)}}},
		{Pages: []state.SeenRulePages{{Pages: make([]*[16]uint64, 0, state.MaxScratch+1)}}},
	} {
		if memo.Reset() {
			t.Fatal("oversized directory retained")
		}
	}
	memo := state.MemoTable{Stride: 65}
	for i := 0; i <= state.MaxScratch/16; i++ {
		memo.MarkSparseSeen(i*1024, 0)
	}
	if memo.Reset() {
		t.Fatal("oversized pages retained")
	}
}

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
	prog, err := entry.Compile(g, compile.Options{})
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
				if p.Memo.Sparse != want {
					t.Fatalf("%s/%s: sparse=%t want=%t", backend, unit, p.Memo.Sparse, want)
				}
				if !want && len(p.Memo.Pages) != 0 {
					t.Fatal("short input allocated sparse directory")
				}
			}
		}
	}
}
