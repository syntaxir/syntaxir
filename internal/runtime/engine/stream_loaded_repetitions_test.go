package engine

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// Prefix captures and emitted values must survive isolation for both compact
// and wide repetition instructions, including bytecode loaded from a compiled grammar.
func TestStreamPrefixLoadedRepetitions(t *testing.T) {
	t.Parallel()
	bounds := []string{"#repeat(0)", "#repeat(2)", "*"}
	if strconv.IntSize == 64 {
		bounds = append(bounds, "#repeat(0, 4294967296)")
	}
	for _, bound := range bounds {
		text, count := "#head\nxx", 2
		if bound == "#repeat(0)" {
			text, count = "#head\n", 0
		}
		source := `rule main = h:"#head\n" #atomic (x:"x" #atomic)` + bound + ` #stream $$ {text($h) == "#head\n"}`
		prog := mustCompile(t, source)
		for _, mode := range valueModes {
			saved := saveLoad(t, prog, "main", mode)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if backend == Closure {
					err := saved.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { return nil }, servedBy(mode, ParseOptions{Backend: backend}))
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", bound, mode, backend, unit), func(t *testing.T) {
						var retained []*value.Node
						err := saved.ParseStreamWith("main", strings.NewReader(text), func(n *value.Node) error { retained = append(retained, n); return nil }, servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
						if err != nil || len(retained) != count {
							t.Fatalf("got %d rows: %v", len(retained), err)
						}
						for _, n := range retained {
							if nodes.D(n.Field("x").(*value.Node)).Text != "x" {
								t.Fatal("retained capture changed")
							}
						}
					})
				}
			}
		}
	}
}
