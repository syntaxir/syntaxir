//go:build !sxir_budget_controls_reference

package engine

import (
	"strings"
	"testing"
	"unsafe"

	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime"
)

// TestFlowRegistersCounted checks that the memory count holds the registers of the Flow bodies that
// run, by their capacity, on both VM models: when the Flow body of the value rule of the JSON grammar
// has extra more registers, a parse whose value calls nest depth deep counts at least the
// registers it adds, depth times extra, less the rounding of the register stack to its chunks.
func TestFlowRegistersCounted(t *testing.T) {
	const depth, extra = 64, 256
	_, narrow := jsonFlowPrograms(t, nil)
	_, wide := jsonFlowPrograms(t, func(f *bytecode.Flow) { f.Regs += extra }) // registers no instruction uses
	in := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	added := uint64(depth*extra) * uint64(unsafe.Sizeof(any(nil)))
	for _, b := range []Backend{Bytecode, BytecodeIterative} {
		least := func(prog *Program) uint64 {
			return minMemory(t, func(mem uint64) error {
				_, err := prog.ParseWith("", in, ParseOptions{Backend: b, Limits: runtime.Limits{Memory: mem}})
				return err
			})
		}
		n, w := least(narrow), least(wide)
		// A chunk of the register stack may end with registers that no body uses, so the count
		// grows by at least half of the registers added.
		if w < n || 2*(w-n) < added {
			t.Errorf("backend %v: the least memory is %d with %d registers more per call and %d without: the count holds less than half of the %d bytes they add", b, w, extra, n, added)
		}
	}
}
