//go:build !sxir_budget_controls_reference

package engine

import (
	"io"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestVMFamiliesMatchOnCorpus checks that the two families of each VM give the same results,
// errors, expectations, recovered errors, positions and statistics on every grammar and input of
// the corpus, when no tracer observes the calls.
func TestVMFamiliesMatchOnCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.GenCorpus(t) {
		t.Run(c.Name, func(t *testing.T) {
			for _, o := range []checkpkg.Options{{}, {DisableMemo: true}} {
				prog := mustCompile(t, c.Src, o)
				for _, b := range vmBackends {
					for _, in := range c.Inputs {
						for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
							x, xs := parseInFamily(prog, b, in, u, state.Inlined)
							y, ys := parseInFamily(prog, b, in, u, state.CallPreserving)
							if x != y || xs != ys {
								t.Errorf("%s memo off=%t input %q (%v): families differ\n inlined         %s %+v\n call-preserving %s %+v", b, o.DisableMemo, in, u, x, xs, y, ys)
							}
						}
					}
				}
			}
		})
	}
}

// TestVMFamiliesPreparedOnFirstUse checks that a program prepares the VM program of each family
// only when a parse first runs it: ordinary parses the inlined one, traced and Document parses the
// call-preserving one, and that the families run distinct rules (TestVMFamilyCharges checks which
// family calls plain rules directly).
func TestVMFamiliesPreparedOnFirstUse(t *testing.T) {
	t.Parallel()
	const src = "rule main = item* $$\nrule item = \"a\""
	for _, b := range vmBackends {
		prepared := func(prog *Program) [2]bool {
			v := &prog.vms[vmIndex(b)]
			return [2]bool{v[state.Inlined].vm != nil, v[state.CallPreserving].vm != nil}
		}
		o := ParseOptions{Backend: b}
		prog := mustCompile(t, src)
		if _, err := prog.ParseWith("main", "aa", o); err != nil {
			t.Fatal(err)
		}
		if got := prepared(prog); got != [2]bool{true, false} {
			t.Fatalf("%s: after a parse: prepared %v, want the inlined family", b, got)
		}
		traced := o
		traced.Trace = func(TraceEvent) {}
		if _, err := prog.ParseWith("main", "aa", traced); err != nil {
			t.Fatal(err)
		}
		if got := prepared(prog); got != [2]bool{true, true} {
			t.Fatalf("%s: after a traced parse: prepared %v, want both families", b, got)
		}
		in, err := prog.rule(b, "item", state.Inlined, nil)
		if err != nil {
			t.Fatal(err)
		}
		cp, err := prog.rule(b, "item", state.CallPreserving, nil)
		if err != nil {
			t.Fatal(err)
		}
		if in == cp || in.ID != cp.ID || !in.Plain || !cp.Plain {
			t.Fatalf("%s: item: inlined %p (ID %d, plain %t), call-preserving %p (ID %d, plain %t)", b, in, in.ID, in.Plain, cp, cp.ID, cp.Plain)
		}

		doc := mustCompile(t, src)
		d, err := doc.NewDocumentWith("main", "aa", o)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(); err != nil {
			t.Fatal(err)
		}
		if got := prepared(doc); got != [2]bool{false, true} {
			t.Fatalf("%s: after a Document parse: prepared %v, want the call-preserving family", b, got)
		}

		stream := mustCompile(t, codegentest.Records)
		if err := stream.ParseStreamWith("main", strings.NewReader("#records\na=1\n"), func(*value.Node) error { return nil }, traced); err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if got := prepared(stream); got != [2]bool{false, true} {
			t.Fatalf("%s: after a traced stream parse: prepared %v, want the call-preserving family", b, got)
		}
	}
}
