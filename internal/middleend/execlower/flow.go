// Package execlower converts grammar code from the Flow stage of Code IR to the Exec stage (design
// 065), so that the backends of the Exec stage print the rules of a grammar as they print the
// compositions of the abstract machine.
//
// The conversion keeps the structure of each function (blocks, loops, branches, exits, assignments
// and the arithmetic and logic of conditions), its locals and its registers. Each call of a Flow
// operation becomes a call of the primitive of the Exec stage that stands for it (Prim, the names of
// package prim): the same parameters, results and immediate, of the Exec dialect and the component
// prim.Component, which each target prints in place in its own form. The Flow attributes of each
// function become its Exec-stage attributes (prim.Attrs). The result is checked by verifying it at
// the Exec stage, where an operation of the Flow dialect and a Flow attribute are not legal.
package execlower

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
)

// Stage is the name of the Exec stage of Code IR, and Dialect the name of its dialect.
const (
	Stage   = "exec"
	Dialect = "exec"
)

// primPrefix begins the names of the primitives.
const primPrefix = prim.Component + "."

// Prim returns the name of the primitive of the Exec stage that stands for the Flow operation name
// ("flow.matchLit" -> "runtime.matchLit", prim.OpMatchLit).
func Prim(name string) string { return primPrefix + strings.TrimPrefix(name, flow.Dialect+".") }

// FlowOp returns the name of the Flow operation that the primitive operation name stands for, or ""
// if name is not a primitive operation.
func FlowOp(name string) string {
	if !strings.HasPrefix(name, primPrefix) {
		return ""
	}
	return flow.Dialect + "." + strings.TrimPrefix(name, primPrefix)
}

// Lower converts the module m from the Flow stage to the Exec stage. The functions of m move to the
// result with their Exec-stage attributes, and the result declares the registers and handle types of
// m and the primitive of each Flow operation; m is not used after. The result is verified (Verify),
// which checks that no call of a Flow operation and no Flow attribute remains.
func Lower(m *code.Module) (*code.Module, error) {
	out, err := lower(m)
	if err == nil {
		err = execdata.Verify(out)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// lower is Lower without the verification of the result.
func lower(m *code.Module) (*code.Module, error) {
	if m.Stage != flow.Stage {
		return nil, fmt.Errorf("lower %s: a module at the %q stage, not the Flow stage", m.Name, m.Stage)
	}
	out := code.NewModule(m.Name, m.Features...)
	out.Dialects = []string{Dialect, "action"}
	code.EnterExec(out)
	out.Attrs = m.Attrs
	out.AddComponent(prim.Component, "The state of the runtime that the target implements.")
	out.Registers = m.Registers
	for _, h := range m.Handles {
		out.AddHandle(h.Name, prim.Component, h.Doc)
	}
	for _, o := range m.Ops {
		if o.Dialect != flow.Dialect {
			out.AddOp(o)
			continue
		}
		p, err := PrimOp(o)
		if err != nil {
			return nil, fmt.Errorf("lower %s: %w", m.Name, err)
		}
		out.AddOp(p)
	}
	for _, f := range m.Funcs {
		a, err := attrs(f)
		if err != nil {
			return nil, fmt.Errorf("lower %s: %w", m.Name, err)
		}
		f.Attrs = a
		code.Walk(f.Body, nil, func(e code.Expr) {
			if c, ok := e.(*code.Call); ok && c.Fn == nil && strings.HasPrefix(c.Callee, flow.Dialect+".") {
				c.Callee = Prim(c.Callee)
			}
		})
		out.Funcs = append(out.Funcs, f)
	}
	m.Funcs = nil
	return out, nil
}

// PrimOp returns the primitive of the Exec stage that stands for the Flow operation o: its signature,
// contract, cost, laws and immediate, with the state component of the runtime in place of the Flow
// component. An operation without a primitive in package prim, or whose immediate is not of the type
// of the primitive's, is an error.
func PrimOp(o *code.Op) (*code.Op, error) {
	name := Prim(o.Name)
	if t, ok := prim.ImmType(name); !ok {
		return nil, fmt.Errorf("the Flow operation %s has no primitive", o.Name)
	} else if t != o.ImmType {
		return nil, fmt.Errorf("the immediate of the Flow operation %s is a %v, of its primitive a %v", o.Name, o.ImmType, t)
	}
	p := *o
	p.Name, p.Dialect, p.Component, p.Kind = name, Dialect, prim.Component, code.OpPrimitive
	state := func(names []string) []string {
		out := slices.Clone(names)
		for i, n := range out {
			if n == flow.State {
				out[i] = prim.Component
			}
		}
		return out
	}
	p.Reads, p.Writes = state(o.Reads), state(o.Writes)
	p.Params, p.Results = slices.Clone(o.Params), slices.Clone(o.Results)
	return &p, nil
}

// attrs returns the Exec-stage attributes of the function f at the Flow stage: every Flow attribute
// but the record of the forms, which the end of the Flow passes (flowpass.Finishing) consumes, so a
// function that still has forms is an error. A function without Flow attributes has none.
func attrs(f *code.Func) (*prim.Attrs, error) {
	if f.Attrs == nil {
		return nil, nil
	}
	a, ok := f.Attrs.(*flow.Attrs)
	if !ok {
		return nil, fmt.Errorf("%s: attributes of the type %T at the Flow stage, not %T", f.Name, f.Attrs, a)
	}
	if a.Forms != nil {
		return nil, fmt.Errorf("%s: the forms of the lowering are left; the Flow passes end by removing them", f.Name)
	}
	return &prim.Attrs{
		Rule: a.Rule, Vars: a.Vars, Start: a.Start, Result: a.Result, Ctx: a.Ctx, Fail: a.Fail, Cut: a.Cut,
		Binds: a.Binds, Frame: a.Frame, Node: a.Node, Document: a.Document,
		Traced: a.Traced,
	}, nil
}
