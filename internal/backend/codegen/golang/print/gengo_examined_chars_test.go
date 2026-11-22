package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// TestGeneratedGoExaminedCharacterComparisons checks the examined end of the call of atom at 0 in each
// case of codegentest.ExaminedChars, as the typed parse, the Node parse and the recognizer of the
// generated Go parser report it.
func TestGeneratedGoExaminedCharacterComparisons(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	chars := codegentest.ExaminedChars()
	var cases []codegentest.Case
	for _, c := range chars {
		cases = append(cases, codegentest.Case{Name: c.Name, Src: c.Src(), Inputs: []string{c.Input}})
	}
	codegentest.EachMode(t, func(t *testing.T) {
		seen := map[string]int{}
		for _, r := range runExaminedProbes(t, cases, codegentest.LibCopy(), nil) {
			c := chars[r.Case]
			if r.Bytes != c.Bytes {
				continue
			}
			seen[r.Entry]++
			found := false
			for _, e := range r.Events {
				if e.Kind == instrument.TraceExit && e.Rule == "atom" && e.Pos == 0 {
					found = true
					if e.Matched != c.Matched || e.Examined != c.Examined {
						t.Errorf("%s, %s: atom@0 matched %v, examined %d; want matched %v, examined %d", c.Name, r.Entry, e.Matched, e.Examined, c.Matched, c.Examined)
					}
				}
			}
			if !found {
				t.Errorf("%s, %s: no exit of atom at 0", c.Name, r.Entry)
			}
		}
		for _, entry := range []string{"value", "parse", "recognize"} {
			if seen[entry] != len(chars) {
				t.Errorf("%s ran on %d cases, want %d", entry, seen[entry], len(chars))
			}
		}
	})
}
