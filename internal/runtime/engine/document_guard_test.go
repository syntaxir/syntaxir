//go:build !sxir_budget_controls_reference

package engine

import (
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
)

// TestDocumentGuardsKeepMemoWithoutLimits checks that a Document's statistics do not depend on its
// limits: a choice in a Document parse skips no alternative whose evaluation would write a memo
// entry, with a compute control or without one, so a parse with no limit evaluates the rules that
// one under a limit evaluates.
func TestDocumentGuardsKeepMemoWithoutLimits(t *testing.T) {
	const src = "@start\nrule main = (a / b)* $$\nrule a = x \"1\"\nrule b = x \"2\"\nrule x = \"q\"\n"
	const text = "q1q2q1q2"
	prog := mustCompile(t, src)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		stats := func(lim runtime.Limits) runtime.ParseStats {
			d, err := prog.NewDocumentWith("main", text, ParseOptions{Backend: b, Limits: lim})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if err := d.Edit(4, 4, "z"); err != nil {
				t.Fatal(err)
			}
			d.Parse()
			return d.Stats()
		}
		none, limited := stats(runtime.Limits{Compute: runtime.Unlimited}), stats(runtime.Limits{Compute: 1 << 40})
		if none != limited {
			t.Errorf("backend %v: statistics %+v without a limit, %+v under one", b, none, limited)
		}
	}
}
