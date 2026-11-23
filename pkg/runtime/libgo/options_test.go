package libgo

import (
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestResolveRuntimeOptions checks that the library takes the options of package runtime, the one
// definition of them: each option sets what the library reads, in order, and a negative limit of
// missing tokens is an error.
func TestResolveRuntimeOptions(t *testing.T) {
	g := NewGrammar(100)
	o, err := ResolveOptions(g, []sxirruntime.ParseOption{
		sxirruntime.WithUnit(sxirinput.Bytes),
		sxirruntime.WithLimits(sxirruntime.Limits{Depth: 7, Compute: 5}),
		sxirruntime.WithLimits(sxirruntime.Limits{Compute: 9}),
		sxirruntime.WithMissingTokens(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.unit != sxirinput.Bytes || o.limits.Compute != 9 || o.limits.Depth != 7 || o.maxDepth != 7 {
		t.Errorf("resolved unit %v, limits %+v, depth %d", o.unit, o.limits, o.maxDepth)
	}
	if o, err := ResolveOptions(g, []sxirruntime.ParseOption{sxirruntime.WithMissingTokens(3)}); err != nil || o.missingTokens != 3 {
		t.Errorf("WithMissingTokens(3): limit %d, %v", o.missingTokens, err)
	}
	if _, err := ResolveOptions(g, []sxirruntime.ParseOption{sxirruntime.WithMissingTokens(-1)}); err == nil || !strings.Contains(err.Error(), "missing token limit") {
		t.Errorf("WithMissingTokens(-1): %v", err)
	}
	// The diagnostic options belong to the entry points of a stream.
	diag := []sxirruntime.ParseOption{sxirruntime.WithDiagnosticLimit(0)}
	if _, err := ResolveOptions(g, diag); err == nil || !strings.Contains(err.Error(), "stream") {
		t.Errorf("diagnostic option of a batch parse: %v", err)
	}
	if _, err := resolveEntryOptions(g, diag, true); err != nil {
		t.Errorf("diagnostic option of a stream: %v", err)
	}
}
