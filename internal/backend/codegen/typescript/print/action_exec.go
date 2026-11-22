package print

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	irc "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
)

// Action expressions printed from Exec IR.
//
// Every action expression is lowered by flowlower.Action and printed from Exec IR as the statements of a function: the
// calls of the compositions of the library's Parser (actionBinary, ...) with the failure of each checked
// after it, a step charged for each subexpression under budget controls, and the leaves, which read the
// captures and variables of the action.

// actionStmts returns the statements of a function that returns the value of the action expression ct of Core IR,
// printed from Exec IR. The scope s and the locals resolve the leaves (leaf). A predicate that
// compares the text of a capture with literals tests them at once without limit controls if
// dispatch is set (text-dispatch).
func (g *Generator) actionStmts(ct irc.ActionExpr, s *check.Scope, locals map[string]string, dispatch bool) string {
	if ct == nil {
		panic(fmt.Errorf("rule %s: an action expression without Core IR", g.Cur.Name))
	}
	l, err := flowlower.Action(ct, flowlower.ActionOptions{Steps: !g.NoBudget, Unbounded: g.NoBudget, Proj: g.Proj, TextDispatch: dispatch})
	if err != nil {
		panic(fmt.Errorf("rule %s: %w", g.Cur.Name, err))
	}
	text, err := Statements(l.Module, l.Func, g.actionTarget(s, locals), 1)
	if err != nil {
		panic(fmt.Errorf("rule %s: %w", g.Cur.Name, err))
	}
	g.LoweredActions++
	return text
}

// actionFunc returns the function that evaluates the action expression ct of Core IR, declared with the name name.
func (g *Generator) actionFunc(name string, ct irc.ActionExpr, s *check.Scope, locals map[string]string) string {
	body := g.actionStmts(ct, s, locals, false)
	return fmt.Sprintf("function %s(%s: Actx): [Value, Value] {\n%s}\n\n", name, tsActxParam(body), body)
}

// predicateFunc returns the arrow function of a predicate with the action expression ct of Core IR: a membership
// of a captured text in literals tests them at once without limit controls (text-dispatch).
func (g *Generator) predicateFunc(ct irc.ActionExpr, s *check.Scope) string {
	body := g.actionStmts(ct, s, nil, g.Opt().On("text-dispatch"))
	return fmt.Sprintf("(%s): [Value, Value] => {\n%s}", tsActxParam(body), body)
}

// leaf returns the expression that reads the capture (or, if variable is set, the variable) name of
// an action: a local that holds it (a parameter or $lhs, $rhs and $op, which may hold boxes that
// stand for their values), or the slot of the scope s.
func (g *Generator) leaf(name string, variable bool, s *check.Scope, locals map[string]string) string {
	if variable {
		return fmt.Sprintf("c.variable(%d)", g.Prog.Vars.Index(name))
	}
	if l, ok := locals[name]; ok {
		return "unboxValue(" + l + ")"
	}
	return fmt.Sprintf("c.cap(%d)", s.Slots[name])
}

// actionTarget returns the TypeScript implementation of the operations of a lowered action expression whose leaves
// the scope s and the locals resolve.
func (g *Generator) actionTarget(s *check.Scope, locals map[string]string) *Target {
	return &Target{
		Stmt: func(c *code.Call, a []string, dst []string, decl string) (string, bool) {
			if c.Callee != action.OpActionTextIn || len(dst) != 1 || dst[0] == "_" {
				return "", false
			}
			// One switch over the literals instead of a comparison with each literal in turn.
			cases := ""
			for _, lit := range c.Imm.([]string) {
				cases += "case " + TsString(lit) + ": "
			}
			if decl != "" {
				decl = "let "
			}
			return decl + dst[0] + " = false;\nswitch (" + a[0] + ") { " + cases + dst[0] + " = true; }", true
		},
		Self:     "c.p",
		Handle:   tsHandle,
		FuncName: tsActionMethod,
		Reg:      func(name string) string { panic("tsgen: a lowered action expression reads the register " + name) },
		Op: func(c *code.Call, a []string) string {
			switch c.Callee {
			case action.OpActionConst:
				switch v := c.Imm.(action.Const).Value.(type) {
				case nil:
					return "null"
				case string:
					return TsString(v)
				case int:
					return tsInt(v)
				default:
					return fmt.Sprint(v)
				}
			case action.OpActionRead:
				x := c.Imm.(action.Read)
				return g.leaf(x.Name, x.Kind == action.ReadVar, s, locals)
			case action.OpActionName, action.OpActionNodeType:
				return TsString(c.Imm.(string))
			case action.OpActionField:
				return TsString(c.Imm.(action.Field).Field)
			case action.OpActionCastType:
				return g.castTypeVar(c.Imm.(action.Cast))
			case action.OpActionUncontrolled:
				return "c.p.control === null"
			case action.OpActionTextIn:
				var eqs []string
				for _, lit := range c.Imm.([]string) {
					eqs = append(eqs, a[0]+" === "+TsString(lit))
				}
				return "(" + strings.Join(eqs, " || ") + ")"
			case action.OpActionCastError:
				x := c.Imm.(action.Cast)
				return fmt.Sprintf("castFailure(c, %s, %s, %d, %d)", a[0], g.castTypeVar(x), x.Line, x.Col)
			case action.OpActionTextEqual:
				// == of two strings, which charges as binary does.
				return fmt.Sprintf("c.p.%s(%d, %s, %s)[0] === true", tsActionMethod("binary"), action.BinaryCode("=="), a[0], a[1])
			case action.OpActionFromStr, action.OpActionFromObj, "val.fromBool":
				return a[0]
			case action.OpActionSpend, "seq.step":
				return "c.spend()"
			case "action.kids":
				return "c.p.kidStack.length"
			case "seq.len":
				return a[0] + ".length"
			case "val.unbox":
				// A parameter may hold a box, which stands for its value.
				return "unboxValue(" + a[0] + ")"
			case action.OpActionKeep:
				return fmt.Sprintf("c.p.kidStack.push(%s[%s]!)", a[0], a[1])
			case action.OpActionCopyList:
				return "c.newList(" + a[0] + ")"
			case action.OpActionElemsAbove:
				return "c.p.elemsAbove(" + a[0] + ")"
			case action.OpActionStruct:
				return "c.actionStruct(" + TsString(c.Imm.(action.New).Type) + ")"
			case action.OpActionSetField:
				return fmt.Sprintf("c.actionSetField(%s, %s, %s)", a[0], TsString(c.Imm.(string)), a[1])
			}
			panic("tsgen: action operation " + c.Callee)
		},
	}
}

// castTypeVar returns the constant of the checked type of the cast x.
func (g *Generator) castTypeVar(x action.Cast) string {
	return g.FieldTypeVar(x.Key, x.Check.(types.Ty), tsFieldTypeDecl)
}

// tsCastFailure is the check function castFailure of fieldCheckSource.
var tsCastFailure = strings.TrimSpace(`
// castFailure returns the failure of a cast of v to t, a type that v does not fit.
function castFailure(c: Actx, v: Value, t: FieldType, gline: number, gcol: number): string {
  const e = c.p.makeError(c.start, []);
  return (` + "`cannot cast ${typeName(v)} to ${t.text} at input position ${c.start} (line ${e.line}, column ${e.col})${gline > 0 ? `; cast at grammar position ${gline}:${gcol}` : \"\"}`" + `);
}`)
