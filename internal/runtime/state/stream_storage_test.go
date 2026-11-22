package state_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestStreamPrefixStorageIsolation(t *testing.T) {
	t.Parallel()
	if !state.StreamPrefixIsolation {
		t.Skip("reference build shares prefix storage")
	}
	p := state.NewStreamParser(enginetest.MustCompile(t, `rule main = "a"`).Shared(), strings.NewReader("a"), sxirinput.CodePoints)
	root := p.NewFrame(1)
	p.Frame = root
	prefix := p.NewNode(nodes.Data{Text: "prefix"})
	root.Vals[0] = prefix
	if p.NodeSlab != nil || p.PtrSlab != nil || p.FrameSlab != nil {
		t.Fatal("prefix wasted or shared a bulk allocation")
	}
	p.StartStreamChunks()
	next := p.NewNode(nodes.Data{Text: "element"})
	element := p.NewFrame(1)
	if next == prefix || &element.Vals[0] == &root.Vals[0] || element == root {
		t.Fatal("element allocation still shares live prefix chunks")
	}
	if p.Frame != root || root.Vals[0] != prefix || nodes.D(prefix).Text != "prefix" {
		t.Fatal("prefix isolation changed live root capture")
	}
}

// Large prefixes must switch to slabs instead of allocating every object.
func TestStreamPrefixAllocationBudget(t *testing.T) {
	t.Parallel()
	if !state.StreamPrefixIsolation {
		t.Skip("reference build uses slabs")
	}
	p := state.NewStreamParser(enginetest.MustCompile(t, `rule main = "a"`).Shared(), strings.NewReader("a"), sxirinput.CodePoints)
	for range state.NodeChunk/8 + 4 {
		p.NewNode(nodes.Data{Text: "prefix"})
	}
	if len(p.NodeSlab) == 0 || p.NodeChunks != 1 {
		t.Fatal("large prefix did not switch to bounded slab allocation")
	}
	p.StartStreamChunks()
	if p.NodeChunks != 0 || len(p.NodeSlab) != 0 {
		t.Fatal("stream boundary did not seal large prefix storage")
	}
	p.NewNode(nodes.Data{Text: "element"})
	if p.NodeChunks != 1 || len(p.NodeSlab) != state.NodeChunk-1 {
		t.Fatal("elements did not resume normal slab allocation")
	}
}

func TestStreamPrefixMemoReuse(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = &header h:header (x:"x" #atomic)* #stream $$ {text($h) == "#head\n"}
rule header = "#head\n" #atomic`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := engine.ParseOptions{Backend: backend, Unit: unit}
				input := "#head\n" + strings.Repeat("x", 1300)
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader(input), func(n *value.Node) error {
					if nodes.D(n.Field("x").(*value.Node)).Text != "x" {
						t.Fatal("emitted capture changed")
					}
					count++
					return nil
				}, opts)
				if err != nil || count != 1300 {
					t.Fatalf("emitted %d: %v", count, err)
				}
				if err := prog.ParseStreamWith("main", strings.NewReader(input), nil, opts); err != nil {
					t.Fatalf("nil callback: %v", err)
				}
			})
		}
	}
}

func TestStreamPrefixLeftRecursiveCaptureSurvivesCommit(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = h:head #atomic "x"* #stream {text($h) == "aa"} $$
rule head = head "a" / "a" #atomic`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader("aaxx"), func(*value.Node) error { count++; return nil }, engine.ParseOptions{Backend: backend, Unit: unit})
				if err != nil || count != 2 {
					t.Fatalf("LR capture lost after stream commits: count=%d, %v", count, err)
				}
			})
		}
	}
}
