package code

import (
	"fmt"
	"slices"
)

// The functions of a module of many, one at a time.
//
// A module may hold many functions, such as the module of a rule table, while a pass works on one
// function: the passes run on a module that declares what the module of many declares and holds one
// of its functions (FuncUnit), which is then put back (PutFuncUnit).

// FuncUnit returns a module that declares what m declares and holds the function i of m alone. It
// shares the declarations of m, clipped so that one that is added to it is not added to m, and the
// function itself, which a pass may change in place.
func (m *Module) FuncUnit(i int) *Module {
	u := *m
	u.Dialects = slices.Clip(m.Dialects)
	u.Features = slices.Clip(m.Features)
	u.Components = slices.Clip(m.Components)
	u.Registers = slices.Clip(m.Registers)
	u.Handles = slices.Clip(m.Handles)
	u.Ops = slices.Clip(m.Ops)
	u.Funcs = []*Func{m.Funcs[i]}
	return &u
}

// PutFuncUnit puts the function of u, a module that FuncUnit(i) made from m and that passes may have
// changed or replaced, in place of the function i of m, and declares in m the operations that u
// declares and m does not. A module that does not hold one function is an error.
func (m *Module) PutFuncUnit(i int, u *Module) error {
	if len(u.Funcs) != 1 {
		return fmt.Errorf("a pass replaced or removed the function %s of %s: its unit holds %d functions", m.Funcs[i].Name, m.Name, len(u.Funcs))
	}
	m.Funcs[i] = u.Funcs[0]
	// The operations that u shares with m come first, as FuncUnit made them.
	k := 0
	for k < len(u.Ops) && k < len(m.Ops) && u.Ops[k] == m.Ops[k] {
		k++
	}
	for _, o := range u.Ops[k:] {
		if m.Op(o.Name) == nil {
			m.AddOp(o)
		}
	}
	return nil
}
