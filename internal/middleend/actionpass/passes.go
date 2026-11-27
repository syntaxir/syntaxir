// Package actionpass holds the passes of the Action module of a table that every target that prints
// their forms runs, and the step that ends each function of the module (Finishing).
//
// The Action segment of a target's pipeline lowers the action expressions of a table (actionlower),
// runs the passes of the slot action-passes on each function of a site, then the passes of the
// target, and ends each function with action-finish, which names its locals in the order they appear
// and drops the facts that the lowering recorded for the passes.
package actionpass

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Unit is the unit of the Action passes: an Action module that holds the function of one site
// (code.ActionFuncs).
type Unit = code.At[code.Action]

// The names of the passes and steps of the package.
const (
	PassTextDispatch = "text-dispatch"
	PassFinish       = "action-finish"
)

func init() {
	pass.Register(pass.Flag{Name: PassTextDispatch, Kind: pass.KindActionPass, Default: 1,
		Doc:       "a predicate that compares the text of a capture with literals tests them at once when the parse runs without limit controls",
		Tradeoff:  "One test of the literals against a chain of comparisons, and the text is computed once",
		Mechanism: "G070", Record: 136, Backends: pass.Generated,
		Witnesses: []string{"text dispatch"}})
}

// funcOf returns the function of a unit of one site and its attributes.
func funcOf(u Unit) (*code.Func, *action.Site) {
	f := u.Module().Funcs[0]
	return f, action.SiteOf(f)
}

// TextDispatch is the pass text-dispatch: a predicate that compares the text of one capture or local
// with string literals (action.Membership) first tests, when the parse runs without limit controls
// (action.uncontrolled), the text against the literals at once (action.textIn), and evaluates the
// action expression as written otherwise, where the comparisons count their units.
var TextDispatch = pipe.New(pipe.Def[Unit, Unit]{
	Decl: pipe.Decl{Name: PassTextDispatch,
		Obligation: "The predicate has the value and the failure of its action expression."},
	In: code.ActionStage, Out: code.ActionStage,
	Run: func(_ *pipe.Ctx[Unit], u Unit) (Unit, pass.PreservedSet, error) {
		f, s := funcOf(u)
		if s.Kind != action.SitePred || s.Membership == nil || s.Result != "" {
			return u, pass.PreserveAll(), nil
		}
		failT := code.THandle(action.Fail)
		nm := action.NewNamer(f)
		x := s.Membership
		dispatch := code.Stmts(f, func(b *code.Builder) {
			b.If(code.C(action.OpActionUncontrolled), func(b *code.Builder) {
				k := nm.Next()
				r := b.LetN([]string{action.LocalName("ts", k), action.LocalName("tf", k)},
					code.C("text", &code.Call{Callee: action.OpActionRead, Imm: x.Read}))
				b.If(code.Bin("!=", r[1], code.Null(failT)), func(b *code.Builder) {
					b.Return(code.Null(code.THandle(exec.Datum)), r[1])
				})
				var in code.Expr = &code.Call{Callee: action.OpActionTextIn, Args: []code.Expr{r[0]}, Imm: x.Lits}
				if len(x.Lits) > 1 {
					// A target may print the binding as one switch; one literal stays an equality.
					in = b.Let(action.LocalName("tin", nm.Next()), in)
				}
				b.Return(code.C("val.fromBool", in), code.Null(failT))
			})
		})
		f.Body.Stmts = append(dispatch, f.Body.Stmts...)
		next := *s
		next.Membership = nil
		f.Attrs = &next
		return u, pass.PreserveNone(), nil
	},
})

// Finishing is the step action-finish: it names the locals and loops of the function of a site in the
// order in which they appear (action.Renumber) and drops the facts of the lowering
// (action.Site.Membership, Structs), which no pass after it reads.
var Finishing = pipe.New(pipe.Def[Unit, Unit]{
	Decl: pipe.Decl{Name: PassFinish, Required: true,
		Obligation: "The function evaluates its site as before; only the names of its locals and loops change."},
	In: code.ActionStage, Out: code.ActionStage,
	Run: func(_ *pipe.Ctx[Unit], u Unit) (Unit, pass.PreservedSet, error) {
		f, s := funcOf(u)
		action.Renumber(f)
		next := *s
		next.Membership, next.Structs = nil, nil
		f.Attrs = &next
		return u, pass.PreserveNone(), nil
	},
})
