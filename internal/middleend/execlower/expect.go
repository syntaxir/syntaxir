package execlower

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

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
	srcs := make([]funcSource, len(tables))
	for i, t := range tables {
		m := t.Module.Module()
		srcs[i] = funcSource{prim.TablesOf(m), m.Funcs}
	}
	return number(srcs)
}

// NumberedExpectations returns the expectation table of a parser numbered from the numbered tables of
// the plan p (plandata.Program.Numbered), given the tables of its units at the Exec stage in the order
// of p.Units: as Expectations numbers the tables planned with every rule (plandata.Numbered.Full),
// each function taken from the part that holds it, in the order of the functions of a table lowered
// from the full plan.
func NumberedExpectations(p *plandata.Program, tables []*plandata.Table[code.Exec]) ([]string, error) {
	srcs := make([]funcSource, len(p.Numbered))
	for i, n := range p.Numbered {
		parts := make([]*plandata.Table[code.Exec], len(n.Parts))
		for j, k := range n.Parts {
			parts[j] = tables[k]
		}
		fs, err := merged(n.Full, parts)
		if err != nil {
			return nil, err
		}
		srcs[i] = funcSource{prim.TablesOf(parts[0].Module.Module()), fs}
	}
	return number(srcs)
}

// merged returns the functions of the parts, whose plans hold disjoint parts of the functions of the
// plan full, in the order in which a table lowered from full holds them: the functions of the plan in
// its order (a refused flat function at the place of the flat function), then the outlined functions
// in the order in which the lowering adds them (flowlower.Lowering): the nodes that each function
// calls, in the order of the functions, a node lowered once for its first caller.
func merged(full *plandata.Plan, parts []*plandata.Table[code.Exec]) ([]*code.Func, error) {
	if len(parts) == 1 {
		return parts[0].Module.Module().Funcs, nil
	}
	place := map[plandata.Key]int{}
	for i, s := range full.Funcs {
		place[s.Key] = i
		if s.Fallback != nil {
			place[s.Fallback.Key] = i
		}
	}
	type fn struct {
		spec *plandata.FuncSpec
		f    *code.Func
		at   int
	}
	var base []fn
	children := map[string][]fn{}
	for _, t := range parts {
		fs := t.Module.Module().Funcs
		for i, s := range t.Plan.Funcs {
			if s.Key.Use == plandata.Outlined {
				parent := s.Name[:strings.LastIndex(s.Name, "/outline")]
				children[parent] = append(children[parent], fn{s, fs[i], 0})
				continue
			}
			at, ok := place[s.Key]
			if !ok {
				return nil, fmt.Errorf("internal error: the function %s of a part of a numbered table is not planned in the table", s.Name)
			}
			base = append(base, fn{s, fs[i], at})
		}
	}
	slices.SortStableFunc(base, func(a, b fn) int { return a.at - b.at })
	type call struct {
		node  core.Node
		slots string
		build bool
	}
	seen := map[call]bool{}
	queue := base
	for i := 0; i < len(queue); i++ {
		for _, c := range children[queue[i].spec.Name] {
			k := call{c.spec.Body, strings.Join(core.SlotNames(c.spec.Slots), "\x00"), c.spec.Build}
			if !seen[k] {
				seen[k] = true
				queue = append(queue, c)
			}
		}
	}
	out := make([]*code.Func, len(queue))
	for i, f := range queue {
		out[i] = f.f
	}
	return out, nil
}

// funcSource is the functions of a table in order and the tables of its module, which the parts of a
// numbered table share as the lowering of one Core module.
type funcSource struct {
	t     *prim.Tables
	funcs []*code.Func
}

// number numbers the expectation table of the functions srcs, in order (Expectations).
func number(srcs []funcSource) ([]string, error) {
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
	for _, src := range srcs {
		t := src.t
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
						add(t.Exp(x.Exp))
					}
				case flow.OpMergeRenamed:
					add(t.Exp(e.Imm.(prim.ExpID)))
				case flow.OpMatchLit:
					if lit := e.Imm.(prim.Lit); lit.Text == "" && !lit.Fold {
						add(strconv.Quote(""))
					} else {
						add(t.Exp(lit.Exp))
					}
				case flow.OpMatchClass:
					add(t.Exp(t.Class(e.Imm.(prim.ClassID)).Exp))
				case flow.OpSkipClasses:
					for _, c := range e.Imm.(prim.ClassRun) {
						add(t.Exp(t.Class(c).Exp))
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
		for _, f := range src.funcs {
			block(f.Body)
		}
		if err != nil {
			return nil, err
		}
	}
	return descs, nil
}
