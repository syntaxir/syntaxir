package execlower

import (
	"fmt"
	"strconv"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// Expectations returns the expectation table of a parser that runs the tables, at the Exec stage,
// in order: the fixed expectations (core.FixedDescs), then each expectation that a Flow operation
// of a function records, the first time it is recorded, in the order of the tables, of the
// functions of each table in plan order and of their code. A statement comes before the next one,
// the condition of an If or a Loop before its blocks, and the arguments of a call before the call.
// The table is a fact of the lowered tables, numbered before any table is printed; every function of
// a planned table is one that the backend prints.
//
// The operations that record expectations are flow.expect (a message, or an expectation that is
// not fixed), flow.mergeRenamed, flow.matchLit (the empty literal records the quoted empty text),
// flow.matchClass and flow.skipClasses. A terminal program (flow.skipTerminal) numbers the
// expectations of its grammar expression as it is printed, so a table that runs one is not numbered
// here and Expectations returns an error.
func Expectations(tables ...*plandata.Table[code.Exec]) ([]string, error) {
	descs := append([]string(nil), core.FixedDescs...)
	seen := make(map[string]bool, len(descs))
	for _, d := range descs {
		seen[d] = true
	}
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			descs = append(descs, d)
		}
	}
	for _, t := range tables {
		exps := t.IR.M.Exps
		var err error
		var expr func(code.Expr)
		expr = func(e code.Expr) {
			switch e := e.(type) {
			case *code.Unary:
				expr(e.X)
			case *code.Binary:
				expr(e.X)
				expr(e.Y)
			case *code.Convert:
				expr(e.X)
			case *code.Call:
				expr(e.Fn)
				for _, a := range e.Args {
					expr(a)
				}
				switch FlowOp(e.Callee) {
				case flow.OpExpect:
					if x := e.Imm.(prim.Expectation); x.Message || int(x.Exp) >= len(core.FixedDescs) {
						add(exps[x.Exp])
					}
				case flow.OpMergeRenamed:
					add(exps[e.Imm.(core.ExpID)])
				case flow.OpMatchLit:
					if lit := e.Imm.(*core.Lit); lit.Text == "" && !lit.Fold {
						add(strconv.Quote(""))
					} else {
						add(exps[lit.Exp])
					}
				case flow.OpMatchClass:
					add(exps[e.Imm.(*core.Class).Exp])
				case flow.OpSkipClasses:
					for _, c := range e.Imm.(*core.ClassRun).Classes {
						add(exps[c.Exp])
					}
				case flow.OpSkipTerminal:
					if err == nil {
						err = fmt.Errorf("internal error: the expectations of a terminal program are numbered as it is printed")
					}
				}
			}
		}
		set := func(s *code.Set) {
			if s != nil {
				for _, v := range s.Values {
					expr(v)
				}
			}
		}
		var block func(*code.Block)
		block = func(b *code.Block) {
			if b == nil {
				return
			}
			for _, s := range b.Stmts {
				switch s := s.(type) {
				case *code.Block:
					block(s)
				case *code.Loop:
					set(s.Init)
					expr(s.Cond)
					set(s.Post)
					block(s.Body)
				case *code.If:
					set(s.Init)
					expr(s.Cond)
					block(s.Then)
					block(s.Else)
				case *code.Let:
					expr(s.Value)
				case *code.Set:
					set(s)
				case *code.Do:
					expr(s.Call)
				case *code.Return:
					for _, v := range s.Values {
						expr(v)
					}
				case *code.Protect:
					block(s.Body)
					block(s.Finally)
				case *code.Assert:
					expr(s.Cond)
				}
			}
		}
		for _, f := range t.Module.Module().Funcs {
			block(f.Body)
		}
		if err != nil {
			return nil, err
		}
	}
	return descs, nil
}
