//go:build !sxir_budget_controls_reference

package state_test

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

var vmBackends = []engine.Backend{engine.Bytecode, engine.BytecodeIterative}

// TestVMFamilyCharges pins the budget charge (the least compute limit) of every kind of parse on
// both VMs, traced and untraced. A call charges the same units on the plain path and through
// Parser.Call (spec/runtime/parsing.md#cost), so each family charges the cost of the parse, the
// same on both machines. A traced parse charges more, for merging the expectations of the traced
// calls.
func TestVMFamilyCharges(t *testing.T) {
	t.Parallel()
	type charges = map[string]uint64
	// The stream parses of the default build also charge for walking and clearing the capture
	// frames they keep for reuse at each commit, which the reference build of stream frames drops.
	stream := map[engine.Backend]charges{engine.Bytecode: {"stream": 126, "stream+trace": 130}, engine.BytecodeIterative: {"stream": 126, "stream+trace": 130}}
	if !state.StreamFrameScratch {
		stream = map[engine.Backend]charges{engine.Bytecode: {"stream": 122, "stream+trace": 126}, engine.BytecodeIterative: {"stream": 122, "stream+trace": 126}}
	}
	cases := []struct {
		name, src string
		opts      entry.Options
		input     string
		stream    bool
		want      map[engine.Backend]charges
	}{
		{"plain calls", "rule main = item* $$\nrule item = \"a\"", entry.Options{}, "aaaa", false, map[engine.Backend]charges{
			engine.Bytecode:          {"document": 24, "document+trace": 26, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
			engine.BytecodeIterative: {"document": 24, "document+trace": 26, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
		}},
		{"plain calls, no memo", "rule main = item* $$\nrule item = \"a\"", entry.Options{DisableMemo: true}, "aaaa", false, map[engine.Backend]charges{
			engine.Bytecode:          {"document": 24, "document+trace": 26, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
			engine.BytecodeIterative: {"document": 24, "document+trace": 26, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
		}},
		{"recursion", "rule main = e $$\nrule e = \"(\" e \")\" / x\nrule x = \"x\"", entry.Options{DisableMemo: true}, "((x))", false, map[engine.Backend]charges{
			engine.Bytecode:          {"document": 21, "document+trace": 25, "parse": 31, "parse+trace": 35, "prefix": 30, "prefix+trace": 34, "recognize": 31, "recognize+trace": 35},
			engine.BytecodeIterative: {"document": 21, "document+trace": 25, "parse": 31, "parse+trace": 35, "prefix": 30, "prefix+trace": 34, "recognize": 31, "recognize+trace": 35},
		}},
		{"captures", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", entry.Options{}, "aaa", false, map[engine.Backend]charges{
			engine.Bytecode:          {"document": 23, "document+trace": 25, "parse": 29, "parse+trace": 31, "prefix": 28, "prefix+trace": 30, "recognize": 29, "recognize+trace": 31},
			engine.BytecodeIterative: {"document": 23, "document+trace": 25, "parse": 29, "parse+trace": 31, "prefix": 28, "prefix+trace": 30, "recognize": 29, "recognize+trace": 31},
		}},
		{"stream", codegentest.Records, entry.Options{}, "#records\na=1\nbc=22\n", true, stream},
	}
	for _, c := range cases {
		for _, b := range vmBackends {
			prog := enginetest.MustCompile(t, c.src, c.opts)
			got := familyCharges(t, prog, b, c.input, c.stream)
			if fmt.Sprint(got) != fmt.Sprint(c.want[b]) {
				t.Errorf("%s on %s: charges %v, want %v", c.name, b, got, c.want[b])
			}
		}
	}
}
