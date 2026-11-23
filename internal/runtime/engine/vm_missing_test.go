package engine

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestBackendsMatchOnMissingWithoutInsertion checks that the bytecode VMs parse and recognize the
// grammars that use #missing as the closure engine does when the parse inserts no token: a literal
// with #missing is then strict.
func TestBackendsMatchOnMissingWithoutInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.InsertionCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			prog := mustCompile(t, c.Src)
			if !prog.HasMissing {
				t.Fatal("the grammar does not use #missing")
			}
			for _, in := range c.Inputs {
				checkBackends(t, prog, "main", in)
			}
		})
	}
}

// TestBackendsMatchOnInsertion checks that the bytecode VMs insert missing tokens as the closure
// engine does: the same results, errors, insertions and compute units, at several limits, in both
// units, with and without recognition and limit controls.
func TestBackendsMatchOnInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.InsertionCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			prog := mustCompile(t, c.Src)
			for _, in := range c.Inputs {
				for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					for _, limit := range []int{1, 2, 64} {
						for _, recognize := range []bool{false, true} {
							for _, report := range []bool{false, true} {
								run := func(b Backend) string {
									var units uint64
									o := ParseOptions{Backend: b, Unit: u, Expectations: true, MissingTokens: limit, Recognize: recognize}
									if report {
										o.ComputeReport = &units
									}
									n, err := prog.ParseWith("main", in, o)
									return fmt.Sprintf("%s units=%d", codegentest.ResultJSON(n, err), units)
								}
								want := run(Closure)
								for _, b := range vmBackends {
									if got := run(b); got != want {
										t.Errorf("input %q (%v) limit %d recognize %t report %t: %v differs from closure\n closure %s\n %v %s", in, u, limit, recognize, report, b, want, b, got)
									}
								}
							}
						}
					}
				}
			}
		})
	}
}
