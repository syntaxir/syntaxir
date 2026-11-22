//go:build !sxir_budget_controls_reference

package state_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// leastCompute returns the smallest compute limit under which run succeeds.
func leastCompute(t *testing.T, run func(sxirruntime.Limits) error) uint64 {
	t.Helper()
	fits := func(b uint64) bool { return run(sxirruntime.Limits{Compute: b}) == nil }
	hi := uint64(1)
	for !fits(hi) {
		if hi > 1<<30 {
			t.Fatalf("the parse fails under every limit: %v", run(sxirruntime.Limits{}))
		}
		hi *= 2
	}
	lo := hi / 2
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if fits(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// familyCharges returns the least compute limit of every kind of parse of input by prog on the
// backend b, untraced and traced: each kind runs the family that its options select.
func familyCharges(t *testing.T, prog *engine.Program, b engine.Backend, input string, stream bool) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, traced := range []bool{false, true} {
		o := engine.ParseOptions{Expectations: true, Backend: b}
		suffix := ""
		if traced {
			o.Trace, suffix = func(engine.TraceEvent) {}, "+trace"
		}
		with := func(l sxirruntime.Limits) engine.ParseOptions { x := o; x.Limits = l; return x }
		if stream {
			out["stream"+suffix] = leastCompute(t, func(l sxirruntime.Limits) error {
				return prog.ParseStreamWith("main", strings.NewReader(input), func(*value.Node) error { return nil }, with(l))
			})
			continue
		}
		out["parse"+suffix] = leastCompute(t, func(l sxirruntime.Limits) error { _, err := prog.ParseWith("main", input, with(l)); return err })
		out["recognize"+suffix] = leastCompute(t, func(l sxirruntime.Limits) error {
			x := with(l)
			x.Recognize = true
			_, err := prog.ParseWith("main", input, x)
			return err
		})
		out["prefix"+suffix] = leastCompute(t, func(l sxirruntime.Limits) error { _, err := prog.ParsePrefixWith("main", input, with(l)); return err })
		out["document"+suffix] = leastCompute(t, func(l sxirruntime.Limits) error {
			d, err := prog.NewDocumentWith("main", input, engine.ParseOptions{Expectations: true, Backend: b})
			if err != nil {
				return err
			}
			_, err = d.ParseWith(with(l))
			return err
		})
	}
	return out
}

// TestClosureFamilyCharges pins the budget charge (the least compute limit) of every kind of
// closure parse, traced and untraced. A call charges the same units on the plain path and through
// Parser.Call (spec/runtime/parsing.md#cost), so each family charges the cost of the parse, and the
// numbers are those of the bytecode machines (TestVMFamilyCharges) except for the work that only a
// Document does. A traced parse charges more, for merging the expectations of the traced calls. A stream parse of the default build also charges for walking and clearing the capture
// frames it keeps for reuse at each commit, which the reference build of stream frames drops
// instead, so the stream numbers are those of the build.
func TestClosureFamilyCharges(t *testing.T) {
	t.Parallel()
	stream := map[string]uint64{"stream": 126, "stream+trace": 130}
	if !state.StreamFrameScratch {
		stream = map[string]uint64{"stream": 122, "stream+trace": 126}
	}
	cases := []struct {
		name, src string
		opts      entry.Options
		input     string
		stream    bool
		want      map[string]uint64
	}{
		{"plain calls", "rule main = item* $$\nrule item = \"a\"", entry.Options{}, "aaaa", false, map[string]uint64{"document": 24, "document+trace": 26, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34}},
		{"plain calls, no memo", "rule main = item* $$\nrule item = \"a\"", entry.Options{DisableMemo: true}, "aaaa", false, map[string]uint64{"document": 24, "document+trace": 26, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34}},
		{"recursion", "rule main = e $$\nrule e = \"(\" e \")\" / x\nrule x = \"x\"", entry.Options{DisableMemo: true}, "((x))", false, map[string]uint64{"document": 20, "document+trace": 24, "parse": 30, "parse+trace": 34, "prefix": 29, "prefix+trace": 33, "recognize": 30, "recognize+trace": 34}},
		{"captures", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", entry.Options{}, "aaa", false, map[string]uint64{"document": 23, "document+trace": 25, "parse": 29, "parse+trace": 31, "prefix": 28, "prefix+trace": 30, "recognize": 29, "recognize+trace": 31}},
		{"stream", codegentest.Records, entry.Options{}, "#records\na=1\nbc=22\n", true, stream},
	}
	for _, c := range cases {
		prog := enginetest.MustCompile(t, c.src, c.opts)
		got := familyCharges(t, prog, engine.Closure, c.input, c.stream)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: charges %v, want %v", c.name, got, c.want)
		}
	}
}
