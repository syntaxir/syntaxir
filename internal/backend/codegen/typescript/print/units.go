package print

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// Logical compute units (spec/runtime/parsing.md, Compute Units): the form of the functions of a
// parser with limit controls, a Document's included. The library counts the events of the call
// protocol and of its matchers in Parser.units; the printed code adds the input that its terminals
// advance over, the syntax errors that recoveries build and the calls that skipped alternatives
// would make, and checks the counter at the iterations of loops.

// tsUnitsCheck is the check of the counter of compute units (Parser.pollUnits).
const tsUnitsCheck = "if (p.units > p.unitsCheck) {\n  p.pollUnits();\n}\n"

// skippedCalls returns the statement that makes the effects of the calls that the alternative a
// guard skips would make (flow.skippedCalls). Calls of rules that the parse does not memoize only
// count their units; the calls of memoized rules have the effects of the memo too, which the library
// makes (Parser.skipCalls). A rule that the parse does not memoize is given by whether it is on a
// cycle of the call graph, all that its call's unit needs, as its plain function does: such a rule
// may be entered only through that function, which leaves its constant out (tableRow).
func (p *tsPrimPrinter) skippedCalls(x prim.SkippedCalls) string {
	g := p.g
	memo := false
	rules := make([]string, len(x.Rules))
	for i, k := range x.Rules {
		r := g.RuleOf(k)
		if pol := g.Core().Rules[k].Policy; !pol.Memo || pol.Transient {
			rules[i] = fmt.Sprint(r.Rec)
			continue
		}
		memo = true
		rules[i] = g.ruleVar(r)
	}
	if !memo {
		return fmt.Sprintf("p.skipUnits(%d);\n", len(x.Rules))
	}
	return fmt.Sprintf("p.skipCalls([%s], %s);\n", strings.Join(rules, ", "), p.exp(x.Exp))
}
