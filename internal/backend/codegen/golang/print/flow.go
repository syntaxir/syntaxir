package print

import (
	"fmt"
	"strconv"
	"strings"

	"zeroparse.dev/zp/internal/backend/codegen/common"
	"zeroparse.dev/zp/internal/frontend/check"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/execlower"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowplan"
	rprogram "zeroparse.dev/zp/internal/runtime/program"
	"zeroparse.dev/zp/pkg/grammar/ast"
)

// Typed Go rules printed from Flow IR.
//
// The typed rules are built as Core IR (TypedCore), planned (flowplan.PlanTyped) and lowered as one
// module with the rest of the table (common.Generator.LowerTable), and printed from Flow IR here. The
// rules that flowlower.LocalRule accepts are flat functions, printed into the flat rule that typedCall
// and flatMethod wrap. The bodies of left-recursion leaders and of rules that recover, and the lines of
// Operators Rules, are framed functions, printed into the matcher methods that the library calls under
// the frames it owns (finishFrameBody). A rule that the plan leaves out is reported as an error
// (FailFlow): no table has another route.
//
// The printer follows reachability: a statement at an unreachable point is left out,
// but its text is still formed, so that the package variables it names (literals, expectations,
// class tables, shapers, scopes) are declared in the same order. A variable that no printed statement
// reads is not declared and its assignments are left out (flowVars); one that must stay is assigned to the
// blank identifier on the first line.

// startFlow builds the Core IR of the typed rules for printing them from Flow IR.
func (g *Generator) startFlow() error {
	m, err := g.TypedCore()
	if err != nil {
		return err
	}
	g.Flow = &common.FlowState{TableIR: &flowplan.TableIR{M: m, Opts: g.FlowOptions(m)},
		Rules: append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)}
	return nil
}

// FlowOptions returns the options of lowering the typed rules of the Core IR m, while the typed
// table is generated.
func (g *Generator) FlowOptions(m *core.Module) flowlower.Options {
	// Node is false: the options of the Node and recognizer tables (common.Generator.StartRecFlow) are
	// built apart, and a function lowered with one set of options is never printed for the other
	// runtime.
	return flowlower.Options{Budget: !g.NoBudget, Recovers: flowlower.Recovers(m), Observe: g.Observability, Document: g.RecordsExamined(), Traced: g.Traced}
}

// flowTypedRule prints the rule r from Flow IR as a flat rule, the flat function of the typed table's
// plan (flowplan.PlanTyped), or returns nil if the plan has none for r.
func (g *Generator) flowTypedRule(r *common.Rule) *flatRule {
	if g.Table != "trules" {
		return nil
	}
	spec, ok := g.plan.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: r.ID})
	if !ok || spec.Form != flowlower.Flat {
		return nil
	}
	l := g.planned(spec.Key)
	g.Cur, g.Proj = r, g.Projections(r)
	defer func() { g.Proj = nil }()
	p := newFlowPrinter(g, false)
	p.document = flow.AttrsOf(l.Func).Document
	return g.execFlatRule(p, l)
}

// flowFrameBody prints the body of the typed rule r from Flow IR as the matcher of its frame body, the
// framed function of the typed table's plan (flowplan.PlanTyped), or returns "" if the plan has none for
// r.
func (g *Generator) flowFrameBody(r *common.Rule, scope *check.Scope) string {
	if g.Table != "trules" {
		return ""
	}
	spec, ok := g.plan.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: r.ID})
	if !ok || spec.Form != flowlower.Frame {
		return ""
	}
	if g.Flow.Opts.Node {
		panic("internal error: a typed rule is lowered with the options of the Node tables")
	}
	l := g.planned(spec.Key)
	g.Cur, g.Proj = r, g.Projections(r)
	defer func() { g.Proj = nil }()
	return g.flowFramed(l, spec.Slots, scope, r.Name+" ("+typedFrameComment(r)+")", true)
}

// flowNodeFrameBody prints the body of the rule r of the Node or recognizer table from Flow IR as
// the matcher of its frame body, the framed function spec of the table's plan (flowplan.Plan): a rule
// whose Core body is a framed body and whose action reads only captures of the frame. The library
// owns depth, memo, frame, action and finish. With budget controls (Options.Budget) the method that
// holds the body charges one step on entry (flow.Attrs.Entered) and the body charges one on each
// iteration of a repetition, plus the input spend of each Peek; the limits of a parse hold at these
// charges.
func (g *Generator) flowNodeFrameBody(r *common.Rule, spec *flowplan.FuncSpec, scope *check.Scope) string {
	if !g.Flow.Opts.Node {
		panic("internal error: a Node rule is lowered with the options of the typed table")
	}
	l := g.planned(spec.Key)
	g.Cur, g.Proj = r, g.Projections(r)
	defer func() { g.Proj = nil }()
	return g.flowFramed(l, spec.Slots, scope, r.Name+" (Node body inlined)", true)
}

// flowStreamBody prints the body of the start rule r of ParseStream from Flow IR as the matcher of its
// frame body, the function of the plan of the Node table that delivers the elements of its #stream
// repetition (flowplan.StreamBody), or returns "" if the plan has none. The capture layout of the printed
// body must be the batch body's, which the action addresses by slot.
func (g *Generator) flowStreamBody(r *common.Rule, scope *check.Scope) string {
	spec, ok := g.plan.Func(flowplan.Key{Use: flowplan.StreamBody, Rule: r.ID})
	if !ok {
		return ""
	}
	l := g.planned(spec.Key)
	g.Cur, g.Proj = r, g.Projections(r)
	defer func() { g.Proj = nil }()
	return g.flowFramed(l, spec.Slots, scope, r.Name+" (stream body)", true)
}

// flowOperatorsRuleBody prints the line of the Operators Rule g.cur whose use in the table is k from
// Flow IR, the function that the table's plan holds for k (flowplan.Plan, flowplan.PlanTyped), or
// returns "" if the plan has none.
//
// In the typed table every line is printed from Flow IR, as in the Node and recognizer tables (a line with
// recovery builds its error node with NewErrorNode there). Nothing needs a check. The lines of an Operators
// Rule have no projected repetitions (ruleProjections gives none for one), so Core IR and the tables agree
// on the shape of every capture, and the checker lets an action name only captures of its line. With budget
// controls (Options.Budget) the method that holds the function charges its entry and the function charges
// each iteration of a repetition, as Flow IR makes them explicit.
func (g *Generator) flowOperatorsRuleBody(k flowplan.Key, scope *check.Scope, build bool) string {
	spec, ok := g.plan.Func(k)
	if !ok {
		return ""
	}
	if spec.Build != build && g.FlowErr == nil {
		g.FlowErr = fmt.Errorf("internal error: a line of Operators Rule %s is planned with build %v, not %v", g.Cur.Name, spec.Build, build)
	}
	l := g.planned(k)
	prev := g.Proj
	g.Proj = nil // the actions of the lines use no projections of the rule
	defer func() { g.Proj = prev }()
	comment := " (typed OperatorsRule body inlined)"
	if g.Table != "trules" {
		comment = " (OperatorsRule body inlined)"
		if g.hooks.OperatorsRoute != nil && g.collect == nil && !g.Traced {
			g.hooks.OperatorsRoute(g.Table, g.Cur)
		}
	}
	return g.flowFramed(l, spec.Slots, scope, g.Cur.Name+comment, false)
}

// flowOperatorsRule returns the Core IR of the Operators Rule r, or nil if its lines are not
// printed from Flow IR: the typed table prints them all from it, the Node and recognizer tables
// when their Flow route is on (flowOperatorsRuleBody decides each line).
func (g *Generator) flowOperatorsRule(r *common.Rule) *core.OperatorsRule {
	if g.Flow == nil || g.Table != "trules" && !g.Flow.Opts.Node {
		return nil
	}
	o, _ := g.Flow.M.Rules[r.ID].Body.(*core.OperatorsRule)
	return o
}

// flowFramed prints the framed function f, whose capture scope has the slots, as a matcher method
// with the comment, as finishFrameBody does, and sets scope to its capture scope. A rule body (ruleBody)
// has the signature of the Body field of the rule table (see bodyMethod).
func (g *Generator) flowFramed(l *execlower.Lowered, slots []core.Slot, scope *check.Scope, comment string, ruleBody bool) string {
	p := newFlowPrinter(g, true)
	p.document = flow.AttrsOf(l.Func).Document
	return g.execFramed(p, l, slots, scope, comment, ruleBody)
}

// planned returns the function of the use k in the module of the table's plan, which LowerTable
// lowered: the plan holds it, since the printer asks only for the functions of the plan.
func (g *Generator) planned(k flowplan.Key) *execlower.Lowered {
	l, ok := g.plan.Lowered(k)
	if !ok {
		panic(fmt.Errorf("internal error: the plan of table %s has no function %+v", g.Table, k))
	}
	return l
}

func goType(t flow.Type) string {
	return [...]string{flow.TypeInt: "int", flow.TypeBool: "bool", flow.TypeRune: "rune", flow.TypeValue: "any",
		flow.TypeEnv: "*Env", flow.TypeExpMark: "ExpMark", flow.TypeExpIDs: "[]ExpID", flow.TypeVals: "[]any",
		flow.TypeCtx: "*TActCtx", flow.TypeMark: "Mark", flow.TypeFrame: "*TCaptureFrame", flow.TypeError: "*SyntaxError"}[t]
}

// goType returns the Go type of a variable of the function printed: the types of the Node library
// where the printer writes its code, otherwise those of the typed library.
func (p *flowPrinter) goType(t flow.Type) string {
	if p.untyped() {
		switch t {
		case flow.TypeValue:
			return "*Node"
		case flow.TypeVals:
			return "[]*Node"
		case flow.TypeFrame:
			return "*CaptureFrame"
		}
	}
	return goType(t)
}

// flowPrinter prints the body of a Flow IR function as the statements of a flat rule or, if
// frame, of a matcher method.
type flowPrinter struct {
	g     *Generator
	frame bool
	// document reports that the function is lowered for Documents (flow.Attrs.Document): its in-place reads
	// are guarded by p.Document == nil, so that a Document parse reads through the recording primitives.
	document bool
	b        strings.Builder
	// names holds the package variables of the capture names of scopes, declared on first use
	// unless a DeclareNames statement declares them.
	names map[*flow.Names]string
	// pre holds the statements the expression printed last needs before it: those of the term of an Eval.
	pre string
}

func newFlowPrinter(g *Generator, frame bool) *flowPrinter {
	return &flowPrinter{g: g, frame: frame, names: map[*flow.Names]string{}}
}

// scopeNames returns the package variable of the capture names n, naming it on first use.
func (p *flowPrinter) scopeNames(n *flow.Names) string {
	if name, ok := p.names[n]; ok {
		return name
	}
	name := p.g.Name("scope")
	p.names[n] = name
	if !n.Declared {
		p.declare(n)
	}
	return name
}

func (p *flowPrinter) declare(n *flow.Names) {
	fmt.Fprintf(&p.g.Vars, "var %s = %s\n", p.names[n], goStrings(n.List))
}

// untyped reports whether the printer writes the Node library's code (the Node and recognizer tables).
func (p *flowPrinter) untyped() bool { return p.g.Table == "recRules" || p.g.Table == "rules" }

// matcher returns the parser whose methods match literals: the embedded Node parser of a typed
// parser, or the Node parser itself.
func (p *flowPrinter) matcher() string {
	if p.untyped() {
		return "p"
	}
	return "p.Parser"
}

// helper names the library method that the printed code calls on the parser the body runs on: the
// typed parser's embedded parser is not renamed.
func (p *flowPrinter) helper(name string) string {
	if !p.untyped() {
		return name
	}
	return p.g.helper(name)
}

// write writes s.
func (p *flowPrinter) write(s string) { p.b.WriteString(s) }

// expectHelper names the library method that records an expectation.
func (p *flowPrinter) expectHelper() string {
	return p.g.helper("Expect")
}

// peekText returns the read of the next character into the places ch, size and ok.
func (p *flowPrinter) peekText(ch, size, ok string) string {
	// The inlined read and its fallback are one call of the library, which the compiler inlines.
	if p.g.Ordinary {
		return fmt.Sprintf("%s, %s, %s = p.NextOrdinary()\n", ch, size, ok)
	}
	if p.g.Traced {
		// A traced body runs only in an observed parse, which records every read.
		return fmt.Sprintf("%s, %s, %s = p.%s()\n", ch, size, ok, p.g.helper("Peek"))
	}
	guard := "p.Pos < len(p.In)"
	if p.document {
		guard = "p.Document == nil && " + guard
	}
	// The inlined read charges its input spend and leaves a Document's examined range to Peek.
	spend := "p.Control.Take(1)\n"
	if p.document {
		spend = "p.SpendRead(1)\n"
	}
	if !p.g.Flow.Opts.Budget {
		spend = ""
	}
	return fmt.Sprintf("if %s {\n%s%s, %s, %s = p.In[p.Pos], 1, true\n} else {\n%[3]s, %[4]s, %[5]s = p.Peek()\n}\n", guard, spend, ch, size, ok)
}

// fixedExpNames names the expectations of the library that every parser has, by ID.
var fixedExpNames = []string{"IdAny", "IdBeginInput", "IdEndInput", "IdBeginLine", "IdEndLine"}

// exp returns the Go expression of the expectation id: the name of a fixed one, otherwise its ID
// in the parser's table.
func (p *flowPrinter) exp(id core.ExpID) string {
	if int(id) < len(fixedExpNames) {
		return fixedExpNames[id]
	}
	return strconv.Itoa(int(p.desc(id)))
}

// desc returns the ID in the parser's table of the expectation id of the module.
func (p *flowPrinter) desc(id core.ExpID) rprogram.ExpID { return p.g.Desc(p.g.Flow.M.Exps[id]) }

// grammarClass returns the character class expression of the class c.
func grammarClass(c *core.Class) *ast.CharClass {
	e := &ast.CharClass{Negated: c.Negated}
	for _, r := range c.Ranges {
		e.Ranges = append(e.Ranges, ast.CharRange{Lo: r.Lo, Hi: r.Hi})
	}
	return e
}

// reject returns the condition under which the code point in the variable ch is not in the class.
func (p *flowPrinter) reject(c *core.Class, ch string) string {
	cond := p.g.ControlledClassReject(grammarClass(c))
	if ch != "ch" {
		cond = chIdent.ReplaceAllString(cond, ch)
	}
	return cond
}

// shaper returns the package variable that holds the shaper of the shape.
func (p *flowPrinter) shaper(sh core.Shape) string {
	return p.g.shaperOf(fmt.Sprintf("Shaper{Elems: %v, K: %d, Mode: %d, Tuple: %v, Shared: %v}", sh.Elems, sh.K, sh.Mode, sh.Tuple, sh.Shared))
}

// term returns the term of the grammar predicate that the node n is built from.
func (p *flowPrinter) term(n core.Node) ast.ActionExpr {
	e, ok := p.g.Flow.M.Origin.Of(n).(*ast.Predicate)
	if !ok {
		panic(fmt.Sprintf("flow printer: no grammar predicate for %T", n))
	}
	return e.Action
}
