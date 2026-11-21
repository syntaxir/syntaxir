package state

import (
	"math/bits"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// growthSpend charges the copy that growing a table from oldLen to newLen entries stands for. The
// charge is a function of the two lengths alone: it falls where the length crosses a power of two,
// as doubling would copy, and never depends on the spare capacity that an earlier parse left in a
// pooled parser, so a parse is charged the same wherever its storage came from. The physical copy
// follows the charge and is not charged again.
func (p *Parser) growthSpend(oldLen, newLen int) {
	if oldLen > 0 && bits.Len(uint(newLen)) > bits.Len(uint(oldLen)) {
		p.chargeValues(oldLen)
	}
}

// Reset during ordinary action evaluation is counted. Mandatory interruption
// cleanup continues to call the original reset or runs after controls are nil.
func ResetSpend[T any](p *Parser, a *Arena[T], n int) {
	if !BudgetControlsEnabled || p.Control == nil {
		a.reset(n)
		return
	}
	for i := n; i < a.N; i++ {
		p.Spend()
		var zero T
		a.Chunks[i/64][i%64] = zero
	}
	a.N = n
}

// ValueEqual reports whether l and r are equal as == compares them; a pair without equality is not.
func (p *Parser) ValueEqual(l, r any) bool {
	// Two strings, as the names of fields are, are compared as the composition does, without its dispatch.
	if ls, ok := l.(string); ok {
		if rs, ok := r.(string); ok {
			return p.TextEqual(ls, rs)
		}
	}
	eq, _ := p.termEqual(l, r)
	return eq
}

// ValueBinary applies the binary operator op to l and r. An operator that is not one of action.BinaryOps,
// as only a corrupted program has, is an error.
func (p *Parser) ValueBinary(op string, l, r any) (any, error) {
	code, ok := binaryCode(op)
	if !ok {
		return nil, p.ActionError("unknown operator ", op)
	}
	return p.termBinary(code, l, r)
}

// ValueUnary applies the unary operator op to x.
func (p *Parser) ValueUnary(op string, x any) (any, error) {
	code, ok := unaryCode(op)
	if !ok {
		return nil, p.ActionError("unknown operator ", op)
	}
	return p.termUnary(code, x)
}

// ValueLogic checks the operand x of the logical operator op and returns it.
func (p *Parser) ValueLogic(op string, x any) (bool, error) {
	code, ok := logicalCode(op)
	if !ok {
		return false, p.ActionError("unknown operator ", op)
	}
	return p.termLogic(code, x)
}

func (p *Parser) ValueTextLen(s string) int {
	if !BudgetControlsEnabled || p.Control == nil || p.Unit == input.Bytes {
		return TextLen(p.Unit, s)
	}
	n := 0
	for off := 0; off < len(s); {
		p.spendValue(min(utf8.UTFMax, len(s)-off))
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
		n++
	}
	return n
}

func (p *Parser) ValueField(fs valueimpl.Fields, name string) (any, bool) {
	if !BudgetControlsEnabled || p.Control == nil {
		return fs.Get(name)
	}
	for _, f := range fs {
		p.Spend()
		if p.ValueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}

func (p *Parser) SetValueField(fs *valueimpl.Fields, name string, v any) {
	if !BudgetControlsEnabled || p.Control == nil {
		valueimpl.SetField(fs, name, v)
		return
	}
	for i := range *fs {
		p.Spend()
		if p.ValueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	*fs = append(*fs, valueimpl.NodeField{Name: name, Value: v})
}

// HasValueField reports whether the declared type t has the field name. Under budget controls it
// charges for the declared fields it scans.
func (p *Parser) HasValueField(t *program.Type, name string) bool {
	if !BudgetControlsEnabled || p.Control == nil {
		return t.HasField(name)
	}
	for _, f := range t.Fields {
		p.Spend()
		if p.ValueEqual(f, name) {
			return true
		}
	}
	return false
}

// VarValue returns the value of the variable in slot i. Under budget controls it charges one
// unit for each node on the path to the slot (the levels of the environment, and the leaf).
func (p *Parser) VarValue(slot int) any {
	if BudgetControlsEnabled && p.Control != nil {
		for l := 0; l <= p.envLevels; l++ {
			p.Spend()
		}
	}
	return p.Env.Get(slot, p.envLevels)
}
