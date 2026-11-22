package state

import (
	"unicode/utf8"

	"syntaxir.dev/sxir/pkg/runtime/input"
)

// MatchChar compares the character want of a literal with the input at the position, as a literal
// compares each of its characters (spec/runtime/documents.md#reuse-model, Slots), and returns its
// size in units if it matches. In code points it reads the character at the position. In bytes it
// compares the UTF-8 encoding of want with the input byte by byte and stops at the first byte that
// differs: no character that begins with other bytes can be want, so the bytes after it are not
// examined, and a stream reads none of them. U+FFFD also matches one invalid byte, which only
// decoding tells, so it decodes the character. With fold, want is folded (FoldASCIIRune) and an
// ASCII letter of the input matches it in either case.
//
// It records the input it examined and consumes nothing. It charges a unit for reading the
// character, as Peek does, and what decoding charges when it decodes; comparing bytes charges
// nothing more.
//
// The first-character guards of every backend test the character a literal begins with through it,
// so that skipping an alternative examines exactly what evaluating the alternative would.
func (p *Parser) MatchChar(want rune, fold bool) (int, bool) {
	p.spendRead(1)
	return p.compareChar(want, fold)
}

// compareChar is MatchChar without the unit of the read.
func (p *Parser) compareChar(want rune, fold bool) (int, bool) {
	if p.Unit == input.CodePoints || want == utf8.RuneError {
		got, size, examined, ok := p.decodeRest(p.Pos)
		p.touch(examined)
		if !ok || got != want && !(fold && FoldASCIIRune(got) == want) {
			return 0, false
		}
		return size, true
	}
	var enc [utf8.UTFMax]byte
	n := utf8.EncodeRune(enc[:], want)
	for k := range n {
		b, ok := p.byteRest(p.Pos + k)
		p.touch(p.Pos + k + 1)
		if !ok || b != enc[k] && !(n == 1 && fold && FoldASCIIRune(rune(b)) == want) {
			return 0, false
		}
	}
	return n, true
}

// ChoiceRead is the read of the character at the position of a choice that the first-character
// guards of its alternatives share (core.First). Reading the character is charged once, at the
// first guard, and decoding it once, at the first guard that needs the decoded character, as one
// Peek before the guards charged them. Each guard examines what the alternative it guards would
// examine there, so that skipping an alternative is observably the same as evaluating it: the guard
// of a literal compares as MatchChar, and the guard of a class decodes the character.
type ChoiceRead struct {
	read, decoded, more bool
	ch                  rune
}

func (c *ChoiceRead) charge(p *Parser) {
	if !c.read {
		p.spendRead(1)
		c.read = true
	}
}

// Literal reports whether the character at the position can begin a literal whose first character
// is want, folded with fold (MatchChar).
func (c *ChoiceRead) Literal(p *Parser, want rune, fold bool) bool {
	c.charge(p)
	if p.Unit == input.CodePoints || want == utf8.RuneError {
		got, ok := c.Char(p)
		return ok && (got == want || fold && FoldASCIIRune(got) == want)
	}
	_, ok := p.compareChar(want, fold)
	return ok
}

// Char returns the character at the position, decoded, and whether there is one.
func (c *ChoiceRead) Char(p *Parser) (rune, bool) {
	c.charge(p)
	if !c.decoded {
		got, _, examined, ok := p.decodeRest(p.Pos)
		p.touch(examined)
		c.ch, c.more, c.decoded = got, ok, true
	}
	return c.ch, c.more
}

// AtLineEnd reports whether a line ends at the position (`$`): the input ends there or a line feed or
// a carriage return follows. It examines the slot at the position alone (spec/runtime/documents.md,
// Slots): a line feed and a carriage return are one byte each, so in bytes the byte at the position
// decides it and no character is decoded. It charges a unit for reading the slot.
func (p *Parser) AtLineEnd() bool {
	p.spendRead(1)
	p.touch(p.Pos + 1)
	if p.Unit == input.Bytes {
		b, ok := p.byteRest(p.Pos)
		return !ok || b == '\n' || b == '\r'
	}
	ch, _, _, ok := p.decodeRest(p.Pos)
	return !ok || ch == '\n' || ch == '\r'
}
