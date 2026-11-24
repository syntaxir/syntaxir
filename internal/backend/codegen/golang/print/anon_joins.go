package print

import (
	"strings"

	"syntaxir.dev/sxir/internal/frontend/types"
)

// anonJoinName returns the Go name of the interface of an undeclared join: its members in the order
// written, joined by Or; one member and error is that member and Error. An unnamed node member is
// named after its value and Node (valueName), so node[string] is StringNode and node[[]A] AListNode.
func anonJoinName(u types.JoinTy) string {
	parts := make([]string, len(u.Alts))
	for i, a := range u.Alts {
		parts[i] = valueName(a)
	}
	if len(parts) == 1 {
		parts = append(parts, "Error")
	}
	return strings.Join(parts, "Or")
}

// valueName returns a Go name for values of type x in the name of an interface: a declared type's
// name, a node's value followed by Node (node[_] is Node), a list's element followed by List, a
// tuple's elements followed by Tuple, and an optional value's after Opt.
func valueName(x types.Ty) string {
	switch x := x.(type) {
	case types.NamedTy:
		if x.Kind == 'c' {
			return "Missing"
		}
		return x.Name
	case types.NodeTy:
		if x.Name != "" {
			return x.Name
		}
		return valueName(x.Value) + "Node"
	case types.ListTy:
		return valueName(x.Elem) + "List"
	case types.TupleTy:
		var b strings.Builder
		for _, e := range x.Elems {
			b.WriteString(valueName(e))
		}
		return b.String() + "Tuple"
	case types.OptTy:
		return "Opt" + valueName(x.Elem)
	case types.JoinTy:
		parts := make([]string, len(x.Alts))
		for i, a := range x.Alts {
			parts[i] = valueName(a)
		}
		return strings.Join(parts, "Or")
	case types.BasicTy:
		switch x {
		case types.TyTop, types.TyAny:
			return ""
		case types.TyErr:
			return "Error"
		}
		return strings.ToUpper(string(x)[:1]) + string(x)[1:]
	}
	return ""
}

// anonJoinText returns the join u of anonJoins as written: one declared node type is that type and
// error.
func anonJoinText(u types.JoinTy) string {
	if len(u.Alts) == 1 {
		return u.String() + " | error"
	}
	return u.String()
}
