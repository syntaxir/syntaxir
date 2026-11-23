package prim

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
)

// Attrs are the attributes of a function of grammar code at the Exec stage, which code.Func.Attrs
// holds. execlower converts the attributes of each function at the Flow stage into them: every
// attribute except the record of the forms, which the end of the Flow passes consumes.
type Attrs struct {
	// Rule is the ID of the rule whose body, or part of it, the function matches.
	Rule int
	// Vars are the locals that are variables of the function, in the order they were made; the
	// other locals are its entry variables Start, Result and Ctx (empty where it has none), which
	// the caller declares: the position at which the rule was entered, the rule's value, and the
	// context of the rule's action. Start is set before the body runs. A framed function has none.
	Vars               []string
	Start, Result, Ctx string
	// Fail is the label of the block that the body leaves on failure. Running to the end of the block
	// is success.
	Fail string
	// Cut is the local of a framed function's cut flag of its outermost scope, or empty. The function
	// reads it from the Cut register on entry and publishes it to the register again on both returns.
	Cut string
	// Binds reports that the body may bind variables, which the entry restores.
	Binds bool
	// Frame reports a framed function, which returns its value and true on success and the null value
	// and false on failure.
	Frame bool
	// Node reports a function that builds the values of the Node runtime (newTerminalNode,
	// newBranchNode) and none of the typed runtime.
	Node bool
	// Document reports a function lowered for a build with Documents: every in-place read has the
	// recording read as its alternative.
	Document bool
	// Traced reports a function of the traced copies that an observed parse runs, every read of which
	// records how far the parse examined the input.
	Traced bool
}

// AttrsOf returns the Exec-stage attributes of f, or nil.
func AttrsOf(f *code.Func) *Attrs {
	a, _ := f.Attrs.(*Attrs)
	return a
}

// VarsOf returns the variables of the function f (Attrs.Vars), in order, with their types.
func VarsOf(f *code.Func) []code.Param {
	a := AttrsOf(f)
	out := make([]code.Param, 0, len(a.Vars))
	for _, name := range a.Vars {
		for _, l := range f.Locals {
			if l.Name == name {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// CloneAttrs returns a copy of a for the copy of its function, so that a pass that changes the
// attributes of the copy leaves those of the original.
func (a *Attrs) CloneAttrs(*code.Cloner) any {
	out := *a
	out.Vars = slices.Clone(a.Vars)
	return &out
}

// verifyFuncs checks that each function of m, a module at the Exec stage that code.Verify has
// verified, has the Exec-stage attributes or none, and the tables of m (verifyTables): the check of
// the Exec stage (code.RegisterStageCheck). An attribute of an earlier stage, such as the Flow attributes, is an
// error, as a call of a Flow operation is.
func verifyFuncs(m *code.Module) error {
	for _, fn := range m.Funcs {
		if fn.Attrs == nil {
			continue
		}
		if _, ok := fn.Attrs.(*Attrs); !ok {
			return fmt.Errorf("%s: attributes of the type %T at the Exec stage, not %T", fn.Name, fn.Attrs, (*Attrs)(nil))
		}
		if err := verifyMarks(fn); err != nil {
			return fmt.Errorf("%s: %w", fn.Name, err)
		}
	}
	return verifyTables(m)
}

// verifyMarks checks the marks of the whole parser state in the function fn of grammar code: in the
// order of the code, each reset, trail drop and position read of a mark takes a local that a markState
// set before it, and a trail drop a mark that saves the trail.
func verifyMarks(fn *code.Func) error {
	marks := map[string]StateMark{}
	var err error
	code.Walk(fn.Body, func(s code.Stmt) {
		if set, ok := s.(*code.Set); ok && len(set.Dst) == 1 && len(set.Values) == 1 && !set.Dst[0].Reg {
			if c, ok := set.Values[0].(*code.Call); ok && c.Callee == OpMarkState {
				marks[set.Dst[0].Name], _ = c.Imm.(StateMark)
			}
		}
	}, func(e code.Expr) {
		c, ok := e.(*code.Call)
		if !ok || err != nil || (c.Callee != OpReset && c.Callee != OpDropTrail && c.Callee != OpMarkPos) {
			return
		}
		l, ok := c.Args[0].(*code.Local)
		if !ok {
			err = fmt.Errorf("%s of a mark that is not a local", c.Callee)
			return
		}
		m, ok := marks[l.Name]
		switch {
		case !ok:
			err = fmt.Errorf("%s of the mark %s before a markState sets it", c.Callee, l.Name)
		case c.Callee == OpDropTrail && !m.Trail:
			err = fmt.Errorf("%s of the mark %s, which does not save the trail", c.Callee, l.Name)
		}
	})
	return err
}

func init() { code.RegisterStageCheck[code.Exec](verifyFuncs) }

// VerifyModule checks m, a module at the Exec stage, as the kind of the Exec stage does: the
// verifier of Code IR, then the attributes of each function and the tables.
func VerifyModule(m *code.Module) error {
	if err := code.Verify(m); err != nil {
		return err
	}
	return verifyFuncs(m)
}
