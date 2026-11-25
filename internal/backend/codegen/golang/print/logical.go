package print

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// Logical compute units (spec/runtime/parsing.md, Compute Units): the form of the functions under
// limit controls. It is the form of the functions without limit controls (the ordinary form, or a
// Document's) with a counter that each logical event adds to, and checks of the counter on cycles.

// callUnit returns the statements that count the unit of a call of r at its entry, and check the
// counter if r is on a cycle of the call graph.
func (g *Generator) callUnit(recv string, r *common.Rule) string {
	if !g.Logical {
		return ""
	}
	s := recv + ".Fuel--\n"
	if r == nil || r.Rec {
		s += "if " + recv + ".Fuel < 0 {\n" + recv + ".PollUnits()\n}\n"
	}
	return s
}

// logicalPoll is the check of the counter at a loop's iteration.
const logicalPoll = "if p.Fuel < 0 {\np.PollUnits()\n}"

// skippedCalls prints the effects of the calls that the alternative a guard skips would make
// (flow.skippedCalls). Calls of rules that the parse does not memoize only count their units; the
// calls of memoized rules have the effects of the memo too, which the library makes. In a Document
// no skip passes over a call of a rule that the Document memoizes (lowerdata.GuardsExact), so the
// transient rules, which only Documents memoize, only count their units.
func (p *primPrinter) skippedCalls(x prim.SkippedCalls) string {
	g := p.g
	memo := false
	for _, k := range x.Rules {
		pol := g.Core().Rules[k].Policy
		memo = memo || pol.Memo && !pol.Transient
	}
	if !memo {
		return fmt.Sprintf("p.SkipUnits(%d)", len(x.Rules))
	}
	// The typed library makes the memo effects from the rows of the Node table, as the entries of
	// typed rules do (FirstCall).
	table := g.tableVar()
	if g.typed() {
		table = g.nodeRows()
	}
	rules := make([]string, len(x.Rules))
	for i, k := range x.Rules {
		rules[i] = fmt.Sprintf("%s[%d]", table, g.RuleOf(k).ID)
	}
	if len(rules) == 1 {
		return fmt.Sprintf("p.SkipCall(%s, %s)", rules[0], p.exp(x.Exp))
	}
	return fmt.Sprintf("p.SkipCalls(%s, %s)", p.exp(x.Exp), strings.Join(rules, ", "))
}

// pickLogical returns the name of the library method name, or of its logical form under logical
// units.
func (g *Generator) pickLogical(name string) string {
	if g.Logical {
		return name + "Logical"
	}
	return name
}
