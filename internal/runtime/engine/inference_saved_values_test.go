package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// A grammar whose types the checker infers through a long chain of rules runs with the same values
// after it is saved to a compiled grammar and loaded again, in every mode that builds values.
func TestInferenceChainSavedValues(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, codegentest.InferredChain(64, false, ""))
	for _, mode := range valueModes {
		q := saveLoad(t, p, "main", mode)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if backend == Closure {
				_, err := q.ParseWith("main", "a", servedBy(mode, ParseOptions{Backend: backend}))
				wantClosureRefused(t, err)
				continue
			}
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				n, err := q.ParseWith("main", "a", servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
				if err != nil || n.String() != "P{}" {
					t.Errorf("saved %s/%s result: %v, %v", backend, unit, n, err)
				}
			}
		}
	}
}
