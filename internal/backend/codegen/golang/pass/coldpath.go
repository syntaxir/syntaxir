package pass

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PassColdPath is the name of the pass that splits the cold path of a function from its guard.
const PassColdPath = "go-cold-path"

// ColdPath is the step go-cold-path of the pipeline of the Go library: it splits the function of each
// request that asks for a cold function (library.Request.Cold) into the guard, which keeps the
// request's name, and the cold function of that name, which it adds to the library as made of that
// request (library.Made).
var ColdPath = pipe.New(pipe.Def[*library.Library, *library.Library]{
	Decl: pipe.Decl{
		Name:     PassColdPath,
		Required: true,
		Obligation: "The guard returns when the function returns before its first effect, and otherwise calls the cold function " +
			"with the parameters, which runs the rest of the function: the pair does what the function does.",
	},
	In: library.IR, Out: library.IR,
	Run: func(_ *pipe.Ctx[*library.Library], l *library.Library) (*library.Library, pass.PreservedSet, error) {
		if l.Funcs == nil {
			return nil, pass.PreserveNone(), fmt.Errorf("go cold path: the library is not specialized")
		}
		out := *l
		out.Funcs = slices.Clone(l.Funcs)
		out.Made = slices.Clone(l.Made)
		changed := false
		for i, r := range l.Requests {
			f := l.Funcs[i]
			cold := r.Cold
			if cold == "" {
				continue
			}
			if l.Module.Func(cold) != nil || slices.ContainsFunc(out.Made, func(m library.Made) bool {
				return l.Requests[m.Request].File == r.File && m.Func.Name == cold
			}) {
				return nil, pass.PreserveNone(), fmt.Errorf("go cold path: the library declares %s already for %q", cold, r.File)
			}
			guard, slow, err := splitColdPath(f, r.Fn, cold)
			if err != nil {
				return nil, pass.PreserveNone(), err
			}
			out.Funcs[i] = guard
			out.Made = append(out.Made, library.Made{Request: i, Func: slow})
			changed = true
		}
		if !changed {
			return l, pass.PreserveAll(), nil
		}
		return &out, pass.PreserveNone(), nil
	},
})

// splitColdPath returns the function f, the specialization of the composition fn, cut after its
// leading guards, the statements that return at once, into the guard function, which calls the
// function cold with the guard function's parameters, and cold, which runs the rest of the body and is
// kept out of its callers (code.Func.NoInline) so that the guard stays small enough to inline.
func splitColdPath(f *code.Func, fn, cold string) (guard, slow *code.Func, err error) {
	n := 0
	for n < len(f.Body.Stmts) && isGuard(f.Body.Stmts[n]) {
		n++
	}
	if n == 0 || n == len(f.Body.Stmts) {
		return nil, nil, fmt.Errorf("go cold path: %s has no guard followed by a body to split", f.Name)
	}
	guards := &code.Block{Stmts: slices.Clone(f.Body.Stmts[:n])}
	var used []string
	code.Walk(guards, nil, func(e code.Expr) {
		if x, ok := e.(*code.Local); ok && !slices.ContainsFunc(f.Params, func(p code.Param) bool { return p.Name == x.Name }) {
			used = append(used, x.Name)
		}
	})
	if len(used) > 0 {
		return nil, nil, fmt.Errorf("go cold path: the guards of %s read the locals %v, which are no parameters and which the guard function does not declare", f.Name, used)
	}
	args := make([]code.Expr, len(f.Params))
	for i, p := range f.Params {
		args[i] = code.L(p.Name)
	}
	slow = &code.Func{
		Name: cold, Params: slices.Clone(f.Params), Results: slices.Clone(f.Results), Locals: slices.Clone(f.Locals),
		Features: slices.Clone(f.Features), Body: &code.Block{Stmts: slices.Clone(f.Body.Stmts[n:])},
		Doc: cold + " runs what " + fn + " does once its guards have not returned. The Go target splits it from " + fn + "\n" +
			"(the pass " + PassColdPath + ") so that the guards inline into the generated code.",
		NoInline: true, Derived: &code.Derived{Pass: PassColdPath, From: fn},
	}
	g := *f
	g.Locals = nil
	g.Body = &code.Block{Label: f.Body.Label, Stmts: append(guards.Stmts, &code.Do{Call: &code.Call{Callee: cold, Args: args}})}
	return &g, slow, nil
}

// isGuard reports whether s returns without values when its condition holds and does nothing else.
func isGuard(s code.Stmt) bool {
	i, ok := s.(*code.If)
	if !ok || i.Init != nil || i.Else != nil || i.Then == nil || len(i.Then.Stmts) != 1 {
		return false
	}
	r, ok := i.Then.Stmts[0].(*code.Return)
	return ok && len(r.Values) == 0
}
