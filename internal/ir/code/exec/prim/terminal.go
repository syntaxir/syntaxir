package prim

import (
	"fmt"
	"unicode/utf8"
)

// Terminal is a value-free terminal expression, made only of case-sensitive literals, classes,
// sequences, choices, options and negative lookaheads, that a target may run as one program, which
// Assemble makes of it. It is the terminal region of Core IR that the lowering lowered,
// without the groups that match as their bodies.
type Terminal struct {
	Kind TerminalKind
	// Lit is the literal of a TerminalLit.
	Lit Lit
	// Class is the class of a TerminalClass.
	Class ClassID
	// Items are the items of a TerminalSeq, the alternatives of a TerminalChoice, in order, and the
	// body of a TerminalOpt or a TerminalNot, its only item.
	Items []Terminal
}

// TerminalKind is the kind of a Terminal.
type TerminalKind uint8

// The kinds of Terminal.
const (
	// TerminalLit matches the literal.
	TerminalLit TerminalKind = iota
	// TerminalClass matches one character of the class.
	TerminalClass
	// TerminalSeq matches the items in turn.
	TerminalSeq
	// TerminalChoice matches the first alternative that matches.
	TerminalChoice
	// TerminalOpt matches the body or nothing.
	TerminalOpt
	// TerminalNot matches nothing where the body does not match.
	TerminalNot
)

// TerminalProgram is the immediate of skipTerminal: a terminal expression (Terminal) assembled into
// the program that the runtime's TerminalProgram interprets (pkg/runtime/libgo/terminal.go), whose
// operation codes are the TerminalOp constants. It is complete but for what only a printed parser
// numbers: the expectation table of the parser and the names of its class and literal tables.
//
// Code is the words of the program. The class operand of an operation indexes Classes, its literal
// operand Lits and its expectation operand Exps; a target prints the program by replacing each
// expectation operand with the number of the expectation in its parser. Refs lists the operand words
// in the order of Code.
type TerminalProgram struct {
	Code []uint16
	// Classes are the classes of the operations, each once, in the order the code first names them.
	// A guard tests a class, which is the first code point of a literal where its alternative begins
	// with one.
	Classes []Charset
	// Lits are the texts of the literals, each once, in the order the code first names them.
	Lits []string
	// Exps are the expectations of the operations, each once, in the order in which the program is
	// specified to record them first (Assemble): the expectation of the guards of a choice before
	// those of its alternatives. TerminalEmptyLit is the expectation of the empty literal.
	Exps []ExpID
	// Refs are the operand words of Code that index Classes, Lits and Exps, in the order of Code.
	Refs []TerminalRef
}

// TerminalEmptyLit is the expectation that a terminal program records for the empty literal: its
// quoted text, `""`, which the module tables do not hold, as the empty literal cannot fail.
const TerminalEmptyLit ExpID = -1

// Charset is a class of a terminal program: inclusive ranges of code points, negated or not.
type Charset struct {
	Ranges  []Range
	Negated bool
}

// TerminalRef is an operand word of a terminal program: its index in the code and what it indexes.
type TerminalRef struct {
	At   int
	Kind TerminalRefKind
}

// TerminalRefKind is what an operand word of a terminal program indexes.
type TerminalRefKind uint8

// The kinds of TerminalRef.
const (
	// RefClass indexes Classes.
	RefClass TerminalRefKind = iota
	// RefLit indexes Lits.
	RefLit
	// RefExp indexes Exps.
	RefExp
)

// The operation codes of a terminal program, those of the runtime's TerminalProgram. Every operation
// begins with its code and its length in words, the operation included.
const (
	// TerminalOpClass, class, exp: one character of the class.
	TerminalOpClass uint16 = iota
	// TerminalOpLiteral, lit, exp: the literal.
	TerminalOpLiteral
	// TerminalOpSeq, operations...: the operations in turn.
	TerminalOpSeq
	// TerminalOpChoice, table, operations...: the first operation that matches; table numbers the
	// dispatch table of the run of guarded alternatives that begins the choice, or is
	// TerminalNoTable.
	TerminalOpChoice
	// TerminalOpOptional, operation: the operation or nothing.
	TerminalOpOptional
	// TerminalOpNot, operation: nothing, where the operation does not match.
	TerminalOpNot
	// TerminalOpGuard, class, exp, operation: an alternative skipped where the next character is not
	// in the class, which records exp.
	TerminalOpGuard
	// TerminalOpGuardClass, class, exp, operations...: a TerminalOpGuard of an alternative that
	// begins with the class, whose operations are the rest of the alternative.
	TerminalOpGuardClass
)

// TerminalNoTable marks a choice without a dispatch table.
const TerminalNoTable = 0xffff

// MinTerminalTable is the smallest run of guarded alternatives at the start of a choice for which
// the runtime builds a dispatch table instead of testing the guards in turn.
const MinTerminalTable = 4

// Assemble returns the terminal expression x assembled into a terminal program, with the tables t
// that its immediates index, and false if the program does not fit the words of its code.
//
// Each alternative of a choice that begins with a literal or a class gets a guard that tests its
// first code point, recording the expectation of that literal or class; where the alternative begins
// with a class, the guard is a TerminalOpGuardClass that consumes it. The expectations are recorded
// in the order the printer of the straight-line code would record them: for a choice, the guard
// expectations of the run of guarded alternatives that begins it, then those of each alternative in
// turn.
func Assemble(x Terminal, t *Tables) (TerminalProgram, bool) {
	a := &assembler{t: t, classes: map[string]int{}, lits: map[string]int{}, exps: map[ExpID]int{}}
	a.emit(&x)
	return a.p, !a.overflow
}

// assembler assembles a terminal program (Assemble).
type assembler struct {
	t        *Tables
	p        TerminalProgram
	classes  map[string]int
	lits     map[string]int
	exps     map[ExpID]int
	tables   int
	overflow bool
}

// word appends n, noting a value that does not fit.
func (a *assembler) word(n int) {
	if n < 0 || n > 0xffff {
		a.overflow = true
	}
	a.p.Code = append(a.p.Code, uint16(n))
}

// ref appends the operand n of the kind k.
func (a *assembler) ref(k TerminalRefKind, n int) {
	a.p.Refs = append(a.p.Refs, TerminalRef{At: len(a.p.Code), Kind: k})
	a.word(n)
}

// open starts an operation whose length close sets.
func (a *assembler) open(op uint16) int {
	at := len(a.p.Code)
	a.p.Code = append(a.p.Code, op, 0)
	return at
}

func (a *assembler) close(at int) {
	n := len(a.p.Code) - at
	if n > 0xffff {
		a.overflow = true
	}
	a.p.Code[at+1] = uint16(n)
}

// class returns the index of the class c in the program.
func (a *assembler) class(c Charset) int {
	key := fmt.Sprintf("%v%v", c.Negated, c.Ranges)
	if i, ok := a.classes[key]; ok {
		return i
	}
	a.classes[key] = len(a.p.Classes)
	a.p.Classes = append(a.p.Classes, c)
	return a.classes[key]
}

// exp returns the index of the expectation e in the program, adding it on first use.
func (a *assembler) exp(e ExpID) int {
	if i, ok := a.exps[e]; ok {
		return i
	}
	a.exps[e] = len(a.p.Exps)
	a.p.Exps = append(a.p.Exps, e)
	return a.exps[e]
}

// litExp returns the expectation of the literal l.
func litExp(l Lit) ExpID {
	if l.Text == "" {
		return TerminalEmptyLit
	}
	return l.Exp
}

// charset returns the class c of the tables as a class of a program.
func charset(c Class) Charset { return Charset{Ranges: c.Ranges, Negated: c.Negated} }

// emit appends the operation of x.
func (a *assembler) emit(x *Terminal) {
	switch x.Kind {
	case TerminalLit:
		i, ok := a.lits[x.Lit.Text]
		if !ok {
			i = len(a.p.Lits)
			a.lits[x.Lit.Text] = i
			a.p.Lits = append(a.p.Lits, x.Lit.Text)
		}
		a.p.Code = append(a.p.Code, TerminalOpLiteral, 4)
		a.ref(RefLit, i)
		a.ref(RefExp, a.exp(litExp(x.Lit)))
	case TerminalClass:
		c := a.t.Class(x.Class)
		a.p.Code = append(a.p.Code, TerminalOpClass, 4)
		a.ref(RefClass, a.class(charset(c)))
		a.ref(RefExp, a.exp(c.Exp))
	case TerminalSeq:
		at := a.open(TerminalOpSeq)
		for i := range x.Items {
			a.emit(&x.Items[i])
		}
		a.close(at)
	case TerminalChoice:
		at := a.open(TerminalOpChoice)
		run := 0
		for run < len(x.Items) {
			if _, _, _, ok := a.first(&x.Items[run]); !ok {
				break
			}
			run++
		}
		if run >= MinTerminalTable && a.tables < TerminalNoTable {
			a.word(a.tables)
			a.tables++
		} else {
			a.word(TerminalNoTable)
		}
		for i := range x.Items {
			alt := &x.Items[i]
			first, exp, from, ok := a.first(alt)
			if !ok {
				a.emit(alt)
				continue
			}
			if class, rest, ok := leadingClass(alt); ok && class == from {
				// The guard tests the class that the alternative begins with.
				guard := a.open(TerminalOpGuardClass)
				a.ref(RefClass, a.class(first))
				a.ref(RefExp, exp)
				for _, it := range rest {
					a.emit(it)
				}
				a.close(guard)
				continue
			}
			guard := a.open(TerminalOpGuard)
			a.ref(RefClass, a.class(first))
			a.ref(RefExp, exp)
			a.emit(alt)
			a.close(guard)
		}
		a.close(at)
	case TerminalOpt:
		at := a.open(TerminalOpOptional)
		a.emit(&x.Items[0])
		a.close(at)
	case TerminalNot:
		at := a.open(TerminalOpNot)
		a.emit(&x.Items[0])
		a.close(at)
	default:
		panic(fmt.Sprintf("prim.Assemble: terminal kind %d", x.Kind))
	}
}

// first returns the class and the expectation (its index in the program) of the guard of an
// alternative of a choice, with the literal or the class it comes from: the first literal or class
// that the alternative must match first, a literal standing for its first code point.
func (a *assembler) first(x *Terminal) (Charset, int, *Terminal, bool) {
	switch x.Kind {
	case TerminalLit:
		if x.Lit.Text == "" {
			break
		}
		r, _ := utf8.DecodeRuneInString(x.Lit.Text)
		return Charset{Ranges: []Range{{Lo: r, Hi: r}}}, a.exp(litExp(x.Lit)), x, true
	case TerminalClass:
		c := a.t.Class(x.Class)
		return charset(c), a.exp(c.Exp), x, true
	case TerminalSeq:
		if len(x.Items) > 0 {
			return a.first(&x.Items[0])
		}
	}
	return Charset{}, 0, nil, false
}

// leadingClass returns the class that x matches first and the expressions it then matches in turn,
// if x is a class or a sequence that begins with one.
func leadingClass(x *Terminal) (*Terminal, []*Terminal, bool) {
	switch x.Kind {
	case TerminalClass:
		return x, nil, true
	case TerminalSeq:
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
