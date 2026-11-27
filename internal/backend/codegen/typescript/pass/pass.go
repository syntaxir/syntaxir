// Package pass holds the TypeScript backend's Exec passes: the forms of a lowered function that the
// TypeScript printer prints more compactly than the operations they replace. Each pass makes
// operations of this target (package tsprim) that the printer prints, and the printer
// prints the operations they replace as well, so that each pass can be turned off by its flag.
package pass

import (
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/tsprim"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Names of the passes, which are also the names of their flags.
const (
	PassMergedRecords = "ts-merged-records"
	PassTestedInits   = "ts-tested-inits"
)

// Passes are the TypeScript Exec passes, in the order they run on a lowered function with its module;
// they change the function only.
var Passes = []*pipe.Pass[*execdata.Lowered, *execdata.Lowered]{
	pipe.New(pipe.Def[*execdata.Lowered, *execdata.Lowered]{
		Decl: pipe.Decl{
			Name:       PassMergedRecords,
			Obligation: "unisolateMerge ends the expectation record and merges its expectations into the enclosing record, as the end and the merge it replaces do.",
		},
		In: execdata.LoweredIR, Out: execdata.LoweredIR,
		Run: func(_ *pipe.Ctx[*execdata.Lowered], l *execdata.Lowered) (*execdata.Lowered, pass.PreservedSet, error) {
			mergedRecords(l)
			return l, pass.PreserveNone(), nil
		},
	}),
	pipe.New(pipe.Def[*execdata.Lowered, *execdata.Lowered]{
		Decl: pipe.Decl{
			Name:       PassTestedInits,
			Obligation: "The condition reports that the call of the If's Init failed, as the negation of the bool the Init sets does.",
		},
		In: execdata.LoweredIR, Out: execdata.LoweredIR,
		Run: func(_ *pipe.Ctx[*execdata.Lowered], l *execdata.Lowered) (*execdata.Lowered, pass.PreservedSet, error) {
			testedInits(l)
			return l, pass.PreserveNone(), nil
		},
	}),
}

// Pipeline is the pipeline of the TypeScript Exec passes.
var Pipeline = pipe.Seq(execdata.LoweredIR, Passes...)

func init() {
	pass.Register(
		pass.Flag{Name: PassMergedRecords, Kind: pass.KindExecPass, Default: 1, Doc: "the end of an expectation record and the merge of its expectations are one call (TypeScript)",
			Tradeoff:  "Saves a copy of the expectations and a local per merge",
			Mechanism: "G240", Record: 143, Backends: pass.On(pass.BackendTypeScript),
			Witnesses: []string{"recovery"}},
		pass.Flag{Name: PassTestedInits, Kind: pass.KindExecPass, Default: 1, Doc: "an `if` that tests whether its first call matched tests the call's result in its condition (TypeScript)",
			Tradeoff:  "Saves the boolean local that the call sets",
			Mechanism: "G240", Record: 143, Backends: pass.On(pass.BackendTypeScript),
			Witnesses: []string{"recovery"}},
	)
}

// addOp declares the primitive operation o of the target in the module of l.
func addOp(l *execdata.Lowered, o *code.Op) {
	o.Component, o.Dialect, o.Kind = prim.Component, execlower.Dialect, code.OpPrimitive
	l.Module.AddOp(o)
}

// rewrite replaces each statement list of the function of l, at any depth, with what f makes of it,
// inner lists first.
func rewrite(l *execdata.Lowered, f func([]code.Stmt) []code.Stmt) {
	var block func(ss []code.Stmt) []code.Stmt
	block = func(ss []code.Stmt) []code.Stmt {
		for _, s := range ss {
			switch s := s.(type) {
			case *code.Block:
				s.Stmts = block(s.Stmts)
			case *code.Loop:
				s.Body.Stmts = block(s.Body.Stmts)
			case *code.If:
				s.Then.Stmts = block(s.Then.Stmts)
				if s.Else != nil {
					s.Else.Stmts = block(s.Else.Stmts)
				}
			}
		}
		return f(ss)
	}
	l.Func.Body.Stmts = block(l.Func.Body.Stmts)
}

// mergedRecords replaces each end of an expectation record that a merge of its expectations follows
// with one call of tsprim.MergeOp.
func mergedRecords(l *execdata.Lowered) {
	addOp(l, &code.Op{Name: tsprim.MergeOp, Params: []code.Param{code.P("m", code.THandle(prim.TExpMark))},
		Reads: []string{prim.Component}, Writes: []string{prim.Component}, NoReturn: true,
		Contract: "Ends the expectation record m and merges its expectations into the enclosing record."})
	rewrite(l, func(ss []code.Stmt) []code.Stmt {
		var out []code.Stmt
		for i := 0; i < len(ss); i++ {
			if i+1 < len(ss) {
				if end, ok := mergedRecord(ss[i], ss[i+1]); ok {
					out = append(out, &code.Do{Call: &code.Call{Callee: tsprim.MergeOp, Args: end.Args}})
					i++
					continue
				}
			}
			out = append(out, ss[i])
		}
		return out
	})
}

// testedInits replaces the condition of each If that tests the call of its Init with a call of
// tsprim.FailedOp, so that nothing else reads the bool the Init sets.
func testedInits(l *execdata.Lowered) {
	addOp(l, &code.Op{Name: tsprim.FailedOp, Results: []code.Type{code.TBool}, Reads: []string{prim.Component},
		Imm: "the bool the Init sets", Contract: "Reports that the call of the Init of the If failed: that the bool it sets is false."})
	rewrite(l, func(ss []code.Stmt) []code.Stmt {
		for _, s := range ss {
			if s, ok := s.(*code.If); ok {
				if flag, ok := testedFlag(s); ok {
					s.Cond = &code.Call{Callee: tsprim.FailedOp, Imm: tsprim.Failed{Flag: flag}}
				}
			}
		}
		return ss
	})
}

// testedFlag reports whether the If s tests that the call of its Init fails, and returns the variable
// of the bool that the Init sets.
func testedFlag(s *code.If) (string, bool) {
	if s.Init == nil || len(s.Init.Dst) != 2 || len(s.Init.Values) != 1 {
		return "", false
	}
	c, ok := s.Init.Values[0].(*code.Call)
	if !ok || !tsprim.TwoResults(c.Callee) {
		return "", false
	}
	flag := s.Init.Dst[1]
	not, ok := s.Cond.(*code.Unary)
	if !ok || not.Op != "!" || flag.Reg || flag.Name == "" {
		return "", false
	}
	if v, ok := not.X.(*code.Local); !ok || v.Name != flag.Name {
		return "", false
	}
	return flag.Name, true
}

// mergedRecord reports whether a and b end an expectation record and merge its expectations into the
// enclosing record, and returns the call that ends the record.
func mergedRecord(a, b code.Stmt) (*code.Call, bool) {
	set, ok := a.(*code.Set)
	if !ok || len(set.Dst) != 2 || len(set.Values) != 1 {
		return nil, false
	}
	c, ok := set.Values[0].(*code.Call)
	if !ok || c.Callee != prim.OpUnisolate {
		return nil, false
	}
	do, ok := b.(*code.Do)
	if !ok || do.Call.Callee != prim.OpMergeExpected {
		return nil, false
	}
	far, ids := set.Dst[0], set.Dst[1]
	f, fok := do.Call.Args[0].(*code.Local)
	e, eok := do.Call.Args[1].(*code.Local)
	if !fok || !eok || far.Reg || ids.Reg || far.Name == "" || ids.Name == "" || f.Name != far.Name || e.Name != ids.Name {
		return nil, false
	}
	return c, true
}
