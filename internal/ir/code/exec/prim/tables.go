package prim

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/code"
)

// Tables are the module attributes of grammar code (code.Module.Attrs) at the Flow and the Exec
// stage: the tables that the immediates of its operations index, one per module, as a Core module has
// one of each. The lowering from Core IR fills them in the order of the Core module, so that an
// index is the same number in both IRs.
type Tables struct {
	// Exps holds the expectations by ExpID.
	Exps []string
	// Classes holds the character classes by ClassID.
	Classes []Class
}

// ExpID identifies an expectation of the module (Tables.Exps).
type ExpID int

// ClassID identifies a character class of the module (Tables.Classes): the immediate of matchClass
// and inClass.
type ClassID int

// Class is a character class: its inclusive ranges of code points, whether it is negated, and the
// expectation it records on failure.
type Class struct {
	Ranges  []Range
	Negated bool
	Exp     ExpID
}

// Range is an inclusive range of code points.
type Range struct{ Lo, Hi rune }

// ClassRun is the immediate of skipClasses: the classes it matches in turn.
type ClassRun []ClassID

// Demand is the immediate of inputDemand: the input that a read needs past the position. The Lit form
// demands the bytes of a literal, in the position units. The Char form demands one complete character at the
// position: one code point in code points, and in bytes enough bytes to decode one UTF-8 character, or the
// final flag. The runtime resolves each form in its unit, so no backend guesses the amount.
type Demand struct {
	Lit  Lit
	Char bool
}

// Lit is the immediate of hasLit and matchLit: the text of a literal, whether it is
// case-insensitive (each ASCII letter of the input matches either case of the same letter in Text),
// and the expectation it records on failure, which the empty literal, which cannot fail, has none
// of.
type Lit struct {
	Text string
	Fold bool
	Exp  ExpID
}

// Insertion is the immediate of insertMissing, newMissingNode and missingValue: a #missing site, numbered from 1
// among those of the module, and the literal that it inserts.
type Insertion struct {
	Site int
	Lit  Lit
}

// Guard is the immediate of guardFails and guardLitFails: what the next character must be for an
// alternative to begin. Lit reports a literal, whose first code point is Rune (Fold: Rune is an ASCII
// lower-case letter, and its upper-case form matches too); otherwise Class is the class. Exp is the
// expectation of the literal or the class, and Depth the depth of the alternative's first terminal.
type Guard struct {
	Lit   bool
	Rune  rune
	Fold  bool
	Class ClassID
	Exp   ExpID
	Depth int
}

// Anchor is the immediate of atAnchor: the anchor a position is tested for.
type Anchor uint8

// The anchors, in the order of the anchor expectations of a module.
const (
	BeginInput Anchor = iota
	EndInput
	BeginLine
	EndLine
)

// Shape is the immediate of shapeSingle, shapeCapture and scopeValue: the static shape of a value
// (the runtime's Shaper). Elems reports that the value is the tuple node of its elements, K is the
// number of valued elements, Mode how the value where it takes one element is made, Tuple that the
// value of a scope with this body is the tuple node, and Shared that the tuple node is shared
// through a splice rule's memo entry.
type Shape struct {
	Elems bool
	K     int
	Mode  SingleMode
	Tuple bool
	// Shared reports that the tuple node is shared through a splice rule's memo entry.
	Shared bool
}

// SingleMode is how the value of an expression is made where it takes one element.
type SingleMode uint8

// The modes of SingleMode.
const (
	// SingleNone: no value.
	SingleNone SingleMode = iota
	// SinglePass: the value of the element itself.
	SinglePass
	// SingleTuple: the tuple node of the elements.
	SingleTuple
)

// TablesOf returns the tables of the module m, or nil if it has none.
func TablesOf(m *code.Module) *Tables {
	t, _ := m.Attrs.(*Tables)
	return t
}

// Exp returns the expectation e.
func (t *Tables) Exp(e ExpID) string { return t.Exps[e] }

// Class returns the class c.
func (t *Tables) Class(c ClassID) Class { return t.Classes[c] }

// verifyTables checks that every immediate of a call in m that indexes the tables of m indexes an
// entry of them: the check of the tables at both stages.
func verifyTables(m *code.Module) error {
	t := TablesOf(m)
	if t == nil {
		if m.Attrs != nil {
			return fmt.Errorf("module attributes of the type %T, not %T", m.Attrs, t)
		}
		t = &Tables{}
	}
	for _, c := range t.Classes {
		if err := t.exp(c.Exp); err != nil {
			return fmt.Errorf("class: %w", err)
		}
	}
	for _, fn := range m.Funcs {
		var err error
		code.Walk(fn.Body, nil, func(e code.Expr) {
			if c, ok := e.(*code.Call); ok && err == nil && c.Imm != nil {
				if err = t.imm(c.Imm); err != nil {
					err = fmt.Errorf("%s: %s: %w", fn.Name, c.Callee, err)
				}
			}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// imm checks that the immediate x indexes entries of t.
func (t *Tables) imm(x any) error {
	switch x := x.(type) {
	case ExpID:
		return t.exp(x)
	case Expectation:
		return t.exp(x.Exp)
	case SkippedCalls:
		return t.exp(x.Exp)
	case Lit:
		if x.Text == "" {
			return nil
		}
		return t.exp(x.Exp)
	case ClassID:
		return t.class(x)
	case ClassRun:
		for _, c := range x {
			if err := t.class(c); err != nil {
				return err
			}
		}
	case TerminalProgram:
		for _, e := range x.Exps {
			if e != TerminalEmptyLit {
				if err := t.exp(e); err != nil {
					return err
				}
			}
		}
	case Guard:
		if !x.Lit {
			if err := t.class(x.Class); err != nil {
				return err
			}
		}
		return t.exp(x.Exp)
	}
	return nil
}

func (t *Tables) exp(e ExpID) error {
	if e < 0 || int(e) >= len(t.Exps) {
		return fmt.Errorf("expectation %d of %d", e, len(t.Exps))
	}
	return nil
}

func (t *Tables) class(c ClassID) error {
	if c < 0 || int(c) >= len(t.Classes) {
		return fmt.Errorf("class %d of %d", c, len(t.Classes))
	}
	return nil
}

// VerifyTables checks the tables of m and the immediates that index them (verifyTables), for the
// verifier of the Flow stage, whose package cannot register a check of its own on prim's behalf.
func VerifyTables(m *code.Module) error { return verifyTables(m) }
