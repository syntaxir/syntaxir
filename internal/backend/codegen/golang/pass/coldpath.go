package pass

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PassColdPath is the name of the pass that splits the cold path of a function from its guard.
const PassColdPath = "go-cold-path"

// ColdPaths names, for the compositions whose form without budget charges the Go library prints as
// a guard that inlines into the generated code, the function that holds the rest of the composition.
var ColdPaths = map[string]string{"expect": "expectSlow"}

// Library is the pipeline of the Go Exec passes of the library's compositions. Its passes run on a
// specialized composition with the module that declares it; the rule bodies' pipeline is Pipeline.
var Library = pipe.Seq(execdata.LoweredIR, pipe.New(pipe.Def[*execdata.Lowered, *execdata.Lowered]{
	Decl: pipe.Decl{
		Name:     PassColdPath,
		Required: true,
		Obligation: "The guard returns when the function returns before its first effect, and otherwise calls the cold function " +
			"with the parameters, which runs the rest of the function: the pair does what the function does.",
	},
	In: execdata.LoweredIR, Out: execdata.LoweredIR,
	Run: func(_ *pipe.Ctx[*execdata.Lowered], l *execdata.Lowered) (*execdata.Lowered, pass.PreservedSet, error) {
		cold, ok := ColdPaths[l.Func.Name]
		if !ok || slices.Contains(l.Func.Features, exec.Budget) {
			return l, pass.PreserveAll(), nil
		}
		out, err := splitColdPath(l, cold)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return out, pass.PreserveNone(), nil
	},
}))

// splitColdPath returns l with its function cut after the leading guards, the statements that return
// at once, into the guard function, which calls the function cold with the guard function's parameters,
// and cold, which runs the rest of the body and is kept out of its callers (code.Func.NoInline) so that
// the guard stays small enough to inline. cold is added to the module after the other functions.
func splitColdPath(l *execdata.Lowered, cold string) (*execdata.Lowered, error) {
	f := l.Func
	n := 0
	for n < len(f.Body.Stmts) && isGuard(f.Body.Stmts[n]) {
		n++
	}
	if n == 0 || n == len(f.Body.Stmts) {
		return nil, fmt.Errorf("go cold path: %s has no guard followed by a body to split", f.Name)
	}
	if l.Module.Func(cold) != nil {
		return nil, fmt.Errorf("go cold path: the module declares %s already", cold)
	}
	guards := &code.Block{Stmts: slices.Clone(f.Body.Stmts[:n])}
	var used []string
	code.Walk(guards, nil, func(e code.Expr) {
		if x, ok := e.(*code.Local); ok && !slices.ContainsFunc(f.Params, func(p code.Param) bool { return p.Name == x.Name }) {
			used = append(used, x.Name)
		}
	})
	if len(used) > 0 {
		return nil, fmt.Errorf("go cold path: the guards of %s read the locals %v, which are no parameters and which the guard function does not declare", f.Name, used)
	}
	args := make([]code.Expr, len(f.Params))
	for i, p := range f.Params {
		args[i] = code.L(p.Name)
	}
	slow := &code.Func{
		Name: cold, Params: slices.Clone(f.Params), Results: slices.Clone(f.Results), Locals: slices.Clone(f.Locals),
		Features: slices.Clone(f.Features), Body: &code.Block{Stmts: slices.Clone(f.Body.Stmts[n:])},
		Doc: cold + " runs what " + f.Name + " does once its guards have not returned. The Go target splits it from " + f.Name + "\n" +
			"(the pass " + PassColdPath + ") so that the guards inline into the generated code.",
		NoInline: true, Derived: &code.Derived{Pass: PassColdPath, From: f.Name},
	}
	guard := *f
	guard.Locals = nil
	guard.Body = &code.Block{Label: f.Body.Label, Stmts: append(guards.Stmts, &code.Do{Call: &code.Call{Callee: cold, Args: args}})}
	m := l.Module.Clone()
	i := slices.Index(l.Module.Funcs, l.Func)
	if i < 0 {
		return nil, fmt.Errorf("go cold path: the module does not hold %s", f.Name)
	}
	m.Funcs[i] = &guard
	m.Funcs = append(m.Funcs, slow)
	return l.WithModule(m)
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
