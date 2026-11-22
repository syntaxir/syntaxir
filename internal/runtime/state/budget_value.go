package state

import (
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// ValueEqual reports whether l and r are equal as == compares them; a pair without equality is not.
func (p *Parser) ValueEqual(l, r any) bool {
	// Two strings, as the names of fields are, are compared as the composition does, without its dispatch.
	if ls, ok := l.(string); ok {
		if rs, ok := r.(string); ok {
			return p.TextEqual(ls, rs)
		}
	}
	eq, _ := p.actionEqual(l, r)
	return eq
}

// ValueBinary applies the binary operator op to l and r. An operator that is not one of action.BinaryOps,
// as only a corrupted program has, is an error.
func (p *Parser) ValueBinary(op string, l, r any) (any, error) {
	code, ok := binaryCode(op)
	if !ok {
		return nil, p.ActionError("unknown operator ", op)
	}
	return p.actionBinary(code, l, r)
}

// ValueUnary applies the unary operator op to x.
func (p *Parser) ValueUnary(op string, x any) (any, error) {
	code, ok := unaryCode(op)
	if !ok {
		return nil, p.ActionError("unknown operator ", op)
	}
	return p.actionUnary(code, x)
}

// ValueLogic checks the operand x of the logical operator op and returns it.
func (p *Parser) ValueLogic(op string, x any) (bool, error) {
	code, ok := logicalCode(op)
	if !ok {
		return false, p.ActionError("unknown operator ", op)
	}
	return p.actionLogic(code, x)
}

// ValueTextLen is str.len: the length of s in the position unit. It counts the bytes of s in either
// unit (spec/runtime/parsing.md#compute-units).
func (p *Parser) ValueTextLen(s string) int {
	p.valueUnits(len(s))
	return TextLen(p.Unit, s)
}

// ValueField returns the field name of fs (node.field). Reading a field counts nothing.
func (p *Parser) ValueField(fs valueimpl.Fields, name string) (any, bool) { return fs.Get(name) }

// SetValueField sets the field name of fs to v. Setting a field counts nothing.
func (p *Parser) SetValueField(fs *valueimpl.Fields, name string, v any) {
	valueimpl.SetField(fs, name, v)
}

// HasValueField reports whether the declared type t has the field name. It counts nothing.
func (p *Parser) HasValueField(t *program.Type, name string) bool { return t.HasField(name) }

// VarValue returns the value of the variable in slot i. Reading a variable counts nothing.
func (p *Parser) VarValue(slot int) any { return p.Env.Get(slot, p.envLevels) }
