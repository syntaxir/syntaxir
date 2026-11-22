//go:build !sxir_budget_controls_reference

package state_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
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

// TestClosureFamilyCharges pins the compute units (the least compute limit) of every kind of
// closure parse, traced and untraced. A call counts the same units on the plain path and through
// Parser.Call, and the work that only a Document, a trace or a stream commit does is no event
// (spec/runtime/parsing.md#compute-units), so every family of a grammar and input counts the same
// units, in either build of the stream frames.
func TestClosureFamilyCharges(t *testing.T) {
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
		prog := enginetest.MustCompile(t, c.src, c.opts)
		got := familyCharges(t, prog, engine.Closure, c.input, c.stream)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: charges %v, want %v", c.name, got, c.want)
		}
	}
}
