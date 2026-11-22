package ref

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
)

// What action expressions make (action.Gathered and action.Made): the stack of the elements gathered for the lists and
// tuples being made, and the list of the structs made. Each is a sequence of identifiers with marks: its
// length, which a drop cuts it back to.

// Elem is the model's handle of an element that a case pushes: a conformance identifier. The null
// element is the null handle, which the model lists as 0.
type Elem struct{ ID int64 }

// Items is the model's handle of the elements of a list or a tuple (action.Seq) that a case pushes at once:
// the identifiers of its elements.
type Items struct{ IDs []int64 }

// Struct is the model's handle of a struct that a case records as made: a conformance identifier.
type Struct struct{ ID int64 }

// Act is the model's handle of the context of an action expression (action.Act). Pushing and recording do not read it,
// so a case has one.
type Act struct{}

type actionState struct {
	gathered, made []int64
	// recorded holds every struct that act.made recorded, those dropped since included: a struct is
	// recorded at most once in a parse.
	recorded map[int64]bool
}

// Gathered returns the identifiers of the elements gathered, from the bottom of the stack, 0 for a null
// element.
func (m *Model) Gathered() []int64 { return slices.Clone(m.actionExprs.gathered) }

// Made returns the identifiers of the structs made, in the order they were recorded.
func (m *Model) Made() []int64 { return slices.Clone(m.actionExprs.made) }

func isActionStateOp(name string) bool {
	switch name {
	case "act.push", "act.pushAll", "action.kids", "action.dropKids", "action.elemsAbove", "action.keep",
		"act.made", "action.created", "action.dropCreated":
		return true
	}
	return false
}

// allowsActionState checks the preconditions of the operations of the elements gathered and the values
// made, and reports false for any other operation.
func (m *Model) allowsActionState(o *code.Op, args []any) (allowed, ok bool) {
	s := &m.actionExprs
	switch o.Name {
	case "act.pushAll":
		_, isItems := args[1].(*Items)
		return isItems, true
	case "action.dropKids", "action.elemsAbove":
		return args[0].(int64) >= 0 && args[0].(int64) <= int64(len(s.gathered)), true
	case "action.keep":
		items, isItems := args[0].(*Items)
		return isItems && args[1].(int64) >= 0 && args[1].(int64) < int64(len(items.IDs)), true
	case "act.made":
		n, isStruct := args[1].(Struct)
		return isStruct && !s.recorded[n.ID], true
	case "action.dropCreated":
		return args[0].(int64) >= 0 && args[0].(int64) <= int64(len(s.made)), true
	case "act.push", "action.kids", "action.created":
		return true, true
	}
	return false, false
}

// actionStateQuantity returns action.QDropped and action.QSeqLen for a call of an operation of the elements
// gathered or the values made.
func (m *Model) actionStateQuantity(name string, o *code.Op, args []any) (int64, bool) {
	if o == nil || !isActionStateOp(o.Name) {
		return 0, false
	}
	switch {
	case name == action.QDropped && (o.Name == "action.dropKids" || o.Name == "action.elemsAbove"):
		return int64(len(m.actionExprs.gathered)) - args[0].(int64), true
	case name == action.QDropped && o.Name == "action.dropCreated":
		return int64(len(m.actionExprs.made)) - args[0].(int64), true
	case name == action.QSeqLen && o.Name == "act.pushAll":
		return int64(len(args[1].(*Items).IDs)), true
	}
	return 0, false
}

// callActionState runs an operation of the elements gathered or the values made, and reports false for
// any other.
func (m *Model) callActionState(o *code.Op, args []any) ([]any, bool) {
	s := &m.actionExprs
	switch o.Name {
	case "act.push":
		id := int64(0)
		if e, ok := args[1].(Elem); ok {
			id = e.ID
		}
		s.gathered = append(s.gathered, id)
	case "act.pushAll":
		s.gathered = append(s.gathered, args[1].(*Items).IDs...)
	case "action.kids":
		return []any{int64(len(s.gathered))}, true
	case "action.dropKids":
		s.gathered = s.gathered[:args[0].(int64)]
	case "action.elemsAbove":
		k := args[0].(int64)
		items := &Items{IDs: slices.Clone(s.gathered[k:])}
		s.gathered = s.gathered[:k]
		return []any{items}, true
	case "action.keep":
		s.gathered = append(s.gathered, args[0].(*Items).IDs[args[1].(int64)])
	case "act.made":
		id := args[1].(Struct).ID
		if s.recorded == nil {
			s.recorded = map[int64]bool{}
		}
		s.recorded[id] = true
		s.made = append(s.made, id)
	case "action.created":
		return []any{int64(len(s.made))}, true
	case "action.dropCreated":
		s.made = s.made[:args[0].(int64)]
	default:
		return nil, false
	}
	return nil, true
}
