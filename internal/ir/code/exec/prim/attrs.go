package prim

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
)

// Attrs are the attributes of a function of grammar code at the Exec stage, which code.Func.Attrs
// holds. execlower converts the attributes of each function at the Flow stage into them: every
// attribute except the record of the forms, which the end of the Flow passes consumes.
type Attrs struct {
	// Rule is the rule whose body, or part of it, the function matches.
	Rule *core.Rule
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
	// Budget reports a function lowered with budget controls, which charges the budget explicitly.
	Budget bool
	// Document reports a function lowered for a build with Documents: every in-place read has the
	// recording read as its alternative.
	Document bool
	// Traced reports a function of the traced copies that an observed parse runs, every read of which
	// records how far the parse examined the input.
	Traced bool
	// Entered reports a function lowered with budget controls whose entry the printed method charges:
	// the method that holds the function begins with one charge, and the function charges nothing for
	// its entry.
	Entered bool
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
// attributes of the copy leaves those of the original. The rule is shared.
func (a *Attrs) CloneAttrs(*code.Cloner) any {
	out := *a
	out.Vars = slices.Clone(a.Vars)
	return &out
}

// verifyFuncs checks that each function of m, a module at the Exec stage that code.Verify has
// verified, has the Exec-stage attributes or none: the check of the Exec stage
// (code.RegisterStageCheck). An attribute of an earlier stage, such as the Flow attributes, is an
// error, as a call of a Flow operation is.
func verifyFuncs(m *code.Module) error {
	for _, fn := range m.Funcs {
		if fn.Attrs == nil {
			continue
		}
		if _, ok := fn.Attrs.(*Attrs); !ok {
			return fmt.Errorf("%s: attributes of the type %T at the Exec stage, not %T", fn.Name, fn.Attrs, (*Attrs)(nil))
		}
	}
	return nil
}

func init() { code.RegisterStageCheck[code.Exec](verifyFuncs) }

// VerifyModule checks m, a module at the Exec stage, as the kind of the Exec stage does: the
// verifier of Code IR, then the attributes of each function.
func VerifyModule(m *code.Module) error {
	if err := code.Verify(m); err != nil {
		return err
	}
	return verifyFuncs(m)
}
