package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The value primitives of terms that only the engine has (term_value.go holds those it shares with the
// library of generated parsers): the codes of operators named by their text, which the engine's value
// API takes, val.unbox, node.isStruct, and act.made, whose context differs from the library's.

// binaryCode returns the code of the binary operator op, or false if op is not one of action.BinaryOps.
// The codes are looked up for every term that is evaluated, so the lookup is a switch and not a search
// of the table; TestOperatorCodes holds it to the table.
func binaryCode(op string) (int, bool) {
	switch op {
	case "+":
		return OpAdd, true
	case "-":
		return OpSub, true
	case "*":
		return OpMul, true
	case "/":
		return OpQuo, true
	case "%":
		return OpRem, true
	case "==":
		return OpEq, true
	case "!=":
		return OpNe, true
	case "<":
		return OpLt, true
	case "<=":
		return OpLe, true
	case ">":
		return OpGt, true
	case ">=":
		return OpGe, true
	case "[]":
		return OpIndex, true
	}
	return 0, false
}

// unaryCode returns the code of the unary operator op, or false if op is not one of action.UnaryOps.
func unaryCode(op string) (int, bool) {
	switch op {
	case "-":
		return OpNeg, true
	case "!":
		return OpNot, true
	}
	return 0, false
}

// logicalCode returns the code of the logical operator op, or false if op is not one of
// action.LogicalOps.
func logicalCode(op string) (int, bool) {
	switch op {
	case "&&":
		return OpAnd, true
	case "||":
		return OpOr, true
	}
	return 0, false
}

// unboxed is the operation val.unbox: the value that a node pointer stands for: nil for a nil
// pointer, the value of a box, and otherwise v.
func unboxed(v any) any {
	if n, ok := v.(*value.Node); ok {
		if n == nil {
			return nil
		}
		if nodes.D(n).Kind == valueimpl.BoxKind {
			return unboxed(nodes.D(n).Box)
		}
	}
	return v
}

// made is the operation act.made.
func (c *EvalCtx) made(n *value.Node) { c.P.Created = append(c.P.Created, n) }

// isStruct is the operation node.isStruct.
func (p *Parser) isStruct(n *value.Node) bool {
	return p.Prog.Types[n.Type()] != nil
}
