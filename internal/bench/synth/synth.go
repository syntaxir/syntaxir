// Package synth generates synthetic grammars of a given shape, for measuring how the work of the
// compiler grows with the size of a grammar (bench/metrics). A grammar is a function of its Shape
// alone: the same shape, seed included, gives the same text on every run and platform.
package synth

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Shape is the size of a synthetic grammar.
type Shape struct {
	// Rules is the number of rules besides the start rule; at least 1.
	Rules int
	// Alts is the number of alternatives of each rule; at least 2.
	Alts int
	// Depth is how deep the groups of the last alternative of each rule nest; 0 gives no group.
	Depth int
	// LeftRecursion, if not 0, makes every LeftRecursion-th rule (counting from the first)
	// directly left-recursive: its first alternative begins with a call of the rule itself.
	LeftRecursion int
	// Seed selects the literals, character classes, calls and suffixes of the alternatives.
	Seed uint64
}

// Grammar returns the source of the grammar of shape s, which the compiler accepts. The start rule
// main repeats the first rule up to the end of the input. Rule i calls rule i+1, so every rule is
// reachable, and calls no rule before it except itself, so the only recursion is the left recursion
// of the shape. Every alternative begins with a literal or, in a left-recursive rule, with a call of
// the rule, so no rule is nullable and every repetition consumes input.
func Grammar(s Shape) string {
	if s.Rules < 1 || s.Alts < 2 || s.Depth < 0 || s.LeftRecursion < 0 {
		panic(fmt.Sprintf("synth: invalid shape %+v", s))
	}
	g := &gen{s: s, r: rand.New(rand.NewPCG(s.Seed, 0x73796e7468))}
	var b strings.Builder
	b.WriteString("package synth\n\n@start\nrule main = r0* $$\n")
	for i := range s.Rules {
		// A rule is one line: a newline ends the expression.
		fmt.Fprintf(&b, "\nrule r%d =", i)
		for a := range s.Alts {
			if a > 0 {
				b.WriteString(" /")
			}
			b.WriteString(" " + g.alt(i, a))
		}
		b.WriteString("\n")
	}
	return b.String()
}

type gen struct {
	s Shape
	r *rand.Rand
}

// alt returns alternative a of rule i.
func (g *gen) alt(i, a int) string {
	var items []string
	if a == 0 && g.s.LeftRecursion > 0 && i%g.s.LeftRecursion == 0 {
		items = append(items, fmt.Sprintf("r%d", i), g.literal())
	} else {
		items = append(items, g.literal())
	}
	if a == 1 && i+1 < g.s.Rules {
		items = append(items, fmt.Sprintf("r%d", i+1))
	}
	items = append(items, g.item(i))
	if a == g.s.Alts-1 && g.s.Depth > 0 {
		items = append(items, g.group(i, g.s.Depth))
	}
	return strings.Join(items, " ")
}

// group returns a group of two alternatives whose first nests depth-1 further groups.
func (g *gen) group(i, depth int) string {
	inner := g.literal() + " " + g.item(i)
	if depth > 1 {
		inner += " " + g.group(i, depth-1)
	}
	return "(" + inner + " / " + g.literal() + ")"
}

// item returns a literal, a character class or a call of a later rule, with an optional suffix.
func (g *gen) item(i int) string {
	var x string
	switch k := g.r.IntN(3); {
	case k == 0 || i+1 >= g.s.Rules:
		x = g.literal()
	case k == 1:
		lo := 'a' + rune(g.r.IntN(20))
		x = fmt.Sprintf("[%c-%c]", lo, lo+rune(1+g.r.IntN(5)))
	default:
		x = fmt.Sprintf("r%d", i+1+g.r.IntN(min(8, g.s.Rules-i-1)))
	}
	return x + [...]string{"", "", "?", "*", "+"}[g.r.IntN(5)]
}

// literal returns a string literal of two to four lowercase letters.
func (g *gen) literal() string {
	n := 2 + g.r.IntN(3)
	b := make([]byte, n)
	for j := range b {
		b[j] = byte('a' + g.r.IntN(26))
	}
	return `"` + string(b) + `"`
}
