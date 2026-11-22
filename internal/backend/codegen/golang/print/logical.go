package print

import (
	"errors"
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Logical compute units (design record 076): the ordinary form of the functions, with a counter
// that each logical event adds to and checks of the counter on cycles. The option is internal; it
// covers the grammars without left recursion, Operators Rules and recovery, in batch parses outside
// a Document.

// LogicalRefusal returns why logical units cannot be generated for the program, nil if they can.
func LogicalRefusal(prog *common.Program, opts common.GenOptions) error {
	switch {
	case opts.NoLimitControls:
		return errors.New("logical units need limit controls (not with -no-limits)")
	case opts.Document || opts.Stream:
		return errors.New("logical units do not cover Documents and streams")
	case opts.Observability:
		return errors.New("logical units do not cover observability")
	}
	for _, r := range prog.RuleList {
		f := ruleFeaturesOf(r)
		if f != "" {
			return fmt.Errorf("logical units do not cover %s (rule %s)", f, r.Name)
		}
	}
	return nil
}

// ruleFeaturesOf names a feature of r that logical units do not cover, "" if none.
func ruleFeaturesOf(r *common.Rule) string {
	if r.Leader || r.Member || r.Cycle != 0 {
		return "left recursion"
	}
	if r.Def == nil {
		return ""
	}
	if _, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
		return "Operators Rules"
	}
	found := ""
	astinspect.WalkExpr(r.Def.Expr, func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Recovery:
			found = "recovery"
		case *ast.Attributed:
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					found = "recovery"
				}
			}
		}
	})
	return found
}

// callUnit returns the statements that count the unit of a call of r at its entry, and check the
// counter if r is on a cycle of the call graph.
func (g *Generator) callUnit(recv string, r *common.Rule) string {
	if !g.Logical {
		return g.step(recv)
	}
	s := recv + ".Units++\n"
	if r == nil || r.Rec {
		s += "if " + recv + ".Units > " + recv + ".UnitsCheck {\n" + recv + ".PollUnits()\n}\n"
	}
	return s
}

// logicalPoll is the check of the counter at a loop's iteration.
const logicalPoll = "if p.Units > p.UnitsCheck {\np.PollUnits()\n}"

// skippedCalls prints the effects of the calls that the alternative a guard skips would make
// (flow.skippedCalls). Calls of rules that the parse does not memoize only count their units; the
// calls of memoized rules have the effects of the memo too, which the library makes.
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
	if g.Table == "trules" {
		table = "rules"
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

// callName names the library method that calls a rule through the call protocol: under logical
// units it counts the unit of the call and the key of a memoized rule first.
func (g *Generator) callName() string { return g.pickLogical("Call") }

// pickLogical returns the name of the library method name, or of its logical form under logical
// units.
func (g *Generator) pickLogical(name string) string {
	if g.Logical {
		return name + "Logical"
	}
	return name
}
