package print

import (
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The stream of a -stream parser parses with the Node table lowered for a suspended stream
// (StreamTable), whose functions demand the input that their reads need (runtime.inputDemand). The
// functions that reach a demand are resumable (prim.Attrs.Resumable): the printer prints each as a
// generator (function*) after the batch code, with each demand a yield of the units of input that
// the parse lacks and each call of a resumable function a yield* of it, and prints the other functions
// of the table as the batch code has them, so that they share its functions. The batch code stays as
// it is without -stream. The parser of the stream is the library's StreamParser, which holds the
// state of the parse as the batch Parser does, and the resumable bodies of the rules (bodyGens); a
// generator holds the locals of its function only.

// StreamTable is the label of the table of the stream of a -stream parser: the Node table lowered for
// a suspended stream (plandata.TableConfig.Suspend).
const StreamTable = "rulesStream"

// tsGenSignature is the parameter and the result of a resumable function.
const tsGenSignature = "(p: StreamParser): Generator<number, R, void>"

// streamRules prints the resumable functions of the stream after the batch code: the body of each
// rule of the Node table that reaches a demand, the functions that these call, and the table of the
// rules' resumable bodies (bodyGens). A Document parse has no stream.
func (g *Generator) streamRules() {
	g.stream = true
	defer func() { g.stream = false }()
	if !g.startTSTable() {
		return
	}
	plain := maps.Clone(g.plainUsed)
	all := append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)
	gens := make([]string, len(all))
	// The start rule of the stream runs its stream body, which delivers the elements of its #stream
	// repetition where the stream runs it at the top (streamsIn) and is the rule's body elsewhere: it
	// is the rule's resumable body, and the rule has no other.
	streams := 0
	for i, r := range all {
		l, ok := execdata.Func(g.table, plandata.Key{Use: plandata.StreamBody, Rule: r.ID})
		if !ok {
			continue
		}
		streams++
		g.Cur = r
		name, gen := g.funcName(l, nil, check.NewScope(), r.Name+" (stream body)")
		if !gen {
			// A stream body that demands no input still runs as the driver runs every body.
			wrapped := name + "Gen"
			fmt.Fprintf(&g.Methods, "// %s (stream body), which never suspends\nfunction* %s%s {\n  return %s(p);\n}\n\n", r.Name, wrapped, tsGenSignature, name)
			name = wrapped
		}
		gens[i] = name
		g.genBodies[r] = name
	}
	if streams != 1 {
		g.printErr(fmt.Errorf("internal error: the stream has %d stream bodies, not that of its start rule", streams))
		return
	}
	for i, r := range all {
		if gens[i] != "" {
			continue
		}
		gens[i] = "null"
		if g.pruned(r) {
			continue
		}
		g.Cur = r
		if e, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
			if g.operatorsRuleGens(r, e) && g.slotReached(r) {
				gens[i] = fmt.Sprintf("(p: StreamParser, min: number) => p.operatorsRuleParseGen(%s, min)", g.ruleVar(r))
			}
			continue
		}
		l, ok := execdata.Func(g.table, plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
		if !ok {
			g.noFunction("the body of rule " + r.Name)
			continue
		}
		if !prim.AttrsOf(l.Func).Resumable {
			continue
		}
		name, _ := g.funcName(l, nil, check.NewScope(), common.Describe(r.BodyExpr()))
		g.genBodies[r] = name
		if g.slotReached(r) {
			gens[i] = name
		}
	}
	fmt.Fprintf(&g.Init, "const bodyGens: (BodyGen | null)[] = [%s];\n", strings.Join(gens, ", "))
	// The plain functions that the resumable code calls: the generators of the resumable rules, and
	// those of the other rules that the batch code does not call.
	for _, r := range all {
		if g.plainGenUsed[r] {
			g.plainGenCall(r)
		}
		if g.plainUsed[r] && !plain[r] {
			l, ok := execdata.Func(g.table, plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
			if !ok {
				g.noFunction("the body of rule " + r.Name)
				continue
			}
			g.Cur = r
			g.plainCall(r, g.funcCall(l, nil, check.NewScope(), common.Describe(r.BodyExpr())))
		}
	}
}

// operatorsRuleGens prints the resumable functions of the skip and the lines of the Operators Rule r of
// the stream that can suspend, and sets them as the resumable forms of the rule's skip and lines
// (ResumableOperatorsRule.skipGen, ResumableLine.mGen), which the resumable forms of the Operators Rule loop
// run in place of the others. It reports whether any can suspend: whether a call of r can.
func (g *Generator) operatorsRuleGens(r *common.Rule, e *ast.OperatorsRule) bool {
	co, ok := g.Core().Rules[r.ID].Body.(*core.OperatorsRule)
	if !ok {
		g.printErr(fmt.Errorf("internal error: the Operators Rule %s has no Core IR of an Operators Rule", r.Name))
		return false
	}
	suspends := false
	// gen prints the function of the line k, whose Core IR is l, if it can suspend, and sets it as the
	// resumable form of the line at place.
	gen := func(k plandata.Key, l *core.OpLine, place string) {
		x, ok := execdata.Func(g.table, k)
		if !ok {
			g.noFunction(fmt.Sprintf("the line %d of the Operators Rule %s", k.Line, r.Name))
			return
		}
		if !prim.AttrsOf(x.Func).Resumable {
			return
		}
		suspends = true
		name, _ := g.funcName(x, core.SlotNames(l.Scope), check.NewScope(), r.Name+" (OperatorsRule line)")
		fmt.Fprintf(&g.Init, "%s = %s;\n", place, name)
	}
	if e.Skip != nil {
		gen(plandata.Key{Use: plandata.OperatorsSkip, Rule: r.ID}, co.Skip, fmt.Sprintf("(%s.operatorsRule! as ResumableOperatorsRule).skipGen", g.ruleVar(r)))
	}
	line := func(path string) string {
		return fmt.Sprintf("(%s.operatorsRule!.%s as ResumableLine).mGen", g.ruleVar(r), path)
	}
	for i := range e.Operands {
		gen(plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: i}, co.Operands[i], line(fmt.Sprintf("operands[%d]!", i)))
	}
	// The operators in the order of their levels, each in the list of its kind (Generator.operatorsRule).
	id, prefix, led := 0, 0, 0
	for _, l := range e.Levels {
		for _, op := range l.Operators {
			place := line(fmt.Sprintf("led[%d]!.line", led))
			if op.Kind == ast.Prefix {
				place = line(fmt.Sprintf("prefix[%d]!.line", prefix))
				prefix++
			} else {
				led++
			}
			gen(plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: len(e.Operands) + id}, co.Ops[id].Line, place)
			id++
		}
	}
	return suspends
}

// resumableRule reports whether a call of the rule r in the stream can suspend: whether the function
// of its body in the table of the stream is resumable, or for an Operators Rule a function of its skip
// or its lines.
func (g *Generator) resumableRule(r *common.Rule) bool {
	if !g.stream || g.table == nil {
		return false
	}
	e, ok := r.Def.Expr.(*ast.OperatorsRule)
	if !ok {
		l, ok := execdata.Func(g.table, plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
		return ok && prim.AttrsOf(l.Func).Resumable
	}
	// An Operators Rule, which suspends where its skip or a line does.
	keys := []plandata.Key{{Use: plandata.OperatorsSkip, Rule: r.ID}}
	n := len(e.Operands)
	for _, l := range e.Levels {
		n += len(l.Operators)
	}
	for i := range n {
		keys = append(keys, plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: i})
	}
	for _, k := range keys {
		if l, ok := execdata.Func(g.table, k); ok && prim.AttrsOf(l.Func).Resumable {
			return true
		}
	}
	return false
}

// plainGenCall prints the resumable function of the plain rule r, which a resumable call site calls in
// place of invokePlain: the template invokePlain with the resumable body of r.
func (g *Generator) plainGenCall(r *common.Rule) {
	f, err := g.template("invokePlain")
	if err != nil {
		g.printErr(err)
		return
	}
	text, err := Statements(g.tables.Library.Module, f, tsTemplateTarget(g.ruleVar(r), "(yield* "+g.genBodies[r]+"(p))", r.Rec, r.Novalue), 1)
	if err != nil {
		g.printErr(err)
		return
	}
	name := g.tracePlainGenCall(r, g.plainGenName(r))
	fmt.Fprintf(&g.Methods, "// %s, called as by invokePlain\nfunction* %s%s {\n%s}\n\n", r.Name, name, tsGenSignature, text)
}

// plainGenName returns the name of the resumable function of the plain rule r (plainGenCall).
func (g *Generator) plainGenName(r *common.Rule) string {
	return fmt.Sprintf("%s%dGen", g.plainPrefix(), r.ID)
}

// demand writes the demand of the input of a read (runtime.inputDemand) in a resumable function: the
// function yields the units of input that the parse lacks until the input holds them or is final.
func (p *tsPrimPrinter) demand(d prim.Demand) {
	if !p.gen {
		admitted("a demand of input outside a resumable function")
	}
	lacks := "p.demandChar()"
	if !d.Char {
		// The units of the literal, as the library matches it (litText).
		text := litText(d.Lit)
		k := fmt.Sprint(len(text))
		if n := utf8.RuneCountInString(text); n != len(text) {
			k = fmt.Sprintf("p.unit === Bytes ? %d : %d", len(text), n)
		}
		lacks = "p.demandUnits(" + k + ")"
	}
	p.write(fmt.Sprintf("for (let lacks = %s; lacks > 0; lacks = %s) yield lacks;\n", lacks, lacks))
}

// streamParser returns the parser of the stream in the function being printed: p in a resumable
// function, whose parameter is the StreamParser, and p cast to it in another function of the stream,
// which the batch code's signature types as a Parser.
func (p *tsPrimPrinter) streamParser() string {
	if p.gen {
		return "p"
	}
	return "(p as StreamParser)"
}
