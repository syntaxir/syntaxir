package core

import (
	"strconv"
	"strings"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// FixedDescs are the descriptions of the fixed expectations of the runtime, in ExpID order: Any
// and the anchors, which name no written text.
var FixedDescs = []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}

// LiteralDesc is the expectation text of a literal, as written: a case-insensitive literal keeps its
// suffix i.
func LiteralDesc(e *ast.Literal) string {
	if e.IgnoreCase {
		return Quote(e.Value, nil) + "i"
	}
	return Quote(e.Value, nil)
}

// Quote returns s as a grammar string literal: \" and \\, \n \t \r, \u{h} for every other
// character that strconv.IsPrint rejects, and every other character as itself. The formatter writes
// literals with it, so a grammar and its expected items spell a literal alike. A non-nil poll is
// called before each character, which lets a caller interrupt the quoting of a long literal.
func Quote(s string, poll func()) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if poll != nil {
			poll()
		}
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if strconv.IsPrint(r) {
				b.WriteRune(r)
			} else {
				b.WriteString(`\u{`)
				b.WriteString(strconv.FormatInt(int64(r), 16))
				b.WriteByte('}')
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// CharClassString returns the character class as grammar syntax writes it, which names the class
// in expectations.
func CharClassString(e *ast.CharClass) string {
	var b strings.Builder
	b.WriteString("[")
	if e.Negated {
		b.WriteString("^")
	}
	esc := func(r rune) string {
		switch r {
		case '-', '[', ']', '^', '\\':
			return `\` + string(r)
		}
		s := strconv.QuoteRune(r)
		return s[1 : len(s)-1]
	}
	for _, rg := range e.Ranges {
		b.WriteString(esc(rg.Lo))
		if rg.Hi != rg.Lo {
			b.WriteString("-" + esc(rg.Hi))
		}
	}
	b.WriteString("]")
	return b.String()
}
