//go:build unix && !sxir_budget_controls_reference

package engine

import (
	"bufio"
	"errors"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"syntaxir.dev/sxir/internal/runtime/limits"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// unreadableText returns a string of n bytes over a mapping that faults when read, so an entry point
// that read it before checking its length would crash the test rather than pass. The mapping takes
// address space only.
func unreadableText(t *testing.T, n int) string {
	t.Helper()
	b, err := syscall.Mmap(-1, 0, n, syscall.PROT_NONE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Skipf("no mapping of %d bytes: %v", n, err)
	}
	t.Cleanup(func() { _ = syscall.Munmap(b) })
	return unsafe.String(unsafe.SliceData(b), n)
}

// No input is longer than 2^31−1 units, on every backend and entry point: zero, Unlimited and any
// larger Input select that bound, an input of 2^31 bytes fails with it before any work, and one of
// 2^31−1 bytes passes the check. The inputs are never read.
func TestMaxInputBound(t *testing.T) {
	t.Parallel()
	if limits.MaxInput != 1<<31-1 {
		t.Fatalf("MaxInput %d", limits.MaxInput)
	}
	const max = limits.MaxInput
	text := unreadableText(t, max+1)
	for _, in := range []uint64{0, runtime.Unlimited, max + 1, 1 << 40} {
		lim, _ := state.ResolveLimits(runtime.Limits{Input: in}, max, false)
		if lim.Input != max || state.ResolveInput(in) != max {
			t.Errorf("Input %d resolves to %d", in, lim.Input)
		}
		if err := state.CheckInput(lim, text[:max], input.Bytes); err != nil {
			t.Errorf("Input %d: 2^31−1 bytes: %v", in, err)
		}
		if err := state.CheckInput(lim, text[:max], input.CodePoints); err != nil {
			t.Errorf("Input %d: 2^31−1 bytes in code points: %v", in, err)
		}
		wantInputLimit(t, state.CheckInputUnits(lim, max+1), max, max+1)
	}
	prog := mustCompile(t, maxInputGrammar)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		if o.Unit != input.Bytes {
			t.Skip("counting code points reads the input")
		}
		for _, in := range []uint64{0, runtime.Unlimited, 1 << 40} {
			o.Limits = runtime.Limits{Input: in}
			_, err := prog.ParseWith("main", text, o)
			wantInputLimit(t, err, max, max+1)
			_, err = prog.ParsePrefixWith("main", text, o)
			wantInputLimit(t, err, max, max+1)
			_, err = prog.NewDocumentWith("main", text, o)
			wantInputLimit(t, err, max, max+1)
			r := o
			r.Recognize = true
			_, err = prog.ParseWith("main", text, r)
			wantInputLimit(t, err, max, max+1)
		}
	})
}

// A stream fails when the parser asks for the unit after the 2^31−1st and the stream has one, and
// completes when it ends there. The stream starts just below the bound instead of reading 2 GiB.
func TestMaxInputBoundStream(t *testing.T) {
	t.Parallel()
	stream := func(rest string) *state.Input {
		return &state.Input{Unit: input.Bytes, Reader: bufio.NewReader(strings.NewReader(rest)), Base: limits.MaxInput - 1, MaxInput: int(state.ResolveInput(0))}
	}
	fill := func(in *state.Input, n int) (ok bool, err error) {
		defer func() {
			if x := recover(); x != nil {
				f, isFatal := x.(state.Fatal)
				if !isFatal {
					panic(x)
				}
				err = f.Err
			}
		}()
		return in.Fill(n), nil
	}
	if ok, err := fill(stream("ab"), limits.MaxInput); !ok || err != nil {
		t.Errorf("unit 2^31−1: %v %v", ok, err)
	}
	_, err := fill(stream("ab"), limits.MaxInput+1)
	var le *runtime.LimitError
	if !errors.As(err, &le) || le.Kind != runtime.LimitInput || le.Limit != limits.MaxInput || le.Requested != limits.MaxInput+1 {
		t.Errorf("unit 2^31: %v", err)
	}
	if ok, err := fill(stream("a"), limits.MaxInput+1); ok || err != nil {
		t.Errorf("a stream of 2^31−1 units: %v %v", ok, err)
	}
}
