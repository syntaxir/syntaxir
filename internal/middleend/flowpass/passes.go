package flowpass

import (
	"fmt"
	"slices"
	"strconv"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Names of the passes. A name is also the name of the flag that turns the pass off; the passes that
// take a number (class-tables, inline-literals) are off at 0.
const (
	PassTerminals      = "terminals"
	PassGuardedClasses = "guarded-classes"
	PassClassRuns      = "class-runs"
	PassClassTables    = "class-tables"
	PassInlineLiterals = "inline-literals"
)

// guardsFlag is the registry flag of the Core pass guards (corepass.PassGuards), which guarded-classes
// reads: the guards of the alternatives are those of that pass, when it runs. The Flow passes read the
// flag by its name in the registry rather than import the Core passes, which would link Core IR.
const guardsFlag = "guards"

// ordinary reports a function printed in the ordinary form: without Documents or the records of a
// traced parse, each of which records the input a read examines.
func ordinary(a *flow.Attrs) bool { return !a.Document && !a.Traced }

// leanTyped reports a function of typed rules printed in the ordinary form.
func leanTyped(a *flow.Attrs) bool { return !a.Node && ordinary(a) }

// Passes are the Flow passes, in the order they run (Pipeline): the passes of forms, and then
// canonicalize, which applies the local rewrite patterns of the stage to every function
// (codepass.PassCanonicalize).
var Passes = append(slices.Clone(FormPasses), codepass.FlowCanonicalize)

// FormPasses are the passes of forms, in the order they run. Each replaces the statements of forms
// the lowering wrote (flow.Forms) and marks them replaced, so that a later pass leaves them alone. A
// pass applies to a function by the attributes of the function (FuncAttrs), the forms the target
// prints (TargetFeature) and the observation of the parser (ObserveFeature).
var FormPasses = []*pipe.Pass[Unit, Unit]{
	pipe.New(pipe.Def[Unit, Unit]{
		Decl: pipe.Decl{
			Name:       PassTerminals,
			Obligation: "skipTerminal consumes the same input and records the same expectations as the region it replaces, and charges nothing, as the region's literals and classes charge nothing in the ordinary form.",
			Features:   []pipe.FeatureID{TargetFeature.ID(), ObserveFeature.ID()},
		},
		In: code.FlowStage, Out: code.FlowStage,
		// A terminal program reports no rule and no skipped alternative, and charges nothing.
		Applies: func(c *pipe.Ctx[Unit]) bool {
			return pipe.Feature(c, TargetFeature).Terminals && !pipe.Feature(c, ObserveFeature) &&
				funcIs(c, func(a *flow.Attrs) bool { return !a.Frame && leanTyped(a) })
		},
		Run: func(c *pipe.Ctx[Unit], u Unit) (Unit, pass.PreservedSet, error) {
			return run(PassTerminals, func(fn *code.Func, a *flow.Attrs, c *pipe.Ctx[Unit]) {
				terminals(fn, a, prim.TablesOf(u.Module()), cancel.New(c.Context()))
			})(c, u)
		},
	}),
	pipe.New(pipe.Def[Unit, Unit]{
		Decl: pipe.Decl{
			Name:       PassGuardedClasses,
			After:      []string{PassTerminals},
			Obligation: "The class that begins a guarded alternative consumes the character that the guard's peek read and tested with the class's own test.",
			Features:   []pipe.FeatureID{TargetFeature.ID()},
			// The guards of the alternatives are those of the Core pass guards, when it runs.
			Params: []string{guardsFlag},
		},
		In: code.FlowStage, Out: code.FlowStage,
		Applies: func(c *pipe.Ctx[Unit]) bool {
			return pipe.Feature(c, TargetFeature).GuardedClasses && c.Value(guardsFlag) != 0 && funcIs(c, ordinary)
		},
		Run: run(PassGuardedClasses, func(fn *code.Func, a *flow.Attrs, c *pipe.Ctx[Unit]) { guardedClasses(fn, a, cancel.New(c.Context())) }),
	}),
	pipe.New(pipe.Def[Unit, Unit]{
		Decl: pipe.Decl{
			Name:       PassClassRuns,
			After:      []string{PassGuardedClasses},
			Obligation: "skipClasses consumes the same input and records the same expectations as the consecutive classes it replaces; a class that consumes a guard's character is not one of them.",
			Features:   []pipe.FeatureID{TargetFeature.ID()},
		},
		In: code.FlowStage, Out: code.FlowStage,
		Applies: func(c *pipe.Ctx[Unit]) bool {
			return pipe.Feature(c, TargetFeature).ClassRuns && funcIs(c, leanTyped)
		},
		Run: run(PassClassRuns, func(fn *code.Func, a *flow.Attrs, c *pipe.Ctx[Unit]) { classRuns(fn, a, cancel.New(c.Context())) }),
	}),
	pipe.New(pipe.Def[Unit, Unit]{
		Decl: pipe.Decl{
			Name:       PassClassTables,
			After:      []string{PassClassRuns},
			Obligation: "matchClass consumes the same character and records the same expectation as the class's peek and test.",
			Features:   []pipe.FeatureID{TargetFeature.ID()},
			Params:     []string{PassClassTables},
		},
		In: code.FlowStage, Out: code.FlowStage,
		Applies: func(c *pipe.Ctx[Unit]) bool {
			return pipe.Feature(c, TargetFeature).ClassTables && c.Value(PassClassTables) > 0 && funcIs(c, leanTyped)
		},
		Run: func(c *pipe.Ctx[Unit], u Unit) (Unit, pass.PreservedSet, error) {
			t := prim.TablesOf(u.Module())
			return run(PassClassTables, func(fn *code.Func, a *flow.Attrs, c *pipe.Ctx[Unit]) {
				classTables(fn, a, t, c.Value(PassClassTables), cancel.New(c.Context()))
			})(c, u)
		},
	}),
	pipe.New(pipe.Def[Unit, Unit]{
		Decl: pipe.Decl{
			Name:       PassInlineLiterals,
			Obligation: "A literal compared in place (hasLit) consumes what matchLit consumes; matchLit stays the alternative that records the expectation when they differ.",
			Features:   []pipe.FeatureID{TargetFeature.ID()},
			Params:     []string{PassInlineLiterals},
		},
		In: code.FlowStage, Out: code.FlowStage,
		Applies: func(c *pipe.Ctx[Unit]) bool {
			return pipe.Feature(c, TargetFeature).InlineLiterals && c.Value(PassInlineLiterals) > 0 &&
				funcIs(c, func(a *flow.Attrs) bool { return !a.Traced })
		},
		Run: run(PassInlineLiterals, func(fn *code.Func, _ *flow.Attrs, c *pipe.Ctx[Unit]) {
			inlineLiterals(fn, c.Value(PassInlineLiterals), cancel.New(c.Context()))
		}),
	}),
}

// funcIs reports whether the function of the unit of c has attributes that make p hold: false for a
// function without attributes, which no pass applies to.
func funcIs(c *pipe.Ctx[Unit], p func(*flow.Attrs) bool) bool {
	a, err := pipe.Query(c, FuncAttrs)
	return err == nil && a != nil && p(a)
}

// run adapts the pass name over the function of a unit, which it changes in place. A function of a
// suspended stream is refused: a form pass replaces the statements of its forms, which carry the
// demands of the reads (flow.inputDemand), and no form pass keeps them yet.
func run(name string, f func(fn *code.Func, a *flow.Attrs, c *pipe.Ctx[Unit])) func(*pipe.Ctx[Unit], Unit) (Unit, pass.PreservedSet, error) {
	return func(c *pipe.Ctx[Unit], u Unit) (Unit, pass.PreservedSet, error) {
		fn := flow.FuncOf(u.Module())
		a := flow.AttrsOf(fn)
		if a == nil || a.Forms == nil {
			return u, pass.PreserveAll(), nil
		}
		if a.Suspend {
			return u, pass.PreserveNone(), fmt.Errorf("flowpass: pass %s is not demand-aware and cannot change function %s of a suspended stream", name, fn.Name)
		}
		f(fn, a, c)
		return u, pass.PreserveNone(), nil
	}
}

// failIf returns the statement that leaves the form on failure when cond holds.
func failIf(form *flow.Form, cond code.Expr) code.Stmt {
	return &code.If{Cond: cond, Then: &code.Block{Stmts: []code.Stmt{form.FailJump()}}}
}

// terminals matches each terminal region of a local function of typed rules in the ordinary form as
// one program, the region's expression assembled with the tables t (prim.Assemble). A region whose
// program does not fit the words of its code keeps its statements.
func terminals(fn *code.Func, a *flow.Attrs, t *prim.Tables, control *cancel.Control) {
	for _, r := range a.Forms.Regions {
		control.Poll()
		if !r.Live() || r.Build {
			continue
		}
		p, ok := prim.Assemble(r.Terminal, t)
		if !ok {
			continue
		}
		r.Block.Stmts = []code.Stmt{failIf(r, code.Not(&code.Call{Callee: flow.OpSkipTerminal, Imm: p}))}
		r.By = PassTerminals
	}
}

// guardedClasses keeps the size of the character that the guards of a choice peek, and lets the
// class that begins a guarded alternative consume it.
func guardedClasses(fn *code.Func, a *flow.Attrs, control *cancel.Control) {
	sizes := map[*flow.ChoiceForm]string{}
	for _, c := range a.Forms.Choices {
		control.Poll()
		if !c.Live() {
			continue
		}
		size := newVar(fn, a, prim.TypeInt, c.Event)
		sizes[c] = size
		for _, p := range c.Peeks {
			p.Dst[1] = code.Place{Name: size}
		}
	}
	for _, cl := range a.Forms.Classes {
		control.Poll()
		size, ok := sizes[cl.Guard]
		if !ok || !cl.Live() {
			continue
		}
		// The peek and the test go; the advance consumes the guard's character.
		rest := cl.Block.Stmts[2:]
		for _, st := range rest {
			if d, ok := st.(*code.Do); ok && d.Call.Callee == flow.OpAdvance {
				d.Call.Args = []code.Expr{code.L(size)}
			}
		}
		cl.Block.Stmts = rest
		cl.By = PassGuardedClasses
	}
}

// classRuns matches the consecutive classes of a value-free sequence as one.
func classRuns(fn *code.Func, a *flow.Attrs, control *cancel.Control) {
	for _, s := range a.Forms.Seqs {
		control.Poll()
		if !s.Live() {
			continue
		}
		for i := 0; i < len(s.Items); i++ {
			n := 0
			for i+n < len(s.Items) && s.Items[i+n] != nil && s.Items[i+n].Live() {
				n++
			}
			if n < 2 {
				continue
			}
			run := make(prim.ClassRun, 0, n)
			for _, cl := range s.Items[i : i+n] {
				run = append(run, cl.Class)
				cl.Block.Stmts = nil
				cl.By = PassClassRuns
			}
			first := s.Items[i]
			first.Block.Stmts = []code.Stmt{failIf(first, code.Not(&code.Call{Callee: flow.OpSkipClasses, Imm: run}))}
			i += n - 1
		}
	}
}

// classTables matches each class of at least the pass's number of ranges through its membership
// table, in a function of typed rules in the ordinary form; t are the tables of its module.
func classTables(fn *code.Func, a *flow.Attrs, t *prim.Tables, min int, control *cancel.Control) {
	for _, cl := range a.Forms.Classes {
		control.Poll()
		if !cl.Live() || len(t.Class(cl.Class).Ranges) < min {
			continue
		}
		match := failIf(cl, code.Not(&code.Call{Callee: flow.OpMatchClass, Imm: cl.Class}))
		st := cl.Block.Stmts
		if cl.Build {
			// peek, test, start = pos, advance, value: the start is taken before the match.
			cl.Block.Stmts = []code.Stmt{st[2], match, st[4]}
		} else {
			cl.Block.Stmts = []code.Stmt{match}
		}
		cl.By = PassClassTables
	}
}

// inlineLiterals compares each exact literal of at most the pass's number of code points in place
// first, outside the traced copies, which read every literal through the operation that records it.
func inlineLiterals(fn *code.Func, max int, control *cancel.Control) {
	var stmts func(list []code.Stmt)
	block := func(b *code.Block) {
		if b != nil {
			stmts(b.Stmts)
		}
	}
	stmts = func(list []code.Stmt) {
		for i, st := range list {
			control.Poll()
			switch st := st.(type) {
			case *code.Block:
				block(st)
			case *code.Loop:
				block(st.Body)
			case *code.Protect:
				block(st.Body)
				block(st.Finally)
			case *code.If:
				if lit, k := inlineLiteral(st, max); k > 0 {
					list[i] = &code.If{Cond: &code.Call{Callee: flow.OpHasLit, Imm: lit},
						Then: &code.Block{Stmts: []code.Stmt{&code.Do{Call: &code.Call{Callee: flow.OpAdvance, Args: []code.Expr{code.Int(int64(k))}}}}},
						Else: &code.Block{Stmts: []code.Stmt{st}}}
					continue
				}
				block(st.Then)
				block(st.Else)
			}
		}
	}
	block(fn.Body)
}

// inlineLiteral returns the literal that the If st matches, as the lowering writes a literal
// (if _, ok = matchLit(lit); !ok { fail }), and its number of code points, if it is exact and has at
// most max of them; otherwise the number is 0.
func inlineLiteral(st *code.If, max int) (lit prim.Lit, k int) {
	if st.Init == nil || len(st.Init.Values) != 1 || st.Else != nil {
		return lit, 0
	}
	c, ok := st.Init.Values[0].(*code.Call)
	if !ok || c.Callee != flow.OpMatchLit {
		return lit, 0
	}
	lit, ok = c.Imm.(prim.Lit)
	if !ok || lit.Fold {
		return lit, 0
	}
	if k := len([]rune(lit.Text)); k > 0 && k <= max {
		return lit, k
	}
	return lit, 0
}

// newVar makes a variable of the type t that the lowering would have made at the event at, had it
// written the form a pass chose; Finish places it there.
func newVar(fn *code.Func, a *flow.Attrs, t prim.Type, at *flow.Event) string {
	a.Forms.N++
	name := "x" + strconv.Itoa(a.Forms.N)
	fn.Locals = append(fn.Locals, code.P(name, prim.CodeType(t)))
	a.Vars = append(a.Vars, name)
	if a.Forms.Placed == nil {
		a.Forms.Placed = map[string]*flow.Event{}
	}
	a.Forms.Placed[name] = at
	return name
}
