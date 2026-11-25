package print

import (
	"fmt"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// Action expressions printed from Exec IR.
//
// Every action expression is printed from its function in the Action module of the table, which the Action segment of
// the pipeline lowered and transformed: the calls of the compositions of the library (ActionBinary, ...) with the
// failure of each checked after it, and the leaves, which read the captures and variables of the action.

// The library methods that print the operations action.nodeType (of the Node library) and
// action.castError.
const (
	rtActionNodeType   = "ActionNodeType"
	rtActionCastFailed = "ActionCastFailed"
	rtActionStruct     = "ActionStruct"
	rtActionSetField   = "ActionSetField"
)

// actionBody returns the body of a function that returns the value of the action expression of the action
// site i and its failure, printed from its function in the Action module of the table. The scope s and the locals
// resolve the leaves (leaf).
func (g *Generator) actionBody(i int, s *check.Scope, locals map[string]string) string {
	m, f := g.SiteFunc(i, false)
	return g.siteBody(m, f, s, locals)
}

// actionStmts returns the statements that compute the value of the action of the rule at the action site i into a
// local that they declare, and the name of the local, printed from the function in place of the action
// (action.Site.Result), whose failure ends the parse. The locals of the computation are scoped to a block.
func (g *Generator) actionStmts(i int, s *check.Scope, locals map[string]string) (string, string) {
	m, f := g.SiteFunc(i, true)
	res := fmt.Sprintf("tr%d", g.LoweredActions)
	// One function prints the actions of several flat rules in place: each assigns a local of its own.
	f = code.CloneFunc(f)
	action.Rename(f, map[string]string{action.SiteOf(f).Result: res})
	return fmt.Sprintf("var %s any\n{\n%s}\n", res, g.siteBody(m, f, s, locals)), res
}

// siteBody prints the body of the function f of the Action module m.
func (g *Generator) siteBody(m *code.Module, f *code.Func, s *check.Scope, locals map[string]string) string {
	res := Body(m, f, g.actionTarget(s, locals), nil)
	g.LoweredActions++
	return res.Text
}

// leaf returns the expression that reads the capture (or, if variable is set, the variable) name of
// an action: a local that holds it (a parameter or $lhs, $rhs and $op, which may hold boxes that
// stand for their values), the local that a frame printed in place keeps it in, or the slot of the
// scope s.
func (g *Generator) leaf(name string, variable bool, s *check.Scope, locals map[string]string) string {
	if variable {
		return fmt.Sprintf("c.Var(%d)", g.Prog.Vars.Index(name))
	}
	if l, ok := locals[name]; ok {
		return g.pick("UnboxValue(", "(") + l + ")"
	}
	if g.caps != nil {
		return g.caps[name]
	}
	return fmt.Sprintf("c.Cap(%d)", s.Slots[name])
}

// actionTarget returns the Go implementation of the operations of a lowered action expression whose leaves the scope
// s and the locals resolve.
func (g *Generator) actionTarget(s *check.Scope, locals map[string]string) *Target {
	handles := map[string]string{exec.Datum: "any", action.Fail: "any", action.Str: "string", action.Name: "string",
		action.Act: "*" + g.ctxType(), action.FType: "*FieldType", action.NodeType: g.pick("*nodeTypeKinds", "uint16"),
		action.Seq: g.pick("[]*Node", "[]any"), action.Obj: g.pick("*Node", "TVal"), action.Operand: "TOperand"}
	return &Target{
		Handle: func(name string) string { return handles[name] },
		FuncName: func(name string) string {
			n, ok := ActionExports[name]
			if !ok {
				panic("gogen: the library does not export the composition " + name)
			}
			if g.Logical && name == "hull" {
				// The list or tuple built counts one unit for each element.
				n += "Logical"
			}
			return "c.P." + n
		},
		Op: func(c *code.Call, a []string) string {
			switch c.Callee {
			case action.OpActionConst:
				switch v := c.Imm.(action.Const).Value.(type) {
				case nil:
					return "any(nil)"
				case string:
					return "any(" + strconv.Quote(v) + ")"
				default:
					return fmt.Sprintf("any(%v)", v)
				}
			case action.OpActionRead:
				x := c.Imm.(action.Read)
				return g.leaf(x.Name, x.Kind == action.ReadVar, s, locals)
			case action.OpActionName:
				return strconv.Quote(c.Imm.(string))
			case action.OpActionField:
				return strconv.Quote(c.Imm.(action.Field).Field)
			case action.OpActionNodeType:
				if g.typed() {
					return g.nodeTypeID(c.Imm.(string))
				}
				return "c.P." + rtActionNodeType + "(" + strconv.Quote(c.Imm.(string)) + ")"
			case action.OpActionCastType:
				return g.castTypeVar(c.Imm.(action.Cast))
			case action.OpActionRaise:
				return "c.ActionRaise(" + a[0] + ")"
			case action.OpActionCastError:
				x := c.Imm.(action.Cast)
				return fmt.Sprintf("c.%s(%s, %s, %d, %d)", rtActionCastFailed, a[0], g.castTypeVar(x), x.Line, x.Col)
			case action.OpActionTextEqual:
				return "c.P." + g.pickLogical("TextEqual") + "(" + a[0] + ", " + a[1] + ")"
			case action.OpActionFromStr, action.OpActionFromObj, "val.fromBool":
				return "any(" + a[0] + ")"
			case "action.kids":
				return "len(c.P.KidStack)"
			case action.OpActionElemsAbove:
				return "c.P.Kids(" + a[0] + ")"
			case "units.add":
				return "c.P.UnitsSized(" + a[0] + ")"
			case "units.items":
				return "c.P.UnitsOfItems(" + a[0] + ", " + a[1] + ")"
			case "units.since":
				return "c.P.UnitsSized(len(c.P.KidStack) - " + a[0] + ")"
			case "seq.step":
				// Only the visits of logical units step (ActionOptions.Visits): each is one unit.
				return "c.P.Fuel--"
			case "seq.len":
				return "len(" + a[0] + ")"
			case "val.unbox":
				// A parameter may hold a box, which stands for its value.
				return g.pick("UnboxValue(", "(") + a[0] + ")"
			case action.OpActionKeep:
				return fmt.Sprintf("c.P.KidStack = append(c.P.KidStack, %s[%s])", a[0], a[1])
			case action.OpActionFieldOperand:
				return fmt.Sprintf("c.ActionFieldOperand(%s, %q)", a[0], c.Imm.(string))
			case action.OpActionOperand:
				return "TOperand{V: " + a[0] + "}"
			case action.OpActionCompare:
				return fmt.Sprintf("c.ActionCompare(%s.%s, %s, %s)", libAlias, action.OperatorIdent(c.Imm.(string), false, false), a[0], a[1])
			case action.OpActionUncontrolled:
				return "c.P.Control == nil"
			case action.OpActionTextIn:
				var eqs []string
				for _, lit := range c.Imm.([]string) {
					eqs = append(eqs, a[0]+" == "+strconv.Quote(lit))
				}
				return "(" + strings.Join(eqs, " || ") + ")"
			case action.OpActionCopyList:
				return g.pick("any(c."+g.pickLogical("NewList")+"(", "(c."+g.pickLogical("NewList")+"(") + a[0] + "))"
			case action.OpActionStruct:
				t := c.Imm.(action.New)
				return fmt.Sprintf("c.%s(%q, %d)", rtActionStruct, t.Type, t.Fields)
			case action.OpActionSetField:
				return fmt.Sprintf("c.%s(%s, %q, %s)", rtActionSetField, a[0], c.Imm.(string), a[1])
			}
			if t, ok := c.Imm.(action.New); ok && strings.HasPrefix(c.Callee, action.OpActionNew) {
				// The typed library calls the constructor of the type (tmk_T, see typedLib).
				return "tmk_" + t.Type + "(" + strings.Join(append([]string{"c", strconv.FormatBool(t.Final)}, a...), ", ") + ")"
			}
			panic("gogen: action operation " + c.Callee)
		},
		Stmt: func(c *code.Call, a []string, dst []string, define bool) (string, bool) {
			if c.Callee != action.OpActionTextIn || len(dst) != 1 || dst[0] == "_" {
				return "", false
			}
			// One switch over the literals, which the Go compiler can dispatch on the length and
			// the bytes of the text instead of comparing it with each literal in turn.
			cases := make([]string, len(c.Imm.([]string)))
			for i, lit := range c.Imm.([]string) {
				cases[i] = strconv.Quote(lit)
			}
			init := " = "
			if define {
				init = " := "
			}
			return dst[0] + init + "false\nswitch " + a[0] + " {\ncase " + strings.Join(cases, ", ") + ":\n\t" + dst[0] + " = true\n}", true
		},
		Prec: goPrec,
		Name: actionConstName,
		Pure: func(c *code.Call) bool {
			switch c.Callee {
			case action.OpActionConst, action.OpActionName, action.OpActionField, action.OpActionFromStr, "val.fromBool",
				"val.unbox", "seq.len", action.OpActionOperand, action.OpActionTextIn:
				return true
			}
			return false
		},
	}
}

// loweredActionNames returns the names of the library functions and methods that only lowered action expressions
// call: the compositions of libgo.GoActionExports and the operations of the leaves that the library
// exports for them.
func loweredActionNames() []string {
	// ActionHullLogical is ActionHull under logical units (design record 076).
	names := []string{rtActionNodeType, rtActionCastFailed, rtActionStruct, rtActionSetField, "ActionRaise", "ActionFieldOperand", "ActionCompare", "ActionHullLogical"}
	for _, n := range ActionExports {
		names = append(names, n)
	}
	return names
}

// castTypeVar returns the variable of the checked type of the cast x.
func (g *Generator) castTypeVar(x action.Cast) string {
	return g.FieldTypeVar(x.Key, x.Check.(types.Ty), func(v string, t *types.FieldType) string {
		return fmt.Sprintf("var %s = %s\n", v, goFieldType(t))
	})
}

// actionConstName returns the constant of the library that names the code v that the composition callee
// takes as its argument number arg: the operator of binary, unary and logic, and the kind of a member
// read.
func actionConstName(callee string, arg int, v int64) (string, bool) {
	var names []string
	switch {
	case callee == "binary" && arg == 0:
		names = action.BinaryNames
	case callee == "unary" && arg == 0:
		names = action.UnaryNames
	case callee == "logic" && arg == 0:
		names = action.LogicalNames
	case callee == "member" && arg == 1:
		if v < 0 || int(v) >= len(action.MemberKinds) {
			return "", false
		}
		return libAlias + ".Member" + action.MemberKinds[v].Name, true
	default:
		return "", false
	}
	if v < 0 || int(v) >= len(names) {
		return "", false
	}
	return libAlias + "." + action.Ident(names[v]), true
}
