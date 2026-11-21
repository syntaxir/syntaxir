package prim

import (
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
)

// Registers of grammar code, at the Flow and the Exec stage alike.
const (
	// Pos is the position.
	Pos = "pos"
	// Cut is the cut flag of the scope the runtime entered last.
	Cut = "cut"
)

// Handle types of grammar code, at the Flow and the Exec stage alike: the types of variables besides
// int and bool, which the runtime implements.
const (
	TValue   = "value"
	TEnv     = "env"
	TExpMark = "expmark"
	TExpIDs  = "expids"
	TVals    = "vals"
	TCtx     = "ctx"
	TMark    = "mark"
	TFrame   = "frame"
	TError   = "error"
	TRune    = "rune"
)

// HandleTypes are the handle types, in the order the modules of grammar code declare them.
var HandleTypes = []string{TValue, TEnv, TExpMark, TExpIDs, TVals, TCtx, TMark, TFrame, TError, TRune}

// Type classifies the type of a variable of grammar code: int, bool or one of the handle types.
type Type uint8

const (
	// TypeInt is a position, a count or a length.
	TypeInt Type = iota
	TypeBool
	TypeRune
	// TypeValue is a value of the runtime.
	TypeValue
	// TypeEnv is a variable environment.
	TypeEnv
	// TypeExpMark is a mark of the recorded expectations.
	TypeExpMark
	// TypeExpIDs is a list of expectations.
	TypeExpIDs
	// TypeVals is a list of values being gathered.
	TypeVals
	// TypeCtx is the context of an action.
	TypeCtx
	// TypeMark is a mark of the whole parser state (markState).
	TypeMark
	// TypeFrame is a capture frame of the runtime.
	TypeFrame
	// TypeError is a syntax error.
	TypeError
)

var typeNames = [...]string{"int", "bool", "rune", "value", "env", "expmark", "expids", "vals", "ctx", "mark", "frame", "error"}

func (t Type) String() string { return typeNames[t] }

// Types returns every classification, in the order of their values.
func Types() []Type {
	out := make([]Type, len(typeNames))
	for i := range out {
		out[i] = Type(i)
	}
	return out
}

// TypeOf returns the classification of the Code IR type t of a register, handle or local of grammar
// code. It panics on a type that is none of them.
func TypeOf(t code.Type) Type {
	switch t.Kind {
	case code.KInt:
		return TypeInt
	case code.KBool:
		return TypeBool
	}
	for i, n := range typeNames {
		if n == t.Handle {
			return Type(i)
		}
	}
	panic("prim: no variable type " + t.String())
}

// CodeType returns the Code IR type of a variable classified as t.
func CodeType(t Type) code.Type {
	switch t {
	case TypeInt:
		return code.TInt
	case TypeBool:
		return code.TBool
	}
	return code.THandle(t.String())
}

// unnamedPrefix begins the labels that the lowering gives the loops it leaves unnamed.
const unnamedPrefix = "loop."

// UnnamedLabel returns the label of the n-th loop that the lowering leaves unnamed.
func UnnamedLabel(n int) string { return unnamedPrefix + strconv.Itoa(n) }

// Unnamed reports whether the label is one that the lowering leaves unnamed (UnnamedLabel), which a
// printer numbers instead of naming.
func Unnamed(label string) bool { return strings.HasPrefix(label, unnamedPrefix) }
