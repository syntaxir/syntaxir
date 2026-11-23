package prim

import (
	"reflect"
	"slices"
	"testing"
)

// TestAssemble pins the program of a choice whose first four alternatives are guarded, which takes a
// dispatch table: the guards of literals test their first code point, the guard of an alternative that
// begins with a class consumes it, the expectations of the guards of the run come before those that
// the alternatives record, and the empty literal records its quoted text (TerminalEmptyLit).
func TestAssemble(t *testing.T) {
	tab := &Tables{Exps: []string{`"ab"`, "[x]", `"c"`, `"d"`}, Classes: []Class{{Ranges: []Range{{'x', 'x'}}, Exp: 1}}}
	lit := func(s string, e ExpID) Terminal { return Terminal{Kind: TerminalLit, Lit: Lit{Text: s, Exp: e}} }
	class := Terminal{Kind: TerminalClass, Class: 0}
	x := Terminal{Kind: TerminalChoice, Items: []Terminal{
		{Kind: TerminalSeq, Items: []Terminal{lit("ab", 0), class}},
		lit("c", 2),
		class,
		lit("d", 3),
		{Kind: TerminalOpt, Items: []Terminal{lit("", 0)}},
	}}
	p, ok := Assemble(x, tab)
	if !ok {
		t.Fatal("the program does not fit")
	}
	want := []uint16{
		TerminalOpChoice, 43, 0,
		TerminalOpGuard, 14, 0, 0, TerminalOpSeq, 10, TerminalOpLiteral, 4, 0, 0, TerminalOpClass, 4, 1, 2,
		TerminalOpGuard, 8, 2, 1, TerminalOpLiteral, 4, 1, 1,
		TerminalOpGuardClass, 4, 1, 2,
		TerminalOpGuard, 8, 3, 3, TerminalOpLiteral, 4, 2, 3,
		TerminalOpOptional, 6, TerminalOpLiteral, 4, 3, 4,
	}
	if !slices.Equal(p.Code, want) {
		t.Errorf("code\n%v\nwant\n%v", p.Code, want)
	}
	if wantExps := []ExpID{0, 2, 1, 3, TerminalEmptyLit}; !slices.Equal(p.Exps, wantExps) {
		t.Errorf("expectations %v, want %v", p.Exps, wantExps)
	}
	if wantLits := []string{"ab", "c", "d", ""}; !slices.Equal(p.Lits, wantLits) {
		t.Errorf("literals %q, want %q", p.Lits, wantLits)
	}
	set := func(r rune) Charset { return Charset{Ranges: []Range{{r, r}}} }
	if wantClasses := []Charset{set('a'), set('x'), set('c'), set('d')}; !reflect.DeepEqual(p.Classes, wantClasses) {
		t.Errorf("classes %v, want %v", p.Classes, wantClasses)
	}
	// The references are the operand words in order, each indexing its table.
	var refs []TerminalRef
	for _, at := range []int{5, 6, 11, 12, 15, 16, 19, 20, 23, 24, 27, 28, 31, 32, 35, 36, 41, 42} {
		k := RefExp
		switch at {
		case 5, 15, 19, 27, 31:
			k = RefClass
		case 11, 23, 35, 41:
			k = RefLit
		}
		refs = append(refs, TerminalRef{At: at, Kind: k})
	}
	if !slices.Equal(p.Refs, refs) {
		t.Errorf("references\n%v\nwant\n%v", p.Refs, refs)
	}
	if err := tab.imm(p); err != nil {
		t.Errorf("the tables reject the program: %v", err)
	}
}

// TestAssembleOverflow checks that a program longer than the length word of an operation can hold
// does not assemble.
func TestAssembleOverflow(t *testing.T) {
	tab := &Tables{Exps: []string{"[x]"}, Classes: []Class{{Ranges: []Range{{'x', 'x'}}}}}
	x := Terminal{Kind: TerminalSeq, Items: make([]Terminal, 1<<14)}
	for i := range x.Items {
		x.Items[i] = Terminal{Kind: TerminalClass}
	}
	if _, ok := Assemble(x, tab); ok {
		t.Error("a sequence of 65538 words assembles")
	}
	x.Items = x.Items[:100]
	if _, ok := Assemble(x, tab); !ok {
		t.Error("a sequence of 402 words does not assemble")
	}
}
