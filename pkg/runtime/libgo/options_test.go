package libgo

import (
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestResolveRuntimeOptions checks that the library takes the options of package runtime, the one
// definition of them: each option sets what the library reads, in order, and the option that only
// the reference parser supports is an error.
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
	for _, limit := range []int{1, -1} {
		_, err := ResolveOptions(g, []sxirruntime.ParseOption{sxirruntime.WithMissingTokens(limit)})
		if err == nil || !strings.Contains(err.Error(), "WithMissingTokens") {
			t.Errorf("WithMissingTokens(%d): %v", limit, err)
		}
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
