package print

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Rules of the TypeScript parser printed from Code IR.
//
// Each table is planned (flowplan.Plan) and lowered once, with the options of the untyped tables
// except for the Go-only forms (in-place literal reads and class tables), and every function of the
// plan, the outlined nodes included, is printed by tsFlowPrinter as a function of its own. A block
// becomes a labeled block and a loop a labeled for statement, so that an exit leaves the construct it
// names with break or continue. A rule that the plan has no function for, or that the printer does not
// cover, fails the generation.

// tsFlowUnsupported is the panic value of the printer for a construct it does not print; the
// generation then fails.
type tsFlowUnsupported string

// startTSFlow builds the Core IR of the table being generated and lowers its planned functions to the
// Exec stage (lowerTable) for printing its rules. It reports whether the table was lowered; if not,
// the generation fails with the error it records.
func (g *Generator) startTSFlow() bool {
	g.Flow, g.table = nil, nil
	err := g.StartRecFlow(g.Prog)
	if err == nil {
		g.table, err = g.lowerTable()
	}
	if err != nil {
		if g.FlowErr == nil {
			g.FlowErr = err
		}
		g.table = nil
		return false
	}
	return true
}

// lowerTable plans the functions of the table being generated, whose rules, value-free twins included,
// are by ID, and lowers them through the Flow and the Exec part of the pipeline with the TypeScript
// passes (common.Generator.LowerTable). The plan leaves out the rules that the generator does not print
// (pruned). The table's configuration (common.Generator.StartRecFlow) runs a rule that binds variables
// as a framed body, whose frame the library owns, and outlines nodes, so no function grows large.
func (g *Generator) lowerTable() (*plandata.Table[code.Exec], error) {
	all := append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)
	rules := make([]*flowplan.Rule, len(all))
	for i, r := range all {
		rules[i] = r.PlanRule()
	}
	p := flowplan.Plan(g.Flow.TableIR, rules, func(id int) bool { return !g.pruned(all[id]) })
	t, err := g.LowerTable(p, nil, tspass.Pipeline)
	if err != nil {
		return nil, fmt.Errorf("table %s: %w", g.Table, err)
	}
	return t, nil
}

// flowRule prints the body of the rule r as a function of its own and returns the call expression
// that runs it, or false if the plan of the table has none: a lean value-free rule that may run flat
// is a flat function; another rule's body is a framed body, which runs under the frame the library
// owns, which also runs its action and finish. On success it sets scope to the capture scope of a
// framed body.
func (g *Generator) flowRule(r *common.Rule, scope *check.Scope) (call string, ok bool) {
	if g.table == nil {
		return "", false
	}
	l, ok := execlower.Func(g.table, plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
	if !ok {
		return "", false
	}
	return g.flowFunc(l, g.Flow.M.Rules[r.ID].Scope, scope, common.Describe(r.BodyExpr()), true)
}

// flowFunc prints the lowered function l, whose capture scope has the slots if it is framed, as a
// function of its own and returns the call expression that runs it, or false if the printer does not
// print l. On success it sets scope to the capture scope of a framed l. The comment describes the
// function. The function of a rule body (ruleBody) charges the unit of the body's evaluation first,
// as Method does.
func (g *Generator) flowFunc(l *execlower.Lowered, slots []core.Slot, scope *check.Scope, comment string, ruleBody bool) (call string, ok bool) {
	defer func() {
		if e := recover(); e != nil {
			if _, unsupported := e.(tsFlowUnsupported); !unsupported {
				panic(e)
			}
			call, ok = "", false
		}
	}()
	frame := prim.AttrsOf(l.Func).Frame
	var body string
	var vars []string
	var decls []tsDecl
	body, vars, decls = g.printTSFlowExec(l)
	var b strings.Builder
	for _, v := range vars {
		b.WriteString(v)
	}
	if ruleBody && !g.NoBudget {
		b.WriteString(indent(tsSpend(1)))
	}
	b.WriteString(indent(body))
	if frame {
		*scope = *check.NewScope()
		for _, sl := range slots {
			scope.Slot(sl.Name)
		}
	}
	name, shared := g.MethodName("Parser/R", tsSharedKey(b.String(), decls))
	if !shared {
		for _, d := range decls {
			if tsNamesIdentifier(b.String(), d.name) {
				g.Vars.WriteString(d.text)
			}
		}
		param := "p"
		if !strings.Contains(b.String(), "p.") && !strings.Contains(b.String(), "(p)") {
			param = "_p"
		}
		fmt.Fprintf(&g.Methods, "// %s\nfunction %s(%s: Parser): R {\n%s}\n\n", comment, name, param, b.String())
	}
	return name + "(p)", true
}

var (
	tsMarkParts = [4]string{"Pos", "Env", "Trail", "Rec"}
	tsMarkZero  = [4]string{"0", "null as Env | null", "0", "0"}
)

func tsFlowType(t prim.Type) string {
	switch t {
	case prim.TypeInt, prim.TypeRune:
		return "number"
	case prim.TypeBool:
		return "boolean"
	case prim.TypeValue:
		return "Node | null"
	case prim.TypeVals:
		return "(Node | null)[]"
	case prim.TypeFrame:
		return "Frame"
	case prim.TypeExpMark:
		return "number"
	case prim.TypeExpIDs:
		return "number[]"
	case prim.TypeError:
		return "SyntaxError | null"
	}
	panic(tsFlowUnsupported("variable of type " + t.String()))
}

func tsFlowZero(t prim.Type) string {
	switch t {
	case prim.TypeBool:
		return "false"
	case prim.TypeValue:
		return "null"
	case prim.TypeVals:
		return "[]"
	case prim.TypeFrame:
		return "emptyFrame"
	case prim.TypeExpIDs:
		return "noExp"
	case prim.TypeError:
		return "null"
	}
	return "0"
}

// tsSharedKey returns the text by which printed functions share a body: the text with each constant
// it names replaced by the declaration of the constant, so that equal functions share whatever the
// names of their constants (the literal tables of the TypeScript generation are not shared) are.
func tsSharedKey(code string, decls []tsDecl) string {
	for _, d := range decls {
		if !tsNamesIdentifier(code, d.name) {
			continue
		}
		id := regexp.MustCompile(`\b` + regexp.QuoteMeta(d.name) + `\b`)
		code = id.ReplaceAllLiteralString(code, "{"+id.ReplaceAllLiteralString(d.text, "$")+"}")
	}
	return code
}

// tsDecl is a package-level constant of a printed function.
type tsDecl struct{ name, text string }

func (p *tsFlowPrinter) write(s string) {
	if !p.dead && !p.dry {
		p.b.WriteString(s)
	}
}

// counted returns the expression node, which constructs a node, counted for the memory limit as
// bytes, an expression of the library's size model, when the parser has limit controls.
func (p *tsFlowPrinter) counted(node, bytes string) string {
	if p.g.NoBudget {
		return node
	}
	return "p.counted(" + node + ", " + bytes + ")"
}

// tsSpend returns the statement that charges n steps, as n one-unit charges.
func tsSpend(n int) string {
	if n == 1 {
		return "p.spend();\n"
	}
	return fmt.Sprintf("p.spendUnits(%d);\n", n)
}

func indent(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.SplitAfter(s, "\n")
	var b strings.Builder
	for _, l := range lines {
		if l != "" {
			b.WriteString("  ")
			b.WriteString(l)
		}
	}
	return b.String()
}

func stmtSep(stmt bool) string {
	if stmt {
		return ";\n"
	}
	return ", "
}

var tsFixedExpNames = []string{"idAny", "idBeginInput", "idEndInput", "idBeginLine", "idEndLine"}

// exp returns the TypeScript expression of the expectation id: the name of a fixed one, otherwise its
// ID in the parser's table.
func (p *tsFlowPrinter) exp(id core.ExpID) string {
	if int(id) < len(tsFixedExpNames) {
		return tsFixedExpNames[id]
	}
	return strconv.Itoa(int(p.desc(id)))
}

func (p *tsFlowPrinter) desc(id core.ExpID) rprogram.ExpID { return p.g.Desc(p.g.Flow.M.Exps[id]) }

// classIn returns the condition that the code point in ch is in the class c (false at the end of the
// input is the caller's check).
func tsClassIn(c *core.Class, ch string) string {
	var in []string
	for _, r := range c.Ranges {
		if r.Lo == r.Hi {
			in = append(in, fmt.Sprintf("%s === %d", ch, r.Lo))
		} else {
			in = append(in, fmt.Sprintf("(%s >= %d && %s <= %d)", ch, r.Lo, ch, r.Hi))
		}
	}
	if len(in) == 0 {
		in = []string{ch + " < 0"} // never true; mentions ch so that it is used
	}
	cond := tsJoinCondition(in, " || ")
	if c.Negated {
		return "!" + wrap(cond)
	}
	return cond
}

func wrap(cond string) string {
	if strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
		return cond
	}
	return "(" + cond + ")"
}

// decl declares a package-level constant of the printed function, once per pass that prints.
func (p *tsFlowPrinter) decl(name, s string) {
	p.decls = append(p.decls, tsDecl{name, s})
}

// call returns the call of the rule r from the Operators Rule level min.
func (p *tsFlowPrinter) call(r *common.Rule, min int) string {
	g := p.g
	g.checkCall(r)
	switch {
	case callsPlain(r, g.Document):
		g.plainUsed[r] = true
		return fmt.Sprintf("%s%d(p)", g.plainPrefix(), r.ID)
	case r.Plain && !g.Document:
		return fmt.Sprintf("p.invokePlain(%s, %d)", g.ruleVar(r), min)
	}
	return fmt.Sprintf("p.call(%s, %d)", g.ruleVar(r), min)
}

// tsNamesIdentifier reports whether the code uses the identifier name.
func tsNamesIdentifier(code, name string) bool {
	for i := 0; ; {
		j := strings.Index(code[i:], name)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(name)
		if end >= len(code) || !(code[end] >= '0' && code[end] <= '9' || code[end] >= 'a' && code[end] <= 'z' || code[end] >= 'A' && code[end] <= 'Z' || code[end] == '_') {
			return true
		}
		i = j + 1
	}
}

// tsShape packs a shape as the library's Shaper (tsShaper).
func tsShape(sh core.Shape) int {
	s := int(sh.Mode)<<2 | sh.K<<5
	if sh.Elems {
		s |= 1
	}
	if sh.Tuple {
		s |= 2
	}
	if sh.Shared {
		s |= 16
	}
	return s
}

// scopeNames returns the constant that holds the capture names n, declaring it on first use.
func (p *tsFlowPrinter) scopeNames(n *prim.Names) string {
	if p.dry {
		return "scope"
	}
	if name, ok := p.names[n]; ok {
		return name
	}
	name := p.g.Name("scope")
	if p.names == nil {
		p.names = map[*prim.Names]string{}
	}
	p.names[n] = name
	p.decl(name, fmt.Sprintf("const %s: string[] = %s;\n", name, TsStrings(n.List)))
	return name
}

// grammarTerm returns the term of the grammar predicate that the node n is built from, for the name
// of the variable that a binding sets.
func (p *tsFlowPrinter) grammarTerm(n core.Node) ast.ActionExpr {
	e, ok := p.g.Flow.M.Origin.Of(n).(*ast.Predicate)
	if !ok {
		panic(tsFlowUnsupported(fmt.Sprintf("no grammar predicate for %T", n)))
	}
	return e.Action
}

// flowLine prints the line k of the Operators Rule g.cur, whose Core IR is l, as a function and
// returns the call expression that runs it, or false if the plan of the table has none. It sets scope
// to the capture scope of the line. With budget controls the library loop that runs the line charges
// its entry and the function charges each iteration of a repetition.
func (g *Generator) flowLine(k plandata.Key, l *core.OpLine, scope *check.Scope) (string, bool) {
	if g.table == nil || l == nil {
		return "", false
	}
	x, ok := execlower.Func(g.table, k)
	if !ok {
		return "", false
	}
	prev := g.Proj
	g.Proj = nil // the actions of the lines use no projections of the rule
	defer func() { g.Proj = prev }()
	return g.flowFunc(x, l.Scope, scope, g.Cur.Name+" (OperatorsRule line)", false)
}
