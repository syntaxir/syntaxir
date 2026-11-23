package print

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Value-free terminal expressions in lean typed Go parsers.
//
// A value-free expression made only of literals, character classes, sequences, choices, options
// and negative lookaheads (a terminal expression) needs no state but the position. Written out as
// straight-line code, a large one, such as a keyword list spelled as a trie of case-insensitive
// character classes, takes tens of kilobytes. The generator lowers such an expression to a static
// rt.TerminalProgram that the library's SkipTerminal interprets. SkipTerminal makes the same
// calls as straight-line code would: it consumes the same input and
// records the same expectations at the same positions. Where it departs from the order of that
// code it does so only where the departure is unobservable: a guard that admits the next
// character also matches the class that begins its alternative, and where Expect records
// nothing at the position of a choice, alternatives whose guards do not admit the character are
// passed over without testing them (pkg/runtime/libgo/terminal.go).

// minTerminalTable is the smallest run of guarded alternatives at the start of a choice for
// which the library builds a dispatch table instead of testing the guards in turn.
const minTerminalTable = 4

// terminalTable returns the package variable that holds the terminal expression x lowered to a
// TerminalProgram, declaring it on first use: x is the immediate of skipTerminal, the expression of a
// terminal region that the passes chose (Core IR's TerminalRegions and the Flow form of the region).
// It returns false if its program does not fit the operands of the program.
func (g *Generator) terminalTable(x prim.Terminal) (string, bool) {
	t := &terminalProgram{g: g, tab: g.Tables(), classes: map[string]int{}, literals: map[string]int{}}
	t.emit(&x)
	if t.overflow {
		return "", false
	}
	words := make([]string, len(t.code))
	for i, w := range t.code {
		words[i] = strconv.Itoa(int(w))
	}
	body := fmt.Sprintf("{Code: []uint16{%s}, Classes: []*Class{%s}", strings.Join(words, ", "), strings.Join(t.classNames, ", "))
	if len(t.literalItems) > 0 {
		body += fmt.Sprintf(", Literals: []TerminalText{%s}", strings.Join(t.literalItems, ", "))
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

// Operation codes of rt.TerminalProgram.
const (
	terminalClass uint16 = iota
	terminalLiteral
	terminalSeq
	terminalChoice
	terminalOptional
	terminalNot
	terminalGuard
	terminalGuardClass
)

// terminalNoTable marks a choice without a dispatch table (rt.TerminalNoTable).
const terminalNoTable = 0xffff

// terminalProgram assembles the code of a rt.TerminalProgram.
type terminalProgram struct {
	g                 *Generator
	tab               *prim.Tables
	code              []uint16
	classes, literals map[string]int
	classNames        []string
	literalItems      []string
	tables            int
	overflow          bool
}

// word appends n, noting a value that does not fit.
func (t *terminalProgram) word(n int) {
	if n < 0 || n > 0xffff {
		t.overflow = true
	}
	t.code = append(t.code, uint16(n))
}

// open starts an operation whose length is set by close.
func (t *terminalProgram) open(op uint16) int {
	at := len(t.code)
	t.code = append(t.code, op, 0)
	return at
}

func (t *terminalProgram) close(at int) {
	n := len(t.code) - at
	if n > 0xffff {
		t.overflow = true
	}
	t.code[at+1] = uint16(n)
}

func (t *terminalProgram) class(e *ast.CharClass) int {
	name := t.g.classTable(e)
	if i, ok := t.classes[name]; ok {
		return i
	}
	t.classes[name] = len(t.classNames)
	t.classNames = append(t.classNames, "&"+name)
	return t.classes[name]
}

// emptyLitDesc is the expectation text of the empty literal, which the program records for it though
// the literal cannot fail and Code IR gives it no expectation.
const emptyLitDesc = `""`

// litDesc returns the ID of the expectation of the literal l.
func (t *terminalProgram) litDesc(l prim.Lit) rprogram.ExpID {
	if l.Text == "" {
		return t.g.Desc(emptyLitDesc)
	}
	return t.g.Desc(t.tab.Exp(l.Exp))
}

// emit appends the operation of x.
func (t *terminalProgram) emit(x *prim.Terminal) {
	g := t.g
	switch x.Kind {
	case prim.TerminalLit:
		val := x.Lit.Text
		i, ok := t.literals[val]
		if !ok {
			i = len(t.literalItems)
			t.literals[val] = i
			t.literalItems = append(t.literalItems, fmt.Sprintf("{Runes: %s, Text: %q}", g.literal(val), val))
		}
		t.code = append(t.code, terminalLiteral, 4)
		t.word(i)
		t.word(int(t.litDesc(x.Lit)))
	case prim.TerminalClass:
		c := t.tab.Class(x.Class)
		t.code = append(t.code, terminalClass, 4)
		t.word(t.class(grammarClass(c)))
		t.word(int(g.Desc(t.tab.Exp(c.Exp))))
	case prim.TerminalSeq:
		at := t.open(terminalSeq)
		for i := range x.Items {
			t.emit(&x.Items[i])
		}
		t.close(at)
	case prim.TerminalChoice:
		at := t.open(terminalChoice)
		run := 0
		for run < len(x.Items) {
			if _, _, _, ok := t.first(&x.Items[run]); !ok {
				break
			}
			run++
		}
		if run >= minTerminalTable && t.tables < terminalNoTable {
			t.word(t.tables)
			t.tables++
		} else {
			t.word(terminalNoTable)
		}
		for i := range x.Items {
			alt := &x.Items[i]
			// Each alternative gets the guard that first gives it.
			if first, id, from, ok := t.first(alt); ok {
				if class, rest, ok := leadingClass(alt); ok && class == from {
					// The guard tests the class that the alternative begins with.
					guard := t.open(terminalGuardClass)
					t.word(t.class(first))
					t.word(int(id))
					for _, it := range rest {
						t.emit(it)
					}
					t.close(guard)
					continue
				}
				guard := t.open(terminalGuard)
				t.word(t.class(first))
				t.word(int(id))
				t.emit(alt)
				t.close(guard)
				continue
			}
			t.emit(alt)
		}
		t.close(at)
	case prim.TerminalOpt:
		at := t.open(terminalOptional)
		t.emit(&x.Items[0])
		t.close(at)
	case prim.TerminalNot:
		at := t.open(terminalNot)
		t.emit(&x.Items[0])
		t.close(at)
	default:
		panic(fmt.Sprintf("terminalProgram.emit: kind %d", x.Kind))
	}
}

// first returns the class and expectation of the guard of an alternative of a terminal expression,
// with the literal or the class it comes from: the first literal or class that the alternative must
// match first, a literal standing for its first code point. It is the first terminal of Core IR
// (Attrs.First) of the alternative.
func (t *terminalProgram) first(x *prim.Terminal) (*ast.CharClass, rprogram.ExpID, *prim.Terminal, bool) {
	switch x.Kind {
	case prim.TerminalLit:
		if x.Lit.Text == "" {
			break
		}
		r, _ := utf8.DecodeRuneInString(x.Lit.Text)
		return &ast.CharClass{Ranges: []ast.CharRange{{Lo: r, Hi: r}}}, t.litDesc(x.Lit), x, true
	case prim.TerminalClass:
		c := t.tab.Class(x.Class)
		return grammarClass(c), t.g.Desc(t.tab.Exp(c.Exp)), x, true
	case prim.TerminalSeq:
		if len(x.Items) > 0 {
			return t.first(&x.Items[0])
		}
	}
	return nil, 0, nil, false
}

// leadingClass returns the class that x matches first and the expressions it then matches in turn,
// if x is a class or a sequence that begins with one.
func leadingClass(x *prim.Terminal) (*prim.Terminal, []*prim.Terminal, bool) {
	switch x.Kind {
	case prim.TerminalClass:
		return x, nil, true
	case prim.TerminalSeq:
		if len(x.Items) > 0 {
			if c, rest, ok := leadingClass(&x.Items[0]); ok {
				for i := range x.Items[1:] {
					rest = append(rest, &x.Items[1+i])
				}
				return c, rest, true
			}
		}
	}
	return nil, nil, false
}
