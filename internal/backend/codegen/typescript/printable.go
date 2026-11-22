package typescript

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen/typescript/tsprim"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The printability of the TypeScript tables. The TypeScript printer prints a subset of grammar code
// at the Exec stage, and the step ts-printable, which runs right before print-ts, is the one place
// that decides whether a table is in it. Each function of grammar code of every table must have four
// properties:
//
//   - every call is of a primitive that the printer prints (tsprim.Printed);
//   - every predicate and bind reads the captures of the frame, not variables;
//   - the function reads neither its start nor the context of its action: the printer declares
//     neither;
//   - every variable has a type that the printer declares (tsprim.Declared).
//
// The planning and the lowering keep them for every grammar: a capture read from variables exists
// only in a flat function, which the plan gives no predicate and no value (flowplan.RecEligible); the
// primitives and types outside the sets come from the typed table, from the forms of the Flow passes
// that only Go prints (FlowTarget has none) and from streaming, none of which TypeScript generates. So
// a table without them is an internal error, and the printer, which prints only what this analysis
// admitted, panics on anything else.

// Printability is the step ts-printable: it fails unless every function of grammar code of the tables
// has the properties above, and leaves the tables as they are.
var Printability = pipe.New(pipe.Def[*execdata.Tables, *execdata.Tables]{
	Decl: pipe.Decl{Name: "ts-printable", Required: true,
		Obligation: "Every function of grammar code of the tables is one that the TypeScript printer prints."},
	In: execdata.TablesIR, Out: execdata.TablesIR,
	Run: func(_ *pipe.Ctx[*execdata.Tables], t *execdata.Tables) (*execdata.Tables, pass.PreservedSet, error) {
		return t, pass.PreserveAll(), printable(t)
	},
})

// printable returns an error naming the first function of grammar code of the tables t, in the order
// of the tables and their functions, that lacks a property of printability, and the property.
func printable(t *execdata.Tables) error {
	for i, tab := range t.Tables {
		for _, f := range tab.Module.Module().Funcs {
			if prim.AttrsOf(f) == nil {
				continue
			}
			if what := unprintable(f); what != "" {
				return fmt.Errorf("the TypeScript printer does not print %s in the function %s of the table %s", what, f.Name, t.Names[i])
			}
		}
	}
	return nil
}

// unprintable returns the first construct of the function f of grammar code that the TypeScript
// printer does not print, or "".
func unprintable(f *code.Func) string {
	a := prim.AttrsOf(f)
	for _, v := range prim.VarsOf(f) {
		if t := prim.TypeOf(v.Type); !tsprim.Declared(t) {
			return fmt.Sprintf("the variable %s of the type %s", v.Name, t)
		}
	}
	what := ""
	code.Walk(f.Body, nil, func(e code.Expr) {
		if what != "" {
			return
		}
		switch e := e.(type) {
		case *code.Call:
			if !tsprim.Printed(e.Callee) {
				what = "the operation " + e.Callee
				return
			}
			switch x := e.Imm.(type) {
			case prim.Predicate:
				if !x.Frame {
					what = "a predicate that reads variables"
				}
			case prim.Binding:
				if !x.Frame {
					what = "a bind that reads variables"
				}
			}
		case *code.Local:
			switch {
			case a.Start != "" && e.Name == a.Start:
				what = "a read of the start of the rule"
			case a.Ctx != "" && e.Name == a.Ctx:
				what = "a read of the context of the rule's action"
			}
		}
	})
	return what
}
