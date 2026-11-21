package enginetest

import (
	"errors"
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// CheckBackends checks that every backend of prog produces the same result for input from start
// (value, positions, errors), in both position units, that recognition agrees with the parse, and
// that an unlimited Compute budget gives the result of the default budget.
func CheckBackends(t *testing.T, prog *engine.Program, start, input string) {
	t.Helper()
	for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		n1, err1 := prog.ParseWith(start, input, engine.ParseOptions{Expectations: true, Unit: u})
		want := codegentest.ResultJSON(n1, err1)
		checkRecognize(t, prog, start, input, u, err1)
		// The default budget runs the controlled form; Unlimited runs the uncontrolled one, which must
		// give the same result on inputs within the default.
		if n0, err0 := prog.ParseWith(start, input, engine.ParseOptions{Expectations: true, Unit: u, Limits: runtime.Limits{Compute: runtime.Unlimited}}); !errors.Is(err1, runtime.ErrLimit) {
			if got := codegentest.ResultJSON(n0, err0); got != want {
				t.Errorf("input %q (unit %v): Unlimited differs from the default budget\n default   %s\n unlimited %s", input, u, want, got)
			}
		}
		for _, b := range []engine.Backend{engine.Bytecode, engine.BytecodeIterative} {
			n2, err2 := prog.ParseWith(start, input, engine.ParseOptions{Expectations: true, Unit: u, Backend: b})
			if got := codegentest.ResultJSON(n2, err2); got != want {
				t.Errorf("input %q (unit %v): %v differs from closure\n closure %s\n %v %s", input, u, b, want, b, got)
			}
		}
	}
}

// checkRecognize checks that a parse that builds no tree (recognition) returns the same syntax
// error as an ordinary parse (or succeeds if it does). Results are not compared when the
// ordinary parse fails with a runtime error in an action (recognition does not evaluate
// actions).
func checkRecognize(t *testing.T, prog *engine.Program, start, input string, u sxirinput.PositionUnit, want error) {
	t.Helper()
	switch want.(type) {
	case nil, *value.SyntaxError, value.SyntaxErrors:
	default:
		return
	}
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		n, err := prog.ParseWith(start, input, engine.ParseOptions{Expectations: true, Unit: u, Backend: b, Recognize: true})
		if n != nil || fmt.Sprint(err) != fmt.Sprint(want) {
			t.Errorf("input %q (unit %v, %v): recognition gives %v, %v; want %v", input, u, b, n, err, want)
		}
	}
}
