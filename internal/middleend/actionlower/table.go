package actionlower

import (
	"fmt"
	"slices"
	"strconv"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The Action module of a table.
//
// The step actionlower (Lowering) lowers the action expressions of a table once, into its Action
// module (code.ActionStage): one function for each action site of the table's Core IR, in the order
// of the sites (ir.ActionSites), which the Flow operations of the table name by their number. Each
// function returns the value of its action expression and its failure, with the projections of its
// rule (Options.Proj) and, if the table counts logical compute units, the visits of the elements that
// its loops visit (Options.Visits). The forms that a target chooses are the passes that follow it in
// the Action segment of the target's pipeline.

// PassLower is the name of the step actionlower.
const PassLower = "actionlower"

// CounterSites is the counter of actionlower: the action sites that it lowered.
const CounterSites = "sites"

// Lowering is the step actionlower: it lowers the action expressions of the table to its Action module.
var Lowering = pipe.New(pipe.Def[*plandata.TableIR, code.At[code.Action]]{
	Decl: pipe.Decl{Name: PassLower, Required: true, Counters: []string{CounterSites},
		Obligation: "Each function evaluates the action expression of its site as Core IR defines it."},
	In: plandata.TableCoreIR, Out: code.ActionStage,
	Run: func(c *pipe.Ctx[*plandata.TableIR], t *plandata.TableIR) (code.At[code.Action], pass.PreservedSet, error) {
		u, n, err := lowerTable(t.M, t.Opts.Logical)
		if err != nil {
			return code.At[code.Action]{}, pass.PreserveNone(), err
		}
		c.Count(CounterSites, n)
		return u, pass.PreserveNone(), nil
	},
})

// SiteFunc returns the name of the function of the site i in an Action module.
func SiteFunc(i int) string { return "site" + strconv.Itoa(i) }

// lowerTable lowers the action expressions of the sites of the Core module m into a new Action
// module, counting the visits of loop elements if visits (Options.Visits), and returns it with the
// number of sites.
func lowerTable(m *ir.Module, visits bool) (code.At[code.Action], int, error) {
	mod, err := actionModule()
	if err != nil {
		return code.At[code.Action]{}, 0, err
	}
	mod.Dialects = dialects(mod)
	sites := ir.ActionSitesOf(m)
	proj := map[int]map[string]string{}
	for i, s := range sites.Sites {
		p, ok := proj[s.Rule]
		if !ok {
			p = projections(m.Rules[s.Rule].Body)
			proj[s.Rule] = p
		}
		f, l, err := lowerFunc(mod, SiteFunc(i), s.Action, Options{Visits: visits, Proj: p})
		if err != nil {
			return code.At[code.Action]{}, 0, fmt.Errorf("rule %s: %w", m.Rules[s.Rule].Name, err)
		}
		f.Attrs = &action.Site{Site: i, Kind: siteKind(s.Kind), Rule: s.Rule, Membership: l.membership, Structs: l.structs}
	}
	return code.EnterAction(mod), len(sites.Sites), nil
}

// siteKind returns the kind of an Action function of a site of the kind k.
func siteKind(k ir.ActionSiteKind) action.SiteKind {
	switch k {
	case ir.SiteRule:
		return action.SiteRule
	case ir.SiteLine:
		return action.SiteLine
	case ir.SitePred:
		return action.SitePred
	case ir.SiteBind:
		return action.SiteBind
	}
	panic(fmt.Sprintf("actionlower: action site kind %d", k))
}

// dialects returns the dialects of the operations that m declares, which are legal in an Action
// module, with that of the operations that the Action passes add (action.NewOp).
func dialects(m *code.Module) []string {
	out := []string{action.Dialect}
	for _, o := range m.Ops {
		if !slices.Contains(out, o.Dialect) {
			out = append(out, o.Dialect)
		}
	}
	slices.Sort(out)
	return out
}

// projections returns the projected captures of a rule body, mapped to the field each gathers
// (ir.Capture.Project): an action expression of the rule reads the list of such a capture as the
// values of that field (Options.Proj).
func projections(body ir.Node) map[string]string {
	var proj map[string]string
	ir.Walk(body, func(n ir.Node) {
		if c, ok := n.(*ir.Capture); ok && c.Project != "" {
			if proj == nil {
				proj = map[string]string{}
			}
			proj[c.Name] = c.Project
		}
	})
	return proj
}
