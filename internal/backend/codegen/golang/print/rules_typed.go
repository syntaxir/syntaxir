package print

import (
	"fmt"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Typed Go rules printed from Exec IR.
//
// The typed rules are built as Core IR, planned (flowplan.PlanTyped, with the program) and lowered as one
// module with the rest of the table by the pipeline of the compilation, and printed from Exec IR here. The
// rules that flowlower.LocalRule accepts are flat functions, printed into the flat rule that typedCall
// and flatMethod wrap. The bodies of left-recursion leaders and of rules that recover, and the lines of
// Operators Rules, are framed functions, printed into the matcher methods that the library calls under
// the frames it owns (finishFrameBody). A rule that the plan leaves out is reported as an error
// (Unprintable): no table has another route.
//
// The printer follows reachability: a statement at an unreachable point is left out,
// but its text is still formed, so that the package variables it names (literals, expectations,
// class tables, shapers, scopes) are declared in the same order. A variable that no printed statement
// reads is not declared and its assignments are left out (flowVars); one that must stay is assigned to the
// blank identifier on the first line.

// typedRuleFunc prints the rule r from Exec IR as a flat rule, the flat function of the typed table's
// plan (flowplan.PlanTyped), or returns nil if the plan has none for r.
func (g *Generator) typedRuleFunc(r *common.Rule) *flatRule {
	if g.Table != "trules" {
		return nil
	}
	spec, ok := g.plan.Func(plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
	if !ok || spec.Form != lowerdata.Flat {
		return nil
	}
	l := g.planned(spec.Key)
	g.Cur = r
	return g.execFlatRule(newPrimPrinter(g, false, l), l)
}

// frameBody prints the body of the typed rule r from Exec IR as the matcher of its frame body, the
// framed function of the typed table's plan (flowplan.PlanTyped), or returns "" if the plan has none for
// r.
func (g *Generator) frameBody(r *common.Rule, scope *check.Scope) string {
	if g.Table != "trules" {
		return ""
	}
	spec, ok := g.plan.Func(plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
	if !ok || spec.Form != lowerdata.Frame {
		return ""
	}
	l := g.planned(spec.Key)
	if prim.AttrsOf(l.Func).Node {
		panic("internal error: a typed rule is lowered with the options of the Node tables")
	}
	g.Cur = r
	return g.framedFunc(l, spec.Slots, scope, r.Name+" ("+typedFrameComment(r)+")", true)
}

// nodeFrameBody prints the body of the rule r of the Node or recognizer table from Exec IR as
// the matcher of its frame body, the framed function spec of the table's plan (flowplan.Plan): a rule
// whose Core body is a framed body and whose action reads only captures of the frame. The library
// owns depth, memo, frame, action and finish.
func (g *Generator) nodeFrameBody(r *common.Rule, spec *plandata.FuncSpec, scope *check.Scope) string {
	l := g.planned(spec.Key)
	if !prim.AttrsOf(l.Func).Node {
		panic("internal error: a Node rule is lowered with the options of the typed table")
	}
	g.Cur = r
	return g.framedFunc(l, spec.Slots, scope, r.Name+" (Node body inlined)", true)
}

// streamBodyFunc prints the body of the start rule r of ParseStream from Exec IR as the matcher of its
// frame body, the function of the plan of the Node table that delivers the elements of its #stream
// repetition (plandata.StreamBody), or returns "" if the plan has none. The capture layout of the printed
// body must be the batch body's, which the action addresses by slot.
func (g *Generator) streamBodyFunc(r *common.Rule, scope *check.Scope) string {
	spec, ok := g.plan.Func(plandata.Key{Use: plandata.StreamBody, Rule: r.ID})
	if !ok {
		return ""
	}
	l := g.planned(spec.Key)
	g.Cur = r
	return g.framedFunc(l, spec.Slots, scope, r.Name+" (stream body)", true)
}

// operatorsRuleBody prints the line of the Operators Rule g.cur whose use in the table is k from
// Exec IR, the function that the table's plan holds for k (flowplan.Plan, flowplan.PlanTyped), or
// returns "" if the plan has none.
//
// In the typed table every line is printed from Exec IR, as in the Node and recognizer tables (a line with
// recovery builds its error node with NewErrorNode there). Nothing needs a check. The lines of an Operators
// Rule have no projected repetitions (ruleProjections gives none for one), so Core IR and the tables agree
// on the shape of every capture, and the checker lets an action name only captures of its line.
func (g *Generator) operatorsRuleBody(k plandata.Key, scope *check.Scope, build bool) string {
	spec, ok := g.plan.Func(k)
	if !ok {
		return ""
	}
	if spec.Build != build && g.PrintErr == nil {
		g.PrintErr = fmt.Errorf("internal error: a line of Operators Rule %s is planned with build %v, not %v", g.Cur.Name, spec.Build, build)
	}
	l := g.planned(k)
	comment := " (typed OperatorsRule body inlined)"
	if g.Table != "trules" {
		comment = " (OperatorsRule body inlined)"
		if g.hooks.OperatorsRoute != nil && !g.Traced {
			g.hooks.OperatorsRoute(g.Table, g.Cur)
		}
	}
	return g.framedFunc(l, spec.Slots, scope, g.Cur.Name+comment, false)
}

// operatorsRuleOf returns the Core IR of the Operators Rule r, or nil if no table is being printed
// (common.Generator.Exec): every table prints the lines of an Operators Rule from its functions
// (operatorsRuleBody decides each line).
func (g *Generator) operatorsRuleOf(r *common.Rule) *core.OperatorsRule {
	if g.Exec == nil {
		return nil
	}
	o, _ := g.Core().Rules[r.ID].Body.(*core.OperatorsRule)
	return o
}

// framedFunc prints the framed function f, whose capture scope has the slots, as a matcher method
// with the comment, as finishFrameBody does, and sets scope to its capture scope. A rule body (ruleBody)
// has the signature of the Body field of the rule table (see bodyMethod).
func (g *Generator) framedFunc(l *execdata.Lowered, slots []core.Slot, scope *check.Scope, comment string, ruleBody bool) string {
	return g.execFramed(newPrimPrinter(g, true, l), l, slots, scope, comment, ruleBody)
}

// planned returns the function of the use k in the module of the table, which the compilation lowered: the
// plan holds it, since the printer asks only for the functions of the plan.
func (g *Generator) planned(k plandata.Key) *execdata.Lowered {
	l, ok := execdata.Func(g.table, k)
	if !ok {
		panic(fmt.Errorf("internal error: the plan of table %s has no function %+v", g.Table, k))
	}
	return l
}

func goType(t prim.Type) string {
	return [...]string{prim.TypeInt: "int", prim.TypeBool: "bool", prim.TypeRune: "rune", prim.TypeValue: "any",
		prim.TypeEnv: "*Env", prim.TypeExpMark: "ExpMark", prim.TypeExpIDs: "[]ExpID", prim.TypeVals: "[]any",
		prim.TypeCtx: "*TActCtx", prim.TypeMark: "Mark", prim.TypeFrame: "*TCaptureFrame", prim.TypeError: "*SyntaxError", prim.TypeRun: "*RunState"}[t]
}

// goType returns the Go type of a variable of the function printed: the types of the Node library
// where the printer writes its code, otherwise those of the typed library.
func (p *primPrinter) goType(t prim.Type) string {
	if p.untyped() {
		switch t {
		case prim.TypeValue:
			return "*Node"
		case prim.TypeVals:
			return "[]*Node"
		case prim.TypeFrame:
			return "*CaptureFrame"
		}
	}
	return goType(t)
}

// primPrinter prints the body of an Exec IR function as the statements of a flat rule or, if
// frame, of a matcher method.
type primPrinter struct {
	g     *Generator
	frame bool
	// document reports that the function is lowered for Documents (prim.Attrs.Document): its in-place reads
	// are guarded by p.Document == nil, so that a Document parse reads through the recording primitives.
	document bool
	b        strings.Builder
	// names holds the package variables of the capture names of scopes, declared on first use
	// unless a DeclareNames statement declares them.
	names map[*prim.Names]string
	// pre holds the statements the expression printed last needs before it: those of the action expression of an Eval.
	pre string
}

// newPrimPrinter returns a printer of the lowered function l, a matcher of a frame if frame, with the
// lowering options of l, which its attributes hold.
func newPrimPrinter(g *Generator, frame bool, l *execdata.Lowered) *primPrinter {
	a := prim.AttrsOf(l.Func)
	return &primPrinter{g: g, frame: frame, document: a.Document, names: map[*prim.Names]string{}}
}

// scopeNames returns the package variable of the capture names n, naming it on first use.
func (p *primPrinter) scopeNames(n *prim.Names) string {
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

func (p *primPrinter) declare(n *prim.Names) {
	fmt.Fprintf(&p.g.Vars, "var %s = %s\n", p.names[n], goStrings(n.List))
}

// untyped reports whether the printer writes the Node library's code (the Node and recognizer tables).
func (p *primPrinter) untyped() bool { return p.g.Table == "recRules" || p.g.Table == "rules" }

// matcher returns the parser whose methods match literals: the embedded Node parser of a typed
// parser, or the Node parser itself.
func (p *primPrinter) matcher() string {
	if p.untyped() {
		return "p"
	}
	return "p.Parser"
}

// helper names the library method that the printed code calls on the parser the body runs on: the
// typed parser's embedded parser is not renamed.
func (p *primPrinter) helper(name string) string {
	if !p.untyped() {
		if p.g.Logical && logicalHelperName(name) {
			return name + "Logical"
		}
		return name
	}
	return p.g.helper(name)
}

// write writes s.
func (p *primPrinter) write(s string) { p.b.WriteString(s) }

// expectHelper names the library method that records an expectation.
func (p *primPrinter) expectHelper() string {
	return p.g.helper("Expect")
}

// peekText returns the read of the next character into the places ch, size and ok.
func (p *primPrinter) peekText(ch, size, ok string) string {
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
	// The inlined read leaves a Document's examined range to Peek.
	return fmt.Sprintf("if %s {\n%s, %s, %s = p.In[p.Pos], 1, true\n} else {\n%[2]s, %[3]s, %[4]s = p.Peek()\n}\n", guard, ch, size, ok)
}

// fixedExpNames names the expectations of the library that every parser has, by ID.
var fixedExpNames = []string{"IdAny", "IdBeginInput", "IdEndInput", "IdBeginLine", "IdEndLine"}

// exp returns the Go expression of the expectation id: the name of a fixed one, otherwise its ID
// in the parser's table.
func (p *primPrinter) exp(id prim.ExpID) string {
	if int(id) < len(fixedExpNames) {
		return fixedExpNames[id]
	}
	return strconv.Itoa(int(p.desc(id)))
}

// desc returns the ID in the parser's table of the expectation id of the module.
func (p *primPrinter) desc(id prim.ExpID) rprogram.ExpID { return p.g.Desc(p.g.Tables().Exp(id)) }

// class returns the class id of the module.
func (p *primPrinter) class(id prim.ClassID) prim.Class { return p.g.Tables().Class(id) }

// grammarClass returns the character class expression of the class c.
func grammarClass(c prim.Class) *ast.CharClass {
	e := &ast.CharClass{Negated: c.Negated}
	for _, r := range c.Ranges {
		e.Ranges = append(e.Ranges, ast.CharRange{Lo: r.Lo, Hi: r.Hi})
	}
	return e
}

// reject returns the condition under which the code point in the variable ch is not in the class.
func (p *primPrinter) reject(c prim.Class, ch string) string {
	cond := classReject(grammarClass(c))
	if ch != "ch" {
		cond = chIdent.ReplaceAllString(cond, ch)
	}
	return cond
}

// shaper returns the package variable that holds the shaper of the shape.
func (p *primPrinter) shaper(sh prim.Shape) string {
	return p.g.shaperOf(fmt.Sprintf("Shaper{Elems: %v, K: %d, Mode: %d, Tuple: %v, Shared: %v}", sh.Elems, sh.K, sh.Mode, sh.Tuple, sh.Shared))
}
