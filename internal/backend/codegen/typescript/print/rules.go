package print

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
)

// Rules of the TypeScript parser printed from Code IR.
//
// Each table is planned (flowplan.Plan) and lowered once, with the options of the untyped tables
// except for the Go-only forms (in-place literal reads and class tables), and every function of the
// plan, the outlined nodes included, is printed by tsPrimPrinter as a function of its own. A block
// becomes a labeled block and a loop a labeled for statement, so that an exit leaves the construct it
// names with break or continue. A rule that the plan has no function for, or that the printer does not
// cover, fails the generation.

// admitted panics with the construct what that the printer does not print: the analysis ts-printable
// (typescript.Printability) admitted a function that has it, which is an internal error.
func admitted(what string) {
	panic("tsgen: the analysis admitted " + what)
}

// startTSTable starts printing the rules of the table being generated from its table at the Exec stage
// among the tables of the compilation. It reports whether the table was lowered; if not, the
// generation fails with the error it records.
func (g *Generator) startTSTable() bool {
	g.Exec, g.table = nil, nil
	name := g.IRTable()
	if g.stream {
		name = StreamTable
	}
	t := g.tables.Table(name)
	if t == nil {
		if g.PrintErr == nil {
			g.PrintErr = fmt.Errorf("internal error: the table %s is not lowered", name)
		}
		return false
	}
	g.Exec = t
	g.table = t
	return true
}

// ruleBodyCall prints the body of the rule r as a function of its own and returns the call expression
// that runs it, or false if the plan of the table has none: a lean value-free rule that may run flat
// is a flat function; another rule's body is a framed body, which runs under the frame the library
// owns, which also runs its action and finish. On success it sets scope to the capture scope of a
// framed body.
func (g *Generator) ruleBodyCall(r *common.Rule, scope *check.Scope) (call string, ok bool) {
	if g.table == nil {
		return "", false
	}
	l, ok := execdata.Func(g.table, plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
	if !ok {
		return "", false
	}
	return g.funcCall(l, core.SlotNames(g.Core().Rules[r.ID].Scope), scope, common.Describe(r.BodyExpr())), true
}

// noFunction records that the table being printed has no function of what, which the plan of a table
// holds for every rule it prints, and so fails the generation with an internal error, unless an error
// is recorded already (the table was not lowered).
func (g *Generator) noFunction(what string) {
	if g.PrintErr == nil {
		g.PrintErr = fmt.Errorf("internal error: the table %s has no function of %s", g.IRTable(), what)
	}
}

// funcCall prints the lowered function l, whose capture scope has the slots if it is framed, as a
// function of its own and returns the call expression that runs it, and sets scope to the capture
// scope of a framed l. The comment describes the function. The call of a resumable function of the
// stream is a yield* of it, which only a resumable function makes.
func (g *Generator) funcCall(l *execdata.Lowered, slots []string, scope *check.Scope, comment string) string {
	name, gen := g.funcName(l, slots, scope, comment)
	if gen {
		return "(yield* " + name + "(p))"
	}
	return name + "(p)"
}

// funcName is funcCall, returning the name of the printed function and whether it is a resumable
// function of the stream, which is printed as a generator (function*).
func (g *Generator) funcName(l *execdata.Lowered, slots []string, scope *check.Scope, comment string) (string, bool) {
	frame := prim.AttrsOf(l.Func).Frame
	gen := g.stream && prim.AttrsOf(l.Func).Resumable
	var body string
	var vars []string
	var decls []tsDecl
	body, vars, decls = g.printTSFunc(l)
	var b strings.Builder
	for _, v := range vars {
		b.WriteString(v)
	}
	b.WriteString(indent(body))
	if frame {
		*scope = *check.NewScope()
		for _, sl := range slots {
			scope.Slot(sl)
		}
	}
	key := tsSharedKey(b.String(), decls)
	if gen {
		key = "function*\n" + key
	}
	name, shared := g.MethodName("Parser/R", key)
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
		if gen {
			fmt.Fprintf(&g.Methods, "// %s\nfunction* %s%s {\n%s}\n\n", comment, name, strings.Replace(tsGenSignature, "p:", param+":", 1), b.String())
		} else {
			fmt.Fprintf(&g.Methods, "// %s\nfunction %s(%s: Parser): R {\n%s}\n\n", comment, name, param, b.String())
		}
	}
	return name, gen
}

var (
	tsMarkParts = [4]string{"Pos", "Env", "Trail", "Rec"}
	tsMarkZero  = [4]string{"0", "null as Env | null", "0", "0"}
)

func tsPrimType(t prim.Type) string {
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
	case prim.TypeRun:
		return "RunState"
	}
	admitted("a variable of the type " + t.String())
	return ""
}

func tsPrimZero(t prim.Type) string {
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
	case prim.TypeRun:
		// A run is read only where flow.resumeOn held, after flow.resumeStart set it.
		return "null!"
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

func (p *tsPrimPrinter) write(s string) {
	if !p.dead && !p.dry {
		p.b.WriteString(s)
	}
}

// counted returns the expression node, which constructs a node, counted for the memory limit as
// bytes, an expression of the library's size model, when the parser has limit controls.
func (p *tsPrimPrinter) counted(node, bytes string) string {
	if !p.g.limits {
		return node
	}
	return "p.counted(" + node + ", " + bytes + ")"
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
func (p *tsPrimPrinter) exp(id prim.ExpID) string {
	if int(id) < len(tsFixedExpNames) {
		return tsFixedExpNames[id]
	}
	return strconv.Itoa(int(p.desc(id)))
}

func (p *tsPrimPrinter) desc(id prim.ExpID) rprogram.ExpID { return p.g.Desc(p.g.Tables().Exp(id)) }

// class returns the class id of the module.
func (p *tsPrimPrinter) class(id prim.ClassID) prim.Class { return p.g.Tables().Class(id) }

// classIn returns the condition that the code point in ch is in the class c (false at the end of the
// input is the caller's check).
func tsClassIn(c prim.Class, ch string) string {
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
func (p *tsPrimPrinter) decl(name, s string) {
	p.decls = append(p.decls, tsDecl{name, s})
}

// call returns the call of the rule r from the Operators Rule level min.
func (p *tsPrimPrinter) call(r *common.Rule, min int) string {
	g := p.g
	g.checkCall(r)
	resumable := g.resumableRule(r)
	if !p.gen && resumable {
		// A function that calls a rule whose body suspends is resumable itself (prim.Attrs.Resumable).
		admitted("a call of the resumable rule " + r.Name + " outside a resumable function")
	}
	if resumable {
		// The call of a rule whose body suspends is the resumable form of its call.
		switch {
		case callsPlain(r, g.Document):
			g.plainGenUsed[r] = true
			return "(yield* " + g.plainGenName(r) + "(p))"
		}
		// A plain Operators Rule too, which the batch code calls with invokePlain: for a rule that is
		// neither memoized nor a leader and has no captures, the call protocol counts the unit of the
		// call as invokePlain does and runs invokePlainBody.
		return fmt.Sprintf("(yield* p.callBodyGen(%s, %d))", g.ruleVar(r), min)
	}
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
func tsShape(sh prim.Shape) int {
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
func (p *tsPrimPrinter) scopeNames(n *prim.Names) string {
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

// lineFunc prints the line k of the Operators Rule g.cur, whose Core IR is l, as a function and
// returns the call expression that runs it, or false if the plan of the table has none. It sets scope
// to the capture scope of the line. With limit controls the function counts the logical compute units
// of the line's events and checks the counter at each iteration of a repetition.
func (g *Generator) lineFunc(k plandata.Key, l *core.OpLine, scope *check.Scope) (string, bool) {
	if g.table == nil || l == nil {
		return "", false
	}
	x, ok := execdata.Func(g.table, k)
	if !ok {
		return "", false
	}
	return g.funcCall(x, core.SlotNames(l.Scope), scope, g.Cur.Name+" (OperatorsRule line)"), true
}
