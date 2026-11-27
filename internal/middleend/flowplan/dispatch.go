package flowplan

import (
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Dispatch.
//
// The Core pass guards gives an alternative a guard (core.Alt.Guard) when the next character can rule
// it out: the first terminal of the alternative, which the parse tests before it tries the alternative.
// Whether a table keeps a guard is a decision of its own, which depends on what the parse of the
// table must leave when it skips the alternative: the pass dispatch makes it for every table of the
// program, before plan, from the Core IR of the table and its lowering options (Dispatching), and the
// lowering only reads it (lowerdata.Options.Guarded).

// PassDispatch is the name of the pass dispatch (Dispatching), and of the flag that turns it off.
const PassDispatch = "dispatch"

// CounterGuarded is the counter of dispatch: the alternatives that keep their guard.
const CounterGuarded = "guarded"

func init() {
	pass.Register(pass.Flag{Name: PassDispatch, Kind: pass.KindCorePass, Default: 1,
		Doc:       "keep the guards of the alternatives whose skip leaves what their evaluation would",
		Tradeoff:  "Off, the parser tries every alternative, as with `-no-opt guards`",
		Mechanism: "M010", Record: 141, Backends: pass.On(pass.BackendRecursiveVM, pass.BackendIterativeVM) | pass.Generated,
		Witnesses: []string{"guards"}})
}

// Dispatching is the pass dispatch: it chooses the alternatives of each table of the program that keep
// their guard (Guarded), and sets them as the guarded alternatives of the lowering options of the table
// (lowerdata.Options.Guarded). The units that share a table keep sharing it. The tables it is given
// are unchanged. Without it, the lowering tries every alternative.
var Dispatching = pipe.New(pipe.Def[*plandata.Cores, *plandata.Cores]{
	Decl: pipe.Decl{Name: PassDispatch, Counters: []string{CounterGuarded},
		Obligation: "Skipping a guarded alternative leaves the state that evaluating it leaves, and records the same expectations."},
	In: plandata.CoresIR, Out: plandata.CoresIR,
	Run: func(x *pipe.Ctx[*plandata.Cores], c *plandata.Cores) (*plandata.Cores, pass.PreservedSet, error) {
		out := *c
		out.Units = slices.Clone(c.Units)
		tables := map[*plandata.TableIR]*plandata.TableIR{}
		for i, u := range out.Units {
			ir, ok := tables[u.IR]
			if !ok {
				ir = dispatch(u.IR, cancel.New(x.Context()))
				x.Count(CounterGuarded, len(ir.Opts.Guarded))
				tables[u.IR] = ir
			}
			out.Units[i].IR = ir
		}
		return &out, pass.PreserveNone(), nil
	},
})

// dispatch returns the table ir with the guarded alternatives that the pass dispatch chooses for it
// (Guarded), or ir itself when no alternative keeps its guard. ir is unchanged. It polls control,
// which may be nil, for each node of the module.
func dispatch(ir *plandata.TableIR, control *cancel.Control) *plandata.TableIR {
	g := guarded(ir.M, ir.Opts, control)
	if len(g) == 0 {
		return ir
	}
	out := *ir
	out.Opts.Guarded = g
	return &out
}

// Guarded returns the alternatives of the module m that keep their guard when m is lowered with the
// options o, with what the skip of each makes:
//
//   - With lowerdata.GuardsNone, no guard is kept.
//   - With logical units and lowerdata.GuardsKeep, every guard is kept, and the skip makes none of
//     the effects of the calls it passes over.
//   - Without logical units and without Documents, every guard is kept: the skip makes nothing that
//     the parse would observe.
//   - Otherwise a guard is kept when core.GuardCalls describes the calls that evaluating the
//     alternative makes before it fails at its first terminal, and, with lowerdata.GuardsEvent, only
//     when there are none. Under logical units the skip makes the effects of these calls
//     (flow.skippedCalls). In a lowering for Documents a Document parse does not take a guard whose
//     skip passes over a call of a rule that it memoizes (docMemoized), and evaluates the alternative.
func Guarded(m *core.Module, o lowerdata.Options) map[*core.Alt]lowerdata.Guard {
	return guarded(m, o, nil)
}

// guarded is Guarded, polling control, which may be nil, for each node of m.
func guarded(m *core.Module, o lowerdata.Options, control *cancel.Control) map[*core.Alt]lowerdata.Guard {
	out := map[*core.Alt]lowerdata.Guard{}
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		core.Walk(r.Body, func(n core.Node) {
			control.Poll()
			if c, ok := n.(*core.Choice); ok {
				for _, a := range c.Alts {
					if g, keep := guardOf(m, o, a); keep {
						out[a] = g
					}
				}
			}
		})
	}
	return out
}

// guardOf reports whether the alternative a keeps its guard in a lowering with the options o, and what
// its skip makes (Guarded).
func guardOf(m *core.Module, o lowerdata.Options, a *core.Alt) (lowerdata.Guard, bool) {
	if a.Guard == nil || o.Guards == lowerdata.GuardsNone {
		return lowerdata.Guard{}, false
	}
	if o.Logical && o.Guards == lowerdata.GuardsKeep || !o.Logical && !o.Document {
		return lowerdata.Guard{}, true
	}
	calls, ok := core.GuardCalls(m, a.Body)
	if !ok || len(calls) > 0 && o.Logical && o.Guards == lowerdata.GuardsEvent {
		return lowerdata.Guard{}, false
	}
	g := lowerdata.Guard{Outside: o.Document && docMemoized(m, calls)}
	if o.Logical {
		g.Calls = calls
	}
	return g, true
}

// docMemoized reports whether a Document parse memoizes one of the rules: every rule that an
// ordinary parse memoizes, and the transient rules that only Documents memoize. A skip cannot make
// the effects of such a call in a Document, where the memo records how far each entry examined the
// input and what later parses reuse (spec/runtime/documents.md#reuse), so a Document parse evaluates
// the alternative, as the engine refuses the skip there.
func docMemoized(m *core.Module, calls []int) bool {
	for _, k := range calls {
		if m.Rules[k].Policy.Memo {
			return true
		}
	}
	return false
}
