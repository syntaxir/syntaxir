package synth_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/bench/synth"
	"syntaxir.dev/sxir/internal/driver/entry"
)

// TestGrammarCompiles checks that grammars of several shapes compile.
func TestGrammarCompiles(t *testing.T) {
	for _, s := range []synth.Shape{
		{Rules: 1, Alts: 2},
		{Rules: 3, Alts: 2, Depth: 1, LeftRecursion: 1},
		{Rules: 20, Alts: 4, Depth: 3, LeftRecursion: 4, Seed: 1},
		{Rules: 40, Alts: 3, Depth: 8, LeftRecursion: 3, Seed: 2},
	} {
		src := synth.Grammar(s)
		if _, err := entry.CompileSource(src, "", entry.Options{}); err != nil {
			t.Errorf("%+v: %v\n%s", s, err, src)
		}
	}
}

// TestGrammarDeterministic checks that a shape gives one text and that the seed changes it.
func TestGrammarDeterministic(t *testing.T) {
	s := synth.Shape{Rules: 10, Alts: 3, Depth: 2, LeftRecursion: 2, Seed: 7}
	a, b := synth.Grammar(s), synth.Grammar(s)
	if a != b {
		t.Fatal("two texts for one shape")
	}
	s.Seed++
	if synth.Grammar(s) == a {
		t.Error("the seed changes nothing")
	}
	if !strings.Contains(a, "rule r0 = r0 ") || strings.Contains(a, "rule r1 = r1 ") {
		t.Errorf("left recursion of every second rule missing:\n%s", a)
	}
}
