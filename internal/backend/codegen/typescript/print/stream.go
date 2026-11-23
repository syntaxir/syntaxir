package print

import (
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
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
// rules' resumable bodies (bodyGens). A Document or observed parse has no stream.
func (g *Generator) streamRules() {
	switch {
	case g.Document:
		g.printErr(fmt.Errorf("a TypeScript stream does not parse with Documents"))
		return
	case g.Observability:
		g.printErr(fmt.Errorf("a TypeScript stream is not observed"))
		return
	}
	g.stream = true
	defer func() { g.stream = false }()
	if !g.startTSTable() {
		return
	}
	plain := maps.Clone(g.plainUsed)
	all := append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)
	gens := make([]string, len(all))
	for i, r := range all {
		gens[i] = "null"
		if g.pruned(r) {
			continue
		}
		g.Cur = r
		if _, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
			// Its lines read the input, so the stream needs their resumable form.
			g.printErr(fmt.Errorf("internal error: the TypeScript stream has no resumable form of the Operators Rule %s", r.Name))
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

// resumableRule reports whether a call of the rule r in the stream can suspend: whether the function
// of its body in the table of the stream is resumable.
func (g *Generator) resumableRule(r *common.Rule) bool {
	if !g.stream || g.table == nil {
		return false
	}
	l, ok := execdata.Func(g.table, plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
	return ok && prim.AttrsOf(l.Func).Resumable
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
	fmt.Fprintf(&g.Methods, "// %s, called as by invokePlain\nfunction* %s%s {\n%s}\n\n", r.Name, g.plainGenName(r), tsGenSignature, text)
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
