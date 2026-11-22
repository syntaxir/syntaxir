//go:build !sxir_budget_controls_reference

package state_test

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

var vmBackends = []engine.Backend{engine.Bytecode, engine.BytecodeIterative}

// TestVMFamilyCharges pins the compute units (the least compute limit) of every kind of parse on
// both VMs, traced and untraced. A call counts the same units on the plain path and through
// Parser.Call, and the work that only a Document, a trace or a stream commit does is no event
// (spec/runtime/parsing.md#compute-units), so every family of a grammar and input counts the same
// units, on both machines, in either build of the stream frames, and as the closure engine counts
// them (TestClosureFamilyCharges).
func TestVMFamilyCharges(t *testing.T) {
	t.Parallel()
	every := func(units uint64) map[string]uint64 {
		out := map[string]uint64{}
		for _, k := range []string{"document", "parse", "prefix", "recognize"} {
			out[k], out[k+"+trace"] = units, units
		}
		return out
	}
	cases := []struct {
		name, src string
		opts      check.Options
		input     string
		stream    bool
		want      map[string]uint64
	}{
		{"plain calls", "rule main = item* $$\nrule item = \"a\"", check.Options{}, "aaaa", false, every(10)},
		{"plain calls, no memo", "rule main = item* $$\nrule item = \"a\"", check.Options{DisableMemo: true}, "aaaa", false, every(10)},
		{"recursion", "rule main = e $$\nrule e = \"(\" e \")\" / x\nrule x = \"x\"", check.Options{DisableMemo: true}, "((x))", false, every(10)},
		// len of each one-byte text counts its byte.
		{"captures", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", check.Options{}, "aaa", false, every(11)},
		{"stream", codegentest.Records, check.Options{}, "#records\na=1\nbc=22\n", true, map[string]uint64{"stream": 24, "stream+trace": 24}},
	}
	for _, c := range cases {
		for _, b := range vmBackends {
			prog := enginetest.MustCompile(t, c.src, c.opts)
			got := familyCharges(t, prog, b, c.input, c.stream)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("%s on %s: charges %v, want %v", c.name, b, got, c.want)
			}
		}
	}
}
