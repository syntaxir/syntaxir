// Package execdata holds what the conversion to the Exec stage (execlower) gives the printers and the
// passes of the targets: a lowered function, which is a function of the Exec module of a table with
// its module and its variables, its kind and the view of a module as its lowered functions, the
// verifier of the Exec module, and the tables of a target at the Exec stage (tables@exec), which the
// printers take. It holds no pass code, so the print packages and the backends name these types
// without importing the conversion.
package execdata

import (
	"fmt"
	"io"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Lowered is a function of grammar code at the Exec stage.
type Lowered struct {
	// Module declares the registers, handle types and operations that Func calls, and holds Func.
	Module *code.Module
	Func   *code.Func
	// Vars are the locals of Func that are variables of the lowered function (prim.Attrs.Vars); the
	// other locals are its entry variables, which the caller declares.
	Vars []string
}

// Clone returns a copy of l that a pass may change without changing l: a copy of its module
// (code.Module.Clone) with the copy of its function.
func (l *Lowered) Clone() *Lowered {
	m := l.Module.Clone()
	out := &Lowered{Module: m, Func: l.Func, Vars: slices.Clone(l.Vars)}
	if i := slices.Index(l.Module.Funcs, l.Func); i >= 0 {
		out.Func = m.Funcs[i]
	}
	return out
}

// WithModule returns l with m, a module that passes made from l.Module, in place of its module: its
// function is the function of m at the place of l.Func in l.Module, which must have its name and
// keep the variables (Keeps). l is not changed. A module without that function, or whose function
// lost a variable, is an error.
func (l *Lowered) WithModule(m *code.Module) (*Lowered, error) {
	if i := slices.Index(l.Module.Funcs, l.Func); i >= 0 && i < len(m.Funcs) && m.Funcs[i].Name == l.Func.Name {
		out := &Lowered{Module: m, Func: m.Funcs[i], Vars: l.Vars}
		if err := out.Keeps(); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("an Exec pass replaced or removed the function %s", l.Func.Name)
}

// Keeps reports an error if the module of l no longer holds l.Func, or l.Func no longer declares a
// local of l.Vars: what the printers print, so what every pass on a lowered function keeps.
func (l *Lowered) Keeps() error {
	if !slices.Contains(l.Module.Funcs, l.Func) {
		return fmt.Errorf("an Exec pass replaced or removed the function %s", l.Func.Name)
	}
	for _, v := range l.Vars {
		if !slices.ContainsFunc(l.Func.Locals, func(p code.Param) bool { return p.Name == v }) {
			return fmt.Errorf("an Exec pass removed the local %s of the function %s", v, l.Func.Name)
		}
	}
	return nil
}

// Verify checks a module that the conversion made with the verifier of the Exec stage
// (prim.VerifyModule). A module it rejects is an error of the conversion, which names the module.
func Verify(m *code.Module) error {
	if err := prim.VerifyModule(m); err != nil {
		return fmt.Errorf("lower %s: %w", m.Name, err)
	}
	return nil
}

// View returns the function i of the Exec module m, which the conversion made, as a lowered function:
// with m and the variables of the function (prim.Attrs.Vars), which the conversion keeps.
func View(m *code.Module, i int) *Lowered {
	f := m.Funcs[i]
	return &Lowered{Module: m, Func: f, Vars: prim.AttrsOf(f).Vars}
}

// LoweredIR is the kind of a lowered function, which the passes of a target take and return: its
// module verifies and holds the function with its variables (Lowered.Keeps), and it is dumped as the
// listing of the function.
var LoweredIR = pipe.RegisterIR(&pipe.IR[*Lowered]{
	Name: "lowered@exec",
	Verify: func(l *Lowered) error {
		if err := Verify(l.Module); err != nil {
			return err
		}
		return l.Keeps()
	},
	Print: func(w io.Writer, l *Lowered) { fmt.Fprintf(w, "%s\n", code.FuncStringIn(l.Module, l.Func)) },
	Copy:  (*Lowered).Clone,
})

// LoweredFuncs views a module at the Exec stage as its functions, each a lowered function of the
// unit of that function (View), for the passes of a target. A pass that moved the function in its
// module, removed it or removed one of its variables fails to put it back.
var LoweredFuncs = pipe.RegisterFuncs(code.ExecStage, LoweredIR, &pipe.Funcs[code.At[code.Exec], *Lowered]{
	Name: "lowered-funcs@exec",
	Len:  func(u code.At[code.Exec]) int { return len(u.Module().Funcs) },
	Func: func(u code.At[code.Exec], i int) string { return u.Module().Funcs[i].Name },
	Get:  func(u code.At[code.Exec], i int) *Lowered { return View(u.Module().FuncUnit(i), 0) },
	Put: func(u code.At[code.Exec], i int, l *Lowered) (code.At[code.Exec], error) {
		if err := l.Keeps(); err != nil {
			return u, err
		}
		if l.Module.Funcs[0] != l.Func {
			return u, fmt.Errorf("an Exec pass moved the function %s in its module", l.Func.Name)
		}
		return u, u.Module().PutFuncUnit(i, l.Module)
	},
})

// Func returns the function of the use k in the module of the table t, which the conversion made, as
// a lowered function (View), or false if the table has none.
func Func(t *plandata.Table[code.Exec], k plandata.Key) (*Lowered, bool) {
	i, ok := t.Index(k)
	if !ok {
		return nil, false
	}
	return View(t.Module.Module(), i), true
}
