package libgo

import (
	"slices"
	"testing"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// TestMemoSideKeepsWhatItIsGiven checks that a memo entry gives back the errors, the expectations
// and the key it was given, in any order of the setters: the errors as the same error or list, and
// the slices over the same arrays, so that they keep the same memory alive and count the same bytes.
func TestMemoSideKeepsWhatItIsGiven(t *testing.T) {
	err := &sxirvalue.SyntaxError{}
	list := &errList{n: 2}
	exps := []ExpID{3, 1, 4}
	key := []any{"a", 2}
	for _, r := range []recEntry{{}, {err: err}, {list: list}} {
		for _, x := range [][]ExpID{nil, {}, exps} {
			for _, k := range [][]any{nil, {}, key} {
				for order := range 2 {
					var s memoSide
					if order == 0 {
						s.setRecs(r)
						s.setExps(x)
					} else {
						s.setExps(x)
						s.setRecs(r)
					}
					s.setKey(k)
					if got := s.recs(); got != r {
						t.Fatalf("errors %+v, want %+v", got, r)
					}
					if got := s.exps(); !slices.Equal(got, x) || len(x) > 0 && &got[0] != &x[0] || len(got) != cap(got) {
						t.Fatalf("expectations %v (capacity %d), want %v over the same array", got, cap(got), x)
					}
					if got := s.key(); !slices.Equal(got, k) || len(k) > 0 && &got[0] != &k[0] {
						t.Fatalf("key %v, want %v over the same array", got, k)
					}
				}
			}
		}
	}
	// Setting the errors again replaces the kind of pointer, and setting the expectations again
	// keeps it.
	var s memoSide
	s.setRecs(recEntry{list: list})
	s.setRecs(recEntry{err: err})
	s.setExps(exps)
	if s.recs() != (recEntry{err: err}) || len(s.exps()) != 3 {
		t.Fatalf("errors %+v and %d expectations after resetting", s.recs(), len(s.exps()))
	}
	s.setRecs(recEntry{list: list})
	s.setExps(nil)
	if s.recs() != (recEntry{list: list}) || s.exps() != nil {
		t.Fatalf("errors %+v and expectations %v after resetting", s.recs(), s.exps())
	}
}
