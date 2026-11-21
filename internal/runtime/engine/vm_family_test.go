//go:build !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

var vmBackends = []Backend{Bytecode, BytecodeIterative}

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
	stream := map[Backend]charges{Bytecode: {"stream": 126, "stream+trace": 130}, BytecodeIterative: {"stream": 126, "stream+trace": 130}}
	if !state.StreamFrameScratch {
		stream = map[Backend]charges{Bytecode: {"stream": 122, "stream+trace": 126}, BytecodeIterative: {"stream": 122, "stream+trace": 126}}
	}
	cases := []struct {
		name, src string
		opts      compile.Options
		input     string
		stream    bool
		want      map[Backend]charges
	}{
		{"plain calls", "rule main = item* $$\nrule item = \"a\"", compile.Options{}, "aaaa", false, map[Backend]charges{
			Bytecode:          {"document": 38, "document+trace": 40, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
			BytecodeIterative: {"document": 38, "document+trace": 40, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
		}},
		{"plain calls, no memo", "rule main = item* $$\nrule item = \"a\"", compile.Options{DisableMemo: true}, "aaaa", false, map[Backend]charges{
			Bytecode:          {"document": 34, "document+trace": 36, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
			BytecodeIterative: {"document": 34, "document+trace": 36, "parse": 32, "parse+trace": 34, "prefix": 31, "prefix+trace": 33, "recognize": 32, "recognize+trace": 34},
		}},
		{"recursion", "rule main = e $$\nrule e = \"(\" e \")\" / x\nrule x = \"x\"", compile.Options{DisableMemo: true}, "((x))", false, map[Backend]charges{
			Bytecode:          {"document": 21, "document+trace": 25, "parse": 31, "parse+trace": 35, "prefix": 30, "prefix+trace": 34, "recognize": 31, "recognize+trace": 35},
			BytecodeIterative: {"document": 21, "document+trace": 25, "parse": 31, "parse+trace": 35, "prefix": 30, "prefix+trace": 34, "recognize": 31, "recognize+trace": 35},
		}},
		{"captures", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", compile.Options{}, "aaa", false, map[Backend]charges{
			Bytecode:          {"document": 33, "document+trace": 35, "parse": 29, "parse+trace": 31, "prefix": 28, "prefix+trace": 30, "recognize": 29, "recognize+trace": 31},
			BytecodeIterative: {"document": 33, "document+trace": 35, "parse": 29, "parse+trace": 31, "prefix": 28, "prefix+trace": 30, "recognize": 29, "recognize+trace": 31},
		}},
		{"stream", codegentest.Records, compile.Options{}, "#records\na=1\nbc=22\n", true, stream},
	}
	for _, c := range cases {
		for _, b := range vmBackends {
			prog := mustCompile(t, c.src, c.opts)
			got := familyCharges(t, prog, b, c.input, c.stream)
			if fmt.Sprint(got) != fmt.Sprint(c.want[b]) {
				t.Errorf("%s on %s: charges %v, want %v", c.name, b, got, c.want[b])
			}
		}
	}
}

// TestVMFamiliesMatchOnCorpus checks that the two families of each VM give the same results,
// errors, expectations, recovered errors, positions and statistics on every grammar and input of
// the corpus, when no tracer observes the calls.
func TestVMFamiliesMatchOnCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.GenCorpus(t) {
		t.Run(c.Name, func(t *testing.T) {
			for _, o := range []compile.Options{{}, {DisableMemo: true}} {
				prog := mustCompile(t, c.Src, o)
				if prog.HasMissing {
					continue
				}
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
