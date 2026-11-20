//go:build zp_long

package engine_test

import "testing"

// TestTracedChargeMutantsFail is the negative control of TestTracedUnitsEqualUntraced: each mutant
// charges a traced call one unit more than an untraced one, on the backends that call through the
// printed call protocol and on the iterative VM, which traces a call in a frame of its own.
func TestTracedChargeMutantsFail(t *testing.T) {
	const test = "^TestTracedUnitsEqualUntraced$"
	runPrintedMutants(t, "internal/runtime/state/call_exec.go", []printedMutant{
		{"a traced call charges its unit twice",
			[]printedEdit{{"\t\tv, ok = p.callDoc(r, min)\n\t\treturn v, ok\n\t}\n\tif p.Tr != nil && p.tracing() {\n",
				"\t\tv, ok = p.callDoc(r, min)\n\t\treturn v, ok\n\t}\n\tif p.Tr != nil && p.tracing() {\n\t\tp.Spend()\n"}},
			test, []string{"."}},
	})
	runPrintedMutants(t, "internal/runtime/vm/ivm.go", []printedMutant{
		{"the iterative VM charges a traced call in its trace frame",
			[]printedEdit{{"\t\tp.TraceEnter(f.r, f.min)\n", "\t\tp.Spend()\n\t\tp.TraceEnter(f.r, f.min)\n"}},
			test, []string{"."}},
	})
}
