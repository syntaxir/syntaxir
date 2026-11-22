package bytecode_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestModuleCompilesCorpus checks that every test grammar compiles to bytecode and that all
// branch targets of its Flow bodies are within the body.
func TestModuleCompilesCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.GenCorpus(t) {
		m := enginetest.MustCompile(t, c.Src).Module()
		for rule, f := range m.Flows {
			for i, in := range f.Code {
				if s, _ := in.Op.Shape(); s.Target && (in.Target < 0 || in.Target >= len(f.Code)) {
					t.Errorf("%s: rule %d: instruction %d: jump to %d", c.Name, rule, i, in.Target)
				}
			}
		}
		if !strings.Contains(m.Disassemble(), "RETURN") {
			t.Errorf("%s: no RETURN", c.Name)
		}
	}
}

// TestEmittedFlowsHaveBoundedCycles checks that the compiler emits no Flow body with a cycle that
// may repeat without end under a finite compute limit (Flow.UnboundedCycle), which the loader
// rejects: every body of the rules, of their streams and of the lines and skips of the Operators
// Rules, for the test grammars and the grammars of testdata/grammars. Some bodies of each corpus
// must loop, so that the check is not vacuous.
func TestEmittedFlowsHaveBoundedCycles(t *testing.T) {
	t.Parallel()
	srcs := map[string]string{}
	for _, c := range codegentest.GenCorpus(t) {
		srcs[c.Name] = c.Src
	}
	paths, err := filepath.Glob("../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("testdata grammars: %v", err)
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		srcs[path] = string(b)
	}
	bodies, loops := 0, 0
	for name, src := range srcs {
		m := enginetest.MustCompile(t, src).Module()
		for what, f := range moduleFlows(m) {
			bodies++
			for pc, in := range f.Code {
				if s, _ := in.Op.Shape(); s.Target && in.Target <= pc {
					loops++
					break
				}
			}
			if pc, ok := f.UnboundedCycle(); ok {
				t.Errorf("%s: %s: %v at %d is on a cycle that charges no step\n%s", name, what, f.Code[pc].Op, pc, m.Disassemble())
			}
		}
	}
	if loops == 0 {
		t.Fatalf("none of %d bodies loops", bodies)
	}
	t.Logf("%d bodies, %d with a jump back", bodies, loops)
}

// moduleFlows returns every Flow body of m, keyed by what it is the body of.
func moduleFlows(m *bytecode.Module) map[string]*bytecode.Flow {
	out := map[string]*bytecode.Flow{}
	for rule, f := range m.Flows {
		out[fmt.Sprintf("flow body of rule %d", rule)] = f
	}
	for rule, f := range m.Streams {
		out[fmt.Sprintf("stream body of rule %d", rule)] = f
	}
	for i, p := range m.OperatorsRules {
		if p.SkipFlow != nil {
			out[fmt.Sprintf("skip of Operators Rule %d", i)] = p.SkipFlow
		}
		lines := slices.Clone(p.Operands)
		for _, o := range slices.Concat(p.Prefix, p.InfixPostfix) {
			lines = append(lines, o.Line)
		}
		for j, l := range lines {
			if l.Flow != nil {
				out[fmt.Sprintf("line %d of Operators Rule %d", j, i)] = l.Flow
			}
		}
	}
	return out
}
