package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Action expressions of the engine lowered from Exec IR.
//
// The engine evaluates an action, a predicate or the value of a binding as the function of its site
// in the Action module of its table, as generated parsers do: the same compositions of the
// operators of action expressions (action_exec.go), in the same order, with the same failures. The compiler
// translates the function once, when the program is built, into the instructions of an ActionProgram
// (vmlower.ActionProgram), which ActionProgram.Eval runs; a compiled grammar file holds them
// (Export, LoadAction). Instructions read and write registers of four files, by the Exec IR type
// of the local they hold: values (Datum, Obj), integers (int and bool), element slices (Seq) and
// strings (Str), so that no integer, slice or string is boxed to be held.
//
// Every composition that can fail is followed in the lowered function by the return of its
// failure; the translation folds the two into the one instruction, whose failure ends Eval.

// ActionProgram is an action expression of the action language compiled for the engine. It is immutable and may be
// shared by parses that run concurrently.
type ActionProgram struct {
	Code []ActionInstr
	// The sizes of the register files.
	NA, NN, NS, NT int32
	Consts         []any
	Names          []string // action.name and the names of Operators Rule locals and struct fields
	Members        []ActionMember
	NodeTypes      []*nodes.NodeTypeKinds
	Casts          []ActionCast
	Structs        []ActionStruct
}

type ActionMember struct {
	Kind        int
	Name, Field string
}

type ActionCast struct {
	Type      *program.FieldType
	Line, Col int
}

type ActionStruct struct {
	Kind   *valueimpl.Kind
	Fields int
}

type ActionOp uint8

const (
	TNop ActionOp = iota
	// Moves and constants.
	TConst // a[dst] = consts[a]
	TIntK  // n[dst] = a
	TMoveA // a[dst] = a[a]
	TMoveN // n[dst] = n[a]
	TMoveS // s[dst] = s[a]
	TMoveT // t[dst] = t[a]
	TCap   // a[dst] = the capture in slot a of the frame
	TLocal // a[dst] = the Operators Rule local names[a]
	TVar   // a[dst] = the variable in slot a
	TUnbox // a[dst] = unboxed a[a]
	TFromB // a[dst] = bool n[a]
	TFromT // a[dst] = string t[a]
	TNot   // n[dst] = !n[a]
	TLt    // n[dst] = n[a] < n[b]
	TAdd   // n[dst] = n[a] + n[b]
	TEqN   // n[dst] = n[a] == n[b]
	TIsNil // n[dst] = a[a] == nil
	// Compositions; those marked fallible end Eval with their failure.
	TMember   // fallible: a[dst] = member(kind, a[a]) of members[b]
	TUnary    // fallible: a[dst] = unary(b, a[a])
	TBinary   // fallible: a[dst] = binary(c, a[a], a[b])
	TLogic    // fallible: n[dst] = logic(b, a[a])
	TText     // fallible: t[dst] = text(a[a])
	TTextEq   // n[dst] = t[a] == t[b]
	TLength   // fallible: a[dst] = length(a[a])
	TConvert  // fallible: a[dst] = convert(nodeTypes[b], a[a])
	TConforms // n[dst] = conforms(casts[b], a[a])
	TCastErr  // a[dst] = the failure of the cast casts[b] of a[a]
	TKids     // n[dst] = the number of gathered elements
	TPush     // fallible: push(names[b], a[a])
	TPushAll  // fallible: pushItems(names[b], a[a])
	TAbove    // s[dst] = the elements gathered above n[a], taken off
	THull     // a[dst] = hull(b != 0, s[a]), counting a unit for each element
	TItems    // fallible: s[dst] = items(names[b], a[a])
	TItemAt   // a[dst] = itemAt(s[a], n[b], c != 0)
	TSeqLen   // n[dst] = len(s[a])
	TStep     // count the unit of an element that map, filter or a fold visits (seq.step)
	TKeep     // keep element n[b] of s[a]
	TFilter   // fallible: n[dst] = filterKeep(a[a])
	TCopy     // a[dst] = a new list of the elements s[a], counting two units for each element
	TStruct   // a[dst] = a new struct structs[a]
	TSetField // set field names[c] of the struct a[a] to a[b]
	TCover    // n[dst] = cover(a[a], a[b], n[c])
	TMade     // made(a[a], n[b])
	// Control.
	TJump   // continue at dst, which follows
	TJumpF  // continue at dst, which follows, if n[a] is false
	TRet    // return a[a]
	TRetErr // return the failure a[a]
	// Fused.
	TField // set field names[c] of the struct a[a] to a[b]; n[dst] = cover(a[a], a[b], n[dst])
	// Logical compute units of the values that the plain evaluation of the action expression builds and its
	// lowering does not (spec/runtime/parsing.md#compute-units).
	TUnits      // count a units (units.add)
	TUnitsOf    // count b units for each element of the list or tuple a[a] (units.items)
	TUnitsSince // count a unit for each element gathered above n[a] (units.since)
	// Counted loops, the only backward jumps (checkActionLoops).
	TLoop    // n[c] = len(s[a]); n[b] = 0; continue at dst if n[c] is 0
	TEndLoop // n[b]++; continue at dst if n[b] < n[c]
)

type ActionInstr struct {
	Op      ActionOp
	Dst     int32
	A, B, C int32
}
