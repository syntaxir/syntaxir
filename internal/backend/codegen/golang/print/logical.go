package print

import (
	"errors"
	"fmt"
	"os"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The prototype of logical compute units (design record 076): the ordinary form of the functions,
// with a counter that each logical event adds to and checks of the counter on cycles. The option is
// internal and measures the design; it covers the grammars without left recursion, Operators Rules
// and recovery, in batch parses.

// logicalRefusal returns why the prototype cannot generate the program, nil if it can.
func logicalRefusal(prog *common.Program, opts common.GenOptions) error {
	switch {
	case !opts.LimitControls:
		return errors.New("logical units need -limits")
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

// ruleFeaturesOf names a feature of r that the prototype does not cover, "" if none.
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
	astfacts.WalkExpr(r.Def.Expr, func(e ast.Expr) {
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

// guardInfo is what the skip of a guarded alternative counts under logical units: the calls of the
// fixed chain of rules that the alternative begins with, if every one of them is neither memoized
// nor a leader, so that the skip has no memo effects (static); otherwise the guard is disabled.
type guardInfo struct {
	calls  int
	static bool
}

// logicalGuard returns what the skip of the alternative of guard counts, from the Core IR of the
// table being printed.
func (g *Generator) logicalGuard(m *core.Module, guard *core.First) guardInfo {
	if g.guards == nil || g.guardsOf != m {
		g.guards, g.guardsOf = map[*core.First]guardInfo{}, m
		var walk func(n core.Node)
		walk = func(n core.Node) {
			if c, ok := n.(*core.Choice); ok {
				for _, a := range c.Alts {
					if a.Guard == nil {
						continue
					}
					info := chainOf(m, a.Body)
					if !info.static && os.Getenv("SXIR_LOGICAL_STATS") != "" {
						fmt.Fprintf(os.Stderr, "guard disabled: %s\n", core.PrintNode(m, a.Body, true))
					}
					if prev, ok := g.guards[a.Guard]; ok && prev != info {
						// One guard object for alternatives that count differently. Disabling it would
						// be wrong where an alternative begins past the character the guard tested,
						// so the prototype stops.
						panic(fmt.Sprintf("sxir: logical units: one dispatch guard for alternatives that count differently (%+v, %+v)", prev, info))
					}
					g.guards[a.Guard] = info
				}
			}
			if o, ok := n.(*core.OperatorsRule); ok {
				for _, l := range o.Lines() {
					walk(l.Body)
				}
				return
			}
			for _, c := range core.Children(n) {
				walk(c)
			}
		}
		for _, r := range m.Rules {
			if r != nil {
				walk(r.Body)
			}
		}
	}
	info, ok := g.guards[guard]
	if !ok {
		return guardInfo{}
	}
	return info
}

// chainOf returns the chain of calls that n begins with, up to its first terminal.
func chainOf(m *core.Module, n core.Node) guardInfo {
	calls := 0
	for depth := 0; depth < 1000; depth++ {
		switch x := n.(type) {
		case *core.Lit, *core.ClassNode, *core.ClassRun:
			return guardInfo{calls: calls, static: true}
		case *core.Seq:
			if len(x.Items) == 0 {
				return guardInfo{}
			}
			n = x.Items[0]
		case *core.Capture:
			n = x.Body
		case *core.Text:
			n = x.Body
		case *core.Drop:
			n = x.Body
		case *core.Terminal:
			n = x.Body
		case *core.Call:
			r := m.Rules[x.Rule]
			if r.Policy.Memo || r.Policy.Transient || r.Policy.Leader {
				if os.Getenv("SXIR_LOGICAL_STATS") != "" {
					fmt.Fprintf(os.Stderr, "chain stops at rule %s %+v\n", r.Name, r.Policy)
				}
				return guardInfo{}
			}
			calls++
			n = r.Body
		default:
			if os.Getenv("SXIR_LOGICAL_STATS") != "" {
				fmt.Fprintf(os.Stderr, "chain stops at %T\n", n)
			}
			return guardInfo{}
		}
	}
	return guardInfo{}
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
