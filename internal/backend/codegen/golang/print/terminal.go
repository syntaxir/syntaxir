package print

import (
	"fmt"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Value-free terminal expressions in lean typed Go parsers.
//
// A value-free expression made only of literals, character classes, sequences, choices, options
// and negative lookaheads (a terminal expression) needs no state but the position. Written out as
// straight-line code, a large one, such as a keyword list spelled as a trie of case-insensitive
// character classes, takes tens of kilobytes. The Flow pass terminals assembles such an expression
// into a terminal program (prim.TerminalProgram), which the printer declares as a static
// rt.TerminalProgram that the library's SkipTerminal interprets. SkipTerminal makes the same
// calls as straight-line code would: it consumes the same input and
// records the same expectations at the same positions. Where it departs from the order of that
// code it does so only where the departure is unobservable: a guard that admits the next
// character also matches the class that begins its alternative, and where Expect records
// nothing at the position of a choice, alternatives whose guards do not admit the character are
// passed over without testing them (pkg/runtime/libgo/terminal.go).

// terminalTable returns the package variable that holds the terminal program p, declaring it on first
// use. The printer completes the program: it numbers its expectations in the parser, in the order of
// p.Exps, puts their numbers in its expectation operands, and names its classes and literals in the
// order the code first names them. It returns false if an expectation number does not fit a word.
func (g *Generator) terminalTable(p prim.TerminalProgram) (string, bool) {
	tab := g.Tables()
	ids := make([]int, len(p.Exps))
	for i, e := range p.Exps {
		if e == prim.TerminalEmptyLit {
			ids[i] = int(g.Desc(strconv.Quote("")))
		} else {
			ids[i] = int(g.Desc(tab.Exp(e)))
		}
		if ids[i] > 0xffff {
			return "", false
		}
	}
	code := append([]uint16(nil), p.Code...)
	classNames := make([]string, len(p.Classes))
	litItems := make([]string, len(p.Lits))
	for _, r := range p.Refs {
		i := int(code[r.At])
		switch r.Kind {
		case prim.RefClass:
			if classNames[i] == "" {
				c := p.Classes[i]
				e := &ast.CharClass{Negated: c.Negated}
				for _, r := range c.Ranges {
					e.Ranges = append(e.Ranges, ast.CharRange{Lo: r.Lo, Hi: r.Hi})
				}
				classNames[i] = "&" + g.classTable(e)
			}
		case prim.RefLit:
			if litItems[i] == "" {
				val := p.Lits[i]
				litItems[i] = fmt.Sprintf("{Runes: %s, Text: %q}", g.literal(val), val)
			}
		case prim.RefExp:
			code[r.At] = uint16(ids[i])
		}
	}
	words := make([]string, len(code))
	for i, w := range code {
		words[i] = strconv.Itoa(int(w))
	}
	body := fmt.Sprintf("{Code: []uint16{%s}, Classes: []*Class{%s}", strings.Join(words, ", "), strings.Join(classNames, ", "))
	if len(litItems) > 0 {
		body += fmt.Sprintf(", Literals: []TerminalText{%s}", strings.Join(litItems, ", "))
	}
	body += "}"
	key := "term" + body
	if name := g.varNames[key]; name != "" {
		return name, true
	}
	name := g.Name("term")
	g.varNames[key] = name
	// The program is static data; Prepare builds its dispatch tables at initialization.
	fmt.Fprintf(&g.Vars, "var %sData = TerminalProgram%s\nvar %[1]s = %[1]sData.Prepare()\n", name, body)
	return name, true
}
