package corepass_test

import (
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/ir/core"
)

// TestOperatorLineAttributesAgreeWithFacts checks that the stored nullability of every line of an
// Operators Rule in the corpus, which the left-binding check reads, equals the nullability that the
// left-call check computes from the facts. Both are read per line, without and with token insertion.
// The corpus must hold Operators Rule lines, so that the agreement is not checked vacuously.
func TestOperatorLineAttributesAgreeWithFacts(t *testing.T) {
	t.Parallel()
	var lines atomic.Int64
	t.Cleanup(func() {
		if lines.Load() == 0 {
			t.Fatal("no Operators Rule line in the corpus")
		}
		t.Logf("checked %d Operators Rule lines", lines.Load())
	})
	for _, path := range factsCorpus() {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			m, f := moduleFacts(t, path)
			for _, r := range m.Rules {
				if r == nil {
					continue
				}
				core.Walk(r.Body, func(n core.Node) {
					o, ok := n.(*core.OperatorsRule)
					if !ok {
						return
					}
					for _, l := range o.Lines() {
						lines.Add(1)
						at := l.Body.Attrs()
						if want := f.Nullable(l.Body); at.Nullable != want {
							t.Errorf("rule %d line: Nullable attribute %v, facts %v", r.ID, at.Nullable, want)
						}
						if want := f.NullableIns(l.Body); at.NullableIns != want {
							t.Errorf("rule %d line: NullableIns attribute %v, facts %v", r.ID, at.NullableIns, want)
						}
					}
				})
			}
		})
	}
}
