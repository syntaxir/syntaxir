package print

import (
	"errors"
	"fmt"
	"os"
	"strconv"

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

// guardInfo is what the skip of a guarded alternative counts under logical units. The skip is
// kept (static) when, on every character the guard rejects, the plain evaluation of the
// alternative makes a fixed set of calls and fails at a terminal without advancing: the
// alternative begins with a chain of calls, every alternative of every callee in the chain
// begins the same way, and no predicate, action, binding or recovery runs before that terminal.
// calls is the number of those calls. memo lists the rules of the calls that are ordinarily
// memoized, in order, each as its index in the rule table: the skip reproduces the memo policy
// effects of their calls (the seen mark and the policy counts of FirstCall). Such a rule must make
// no call before its own failing terminal, so whether its call at the position is answered from
// the memo or evaluated costs the same 1 unit, and the failure entry that the plain evaluation may
// store changes no later total; it must read no variables, so its key counts nothing. Leaders and
// transient rules (memoized only by a Document) end the chain, so the guard is disabled.
type guardInfo struct {
	calls  int
	memo   string
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
					info := g.chainOf(m, a.Body)
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

// chainOf returns what skipping n counts, if n fails at its first terminal with a fixed set
// of calls (see guardInfo).
func (g *Generator) chainOf(m *core.Module, n core.Node) guardInfo {
	calls, memo, ok := g.chain(m, n, 0)
	if !ok {
		return guardInfo{}
	}
	info := guardInfo{calls: calls, static: true}
	for i, r := range memo {
		if i > 0 {
			info.memo += ","
		}
		info.memo += strconv.Itoa(r)
	}
	return info
}

// chain returns the calls n makes before it fails at its first terminal, and the ordinarily
// memoized rules among them; ok is false if n is not such an expression.
func (g *Generator) chain(m *core.Module, n core.Node, depth int) (calls int, memo []int, ok bool) {
	if depth > 64 {
		return 0, nil, false
	}
	stop := func(why string) (int, []int, bool) {
		if os.Getenv("SXIR_LOGICAL_STATS") != "" {
			fmt.Fprintf(os.Stderr, "chain stops at %s\n", why)
		}
		return 0, nil, false
	}
	switch x := n.(type) {
	case *core.Lit:
		if x.Text == "" {
			return stop("empty literal")
		}
		return 0, nil, true
	case *core.ClassNode:
		return 0, nil, true
	case *core.ClassRun:
		if len(x.Classes) == 0 {
			return stop("empty class run")
		}
		return 0, nil, true
	case *core.Seq:
		if len(x.Items) == 0 {
			return stop("empty sequence")
		}
		return g.chain(m, x.Items[0], depth+1)
	case *core.Capture:
		// The binding runs after its body matched, and the body fails.
		return g.chain(m, x.Body, depth+1)
	case *core.Text:
		return g.chain(m, x.Body, depth+1)
	case *core.Drop:
		return g.chain(m, x.Body, depth+1)
	case *core.Terminal:
		return g.chain(m, x.Body, depth+1)
	case *core.Choice:
		// Every alternative is tried, and each fails at its first terminal.
		for _, a := range x.Alts {
			c, mm, ok := g.chain(m, a.Body, depth+1)
			if !ok {
				return 0, nil, false
			}
			calls += c
			memo = append(memo, mm...)
		}
		return calls, memo, true
	case *core.Call:
		r := m.Rules[x.Rule]
		if x.Level != 0 || r.Policy.Leader {
			return stop("leader or level call of " + r.Name)
		}
		if r.Policy.Transient && !g.LogicalTransient {
			return stop("transient rule " + r.Name)
		}
		c, mm, ok := g.chain(m, r.Body, depth+1)
		if !ok {
			return 0, nil, false
		}
		if r.Policy.Memo && !r.Policy.Transient {
			if c != 0 || len(r.Policy.Vars) != 0 {
				return stop("memoized rule " + r.Name + " with calls or a key")
			}
			return 1, []int{x.Rule}, true
		}
		return 1 + c, mm, true
	}
	return stop(fmt.Sprintf("%T", n))
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
