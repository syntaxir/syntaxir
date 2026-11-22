package state_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
)

// TestIgnoreCaseMissingSpelling checks that #missing inserts the literal as written, without the
// suffix and without a change of case.
func TestIgnoreCaseMissingSpelling(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = "Sel"i #missing $$`)
	n, errs := requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 2}, 1)
	insertions := statetest.MissingNodes(t, n)
	if len(insertions) != 1 {
		t.Fatalf("missing nodes: %v", insertions)
	}
	if expected, _ := nodes.D(insertions[0]).Fields.Get("expected"); expected != "Sel" {
		t.Fatalf("inserted spelling %q, want Sel (errors %v)", expected, errs)
	}
}

// TestIgnoreCaseMissingSpellingBackends checks that the closure backend, the only one that recovers
// from missing tokens, inserts the spelling written in a case-insensitive literal for #missing, from
// the suffix and from the pragma, and that the expectation keeps the suffix. The bytecode backends
// reject missing-token recovery.
func TestIgnoreCaseMissingSpellingBackends(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = "Sel"i #missing $$`,
		`rule main = ("Sel" #missing $$) #ignorecase`,
		`rule main = "Sel" #missing #ignorecase $$`,
	} {
		prog := enginetest.MustCompile(t, src)
		for _, backend := range []engine.Backend{engine.Closure, engine.Default} {
			n, errs := requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 2, Backend: backend}, 1)
			insertions := statetest.MissingNodes(t, n)
			if len(insertions) != 1 {
				t.Fatalf("%s: backend %v: missing nodes: %v", src, backend, insertions)
			}
			if expected, _ := nodes.D(insertions[0]).Fields.Get("expected"); expected != "Sel" {
				t.Fatalf("%s: backend %v: inserted spelling %q, want Sel (errors %v)", src, backend, expected, errs)
			}
			if !strings.Contains(fmt.Sprint(errs), `"Sel"i`) {
				t.Fatalf("%s: backend %v: expectation lacks the suffix: %v", src, backend, errs)
			}
		}
	}
}

// TestIgnoreCaseMissingInPragmaScope checks that #missing inside the annotated expression inserts
// the literal as written, while the expectation of the same literal keeps the suffix.
func TestIgnoreCaseMissingInPragmaScope(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = ("Sel" #missing $$) #ignorecase`)
	n, errs := requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 2}, 1)
	insertions := statetest.MissingNodes(t, n)
	if len(insertions) != 1 {
		t.Fatalf("missing nodes: %v", insertions)
	}
	if expected, _ := nodes.D(insertions[0]).Fields.Get("expected"); expected != "Sel" {
		t.Fatalf("inserted spelling %q, want Sel (errors %v)", expected, errs)
	}
	if !strings.Contains(fmt.Sprint(errs), `"Sel"i`) {
		t.Fatalf("expectation lacks the suffix: %v", errs)
	}
}
