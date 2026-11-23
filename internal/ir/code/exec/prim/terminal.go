package prim

// Terminal is the immediate of skipTerminal: a value-free terminal expression, made only of
// case-sensitive literals, classes, sequences, choices, options and negative lookaheads, that a
// target may run as one program. It is the terminal region of Core IR that the lowering lowered,
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
