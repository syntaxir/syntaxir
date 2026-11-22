package check_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	fcheck "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

func TestInferenceDependencyChains(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{45, 50, 128, 512} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/reversed=%v", depth, reverse), func(t *testing.T) {
				g, err := enginetest.Parse(codegentest.InferredChain(depth, reverse, ":node[string]"))
				if err != nil {
					t.Fatal(err)
				}
				_, err = entry.Compile(g, entry.Options{})
				if err == nil || !strings.Contains(err.Error(), "produces P, which is not assignable to node[string]") {
					t.Errorf("long-chain type mismatch: %v", err)
				}
				p := enginetest.MustCompile(t, codegentest.InferredChain(depth, reverse, ""))
				for i := range depth {
					if got, ok := inferredType(p, fmt.Sprint("r", i)); !ok || got != "P" {
						t.Fatalf("r%d type = %q, %v; want P", i, got, ok)
					}
				}
				if got, ok := inferredType(p, "main"); !ok || got != "P" {
					t.Errorf("main type = %q, %v; want P", got, ok)
				}
			})
		}
	}
}

func TestInferenceKeepsIndependentTypes(t *testing.T) {
	t.Parallel()
	src := codegentest.InferredChain(128, false, "") + `rule growing = xs:growing? => list($xs)
rule constant = "(" growing ")" => P{}`
	p := enginetest.MustCompile(t, src)
	for _, name := range []string{"main", "r0", "r127", "constant"} {
		if got, _ := inferredType(p, name); got != "P" {
			t.Errorf("%s type = %s; want P", name, got)
		}
	}
	// A recursive type refers to the rule by name.
	if got, _ := inferredType(p, "growing"); got != "[]*growing" {
		t.Errorf("growing type = %s; want []*growing", got)
	}
}

func BenchmarkInferenceDependencies(b *testing.B) {
	for _, c := range []struct{ name, source string }{
		{"caller-first-64", codegentest.InferredChain(64, false, "")},
		{"callee-first-64", codegentest.InferredChain(64, true, "")},
		{"caller-first-512", codegentest.InferredChain(512, false, "")},
		{"growing-list", `rule main = xs:main? => list($xs)`},
	} {
		b.Run(c.name, func(b *testing.B) {
			g, err := enginetest.Parse(c.source)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := entry.Compile(g, entry.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestInferenceChainFieldMismatch(t *testing.T) {
	t.Parallel()
	src := "type Holder = { Value node[string] }\n" + strings.Replace(codegentest.InferredChain(128, false, ""), "rule main = r0", "rule main = r0:r0 => Holder{Value: $r0}", 1)
	g, err := enginetest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = entry.Compile(g, entry.Options{})
	if err == nil || !strings.Contains(err.Error(), "cannot use P as node[string] in field Value of Holder") {
		t.Errorf("long-chain field mismatch: %v", err)
	}
}

// inferredType returns the value type the checker infers for the rule name, with the rules it
// refers to replaced by their values at the top level and in unions and optional types. RuleType
// refers to a rule without a declared type by name instead.
func inferredType(p *engine.Program, name string) (string, bool) {
	t, ok := entry.Checked(p).Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &fcheck.ValueInference{Checker: entry.Checked(p).Checker(), Out: entry.Checked(p).Typed.Values}
	return v.Expand(t).String(), true
}

// captureType returns the type of the capture cap in the value of the rule name, after checkTypes:
// the union of the types of all captures of that name.
func captureType(p *engine.Program, name, cap string) (string, bool) {
	t, ok := entry.Checked(p).Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &fcheck.ValueInference{Checker: entry.Checked(p).Checker(), Out: entry.Checked(p).Typed.Values}
	n, ok := v.Expand(t).(types.NodeTy)
	if !ok {
		return "", false
	}
	ct, ok := fcheck.CapType(n.Caps, cap)
	if !ok {
		return "", false
	}
	return ct.String(), true
}
