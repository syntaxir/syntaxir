package grammar_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A roundTripRun generates n random grammar ASTs from seed with expressions, actions and types
// of a depth of up to depth, and checks the round trip of each that validates.
type roundTripRun struct {
	name  string
	seed  uint64
	n     int
	depth int
	// wide generates every node kind with fields that are often invalid: names of keywords,
	// constants and parameters, unknown operators and pragmas, misplaced blocks, binds and
	// Operators Rules. blocks nests blocks in actions.
	wide, blocks bool
}

// TestFormatRoundTripRandom formats random valid grammar ASTs and parses the text back. The
// parsed grammar must be the formatted one up to the differences that the syntax does not
// spell: groups, a one-item sequence (read back as the item and an empty sequence), a
// one-alternative choice, a negative integer constant (read back as a negation) and a block of
// one statement as the action of a predicate. Formatting the parsed grammar again must give the same grammar. The runs of the
// default tier take a few seconds; the long tier (roundTripRuns in its own file) runs more and
// deeper grammars.
func TestFormatRoundTripRandom(t *testing.T) {
	t.Parallel()
	for _, run := range roundTripRuns {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			g := &astGen{r: rand.New(rand.NewPCG(run.seed, 0)), wide: run.wide, blocks: run.blocks}
			valid, failures := 0, 0
			for i := 0; i < run.n && failures < 5; i++ {
				ast1 := g.grammar(1 + g.r.IntN(run.depth))
				if ast.Validate(ast1) != nil {
					continue
				}
				valid++
				if msg := roundTrip(ast1); msg != "" {
					failures++
					t.Errorf("case %d: %s", i, msg)
				}
			}
			// The generator must keep producing valid grammars, or the run checks nothing.
			if failures == 0 && valid < run.n/10 {
				t.Errorf("only %d of %d random grammars are valid", valid, run.n)
			}
			t.Logf("%d of %d random grammars valid", valid, run.n)
		})
	}
}

// roundTrip formats g, parses the text and compares the trees, and returns a description of the
// first difference, or "".
func roundTrip(g *ast.Grammar) string {
	text := grammar.Format(g)
	back, err := grammar.Load(grammar.FromString("random.sxir", text))
	if err != nil {
		return fmt.Sprintf("the formatted grammar does not parse: %v\n%s", err, text)
	}
	if want, got := normalTree(g, true), normalTree(back, false); !reflect.DeepEqual(want, got) {
		return fmt.Sprintf("the parsed grammar differs at %s\n%s", firstDiff("", want, got), text)
	}
	again := grammar.Format(back)
	back2, err := grammar.Load(grammar.FromString("random.sxir", again))
	if err != nil {
		return fmt.Sprintf("the parsed grammar, formatted again, does not parse: %v\n%s", err, again)
	}
	if want, got := normalTree(back, false), normalTree(back2, false); !reflect.DeepEqual(want, got) {
		return fmt.Sprintf("the parsed grammar, formatted again, differs at %s\n%s", firstDiff("", want, got), again)
	}
	return ""
}

// astGen generates random grammar ASTs.
type astGen struct {
	r            *rand.Rand
	wide, blocks bool
}

func pick[T any](g *astGen, xs ...T) T { return xs[g.r.IntN(len(xs))] }

func (g *astGen) name() string {
	if g.wide && g.r.IntN(8) == 0 {
		return pick(g, "true", "false", "nil")
	}
	if g.wide && g.r.IntN(4) == 0 {
		return pick(g, "true", "nil", "p0", "p1", "node", "List", "startPos", "message",
			"rule", "type", "var", "package", "struct", "terminal", "skip", "operand", "level", "prefix",
			"postfix", "infix", "left", "none", "i", "operators", "error", "_")
	}
	return pick(g, "a", "b", "x", "y", "foo", "T1")
}

func (g *astGen) typeName() string {
	if g.wide && g.r.IntN(5) == 0 {
		return pick(g, "true", "nil", "node", "List", "p0", "int", "_")
	}
	return pick(g, "T", "U", "Node")
}

func (g *astGen) str() string {
	var b strings.Builder
	for range g.r.IntN(4) {
		b.WriteString(pick(g, "a", "\"", "\\", "\n", "\t", "é", "😀", "\x00", " ", "{", "}", "$", "'", "\x7f", "\ufeff"))
	}
	return b.String()
}

func (g *astGen) charClass() *ast.CharClass {
	c := &ast.CharClass{Negated: g.r.IntN(2) == 0}
	for range 1 + g.r.IntN(3) {
		bound := func() rune {
			return pick[rune](g, 'a', 'z', '-', ']', '^', '\\', '\n', 0, 'é', 0x10FFFF, 0x7f, 0x1F600, ' ', '[')
		}
		lo, hi := bound(), bound()
		if hi < lo {
			lo, hi = hi, lo
		}
		if g.r.IntN(2) == 0 {
			hi = lo
		}
		c.Ranges = append(c.Ranges, ast.CharRange{Lo: lo, Hi: hi})
	}
	return c
}

func (g *astGen) expr(d int) ast.Expr {
	if d <= 0 {
		switch g.r.IntN(12) {
		case 0:
			return &ast.Ref{Name: g.name()}
		case 1:
			return &ast.Literal{Value: g.str(), IgnoreCase: g.r.IntN(4) == 0}
		case 2:
			return g.charClass()
		case 3:
			return &ast.Any{}
		case 4:
			return &ast.Cut{}
		case 5:
			return &ast.Top{}
		case 6:
			return &ast.Bottom{}
		case 7:
			return &ast.BeginInput{}
		case 8:
			return &ast.EndInput{}
		case 9:
			return &ast.BeginLine{}
		case 10:
			return &ast.EndLine{}
		default:
			return &ast.Ref{Name: g.name(), Level: pick(g, "", "l1")}
		}
	}
	d--
	if g.wide {
		switch g.r.IntN(12) {
		case 0:
			return g.operators(d)
		case 1:
			a := &ast.Attribute{Name: pick(g, "ignorecase", "missing", "atomic", "stream", "recover", "repeat", "foo", "error"), Pragma: g.r.IntN(2) == 0}
			if g.r.IntN(3) == 0 {
				a.Args = []any{g.expr(d)}
			}
			return &ast.Attributed{Expr: g.expr(d), Attrs: []*ast.Attribute{a}}
		case 2:
			return &ast.Repeat{Expr: g.expr(d), Min: g.r.IntN(4) - 1, Max: g.r.IntN(5) - 2}
		}
	}
	switch g.r.IntN(17) {
	case 0:
		s := &ast.Seq{}
		for range 1 + g.r.IntN(3) {
			s.Items = append(s.Items, g.expr(d))
		}
		return s
	case 1:
		c := &ast.Choice{}
		for range 1 + g.r.IntN(3) {
			c.Alts = append(c.Alts, g.expr(d))
		}
		return c
	case 2:
		return &ast.Group{Expr: g.expr(d)}
	case 3:
		ia := &ast.InlineAction{Expr: g.expr(d)}
		if g.r.IntN(3) == 0 {
			ia.DefaultAction = true
		} else {
			ia.Action = g.action(d)
		}
		return ia
	case 4:
		lo := g.r.IntN(3)
		return &ast.Repeat{Expr: g.expr(d), Min: lo, Max: pick(g, -1, lo, lo+2)}
	case 5:
		return &ast.Optional{Expr: g.expr(d)}
	case 6:
		return &ast.And{Expr: g.expr(d)}
	case 7:
		return &ast.Not{Expr: g.expr(d)}
	case 8:
		return &ast.Recovery{Expr: g.expr(d), Skip: g.expr(d)}
	case 9:
		return &ast.Stream{Expr: g.expr(d)}
	case 10:
		return &ast.Atomic{Expr: g.expr(d)}
	case 11:
		return &ast.Discard{Expr: g.expr(d)}
	case 12:
		return &ast.Capture{Name: g.name(), Expr: g.expr(d)}
	case 13:
		return &ast.Predicate{Action: g.action(d)}
	case 14:
		a := &ast.Attribute{Name: pick(g, "error", "custom", "x")}
		if g.wide && g.r.IntN(3) == 0 {
			a.Name = g.name()
		}
		switch g.r.IntN(4) {
		case 0:
			a.Args = []any{g.r.IntN(5) - 2, g.str(), true, nil}
		case 1:
			a.KwArgs = map[string]any{"message": g.str(), "k": g.expr(d)}
			if g.wide {
				a.KwArgs[g.name()] = pick[any](g, 1, "s", g.expr(d))
			}
		case 2:
			a.Args = []any{g.expr(d)}
		}
		return &ast.Attributed{Expr: g.expr(d), Attrs: []*ast.Attribute{a}}
	default:
		return g.expr(0)
	}
}

func (g *astGen) typ(d int) ast.TypeExpr {
	if d <= 0 {
		if g.wide && g.r.IntN(3) == 0 {
			return &ast.TypeRef{Name: pick(g, "node", "List", "Match", "Error", "true", "nil", "p0", "foo")}
		}
		return &ast.TypeRef{Name: pick(g, "int", "string", "bool", "_", "_|_", "T")}
	}
	d--
	if g.wide && g.r.IntN(6) == 0 {
		a := &ast.TypeApp{Name: pick(g, "node", "List", "foo", "_", "Match", "true")}
		for range g.r.IntN(3) {
			a.Args = append(a.Args, g.typ(d))
		}
		return a
	}
	n := 1 + g.r.IntN(3)
	types := func(extra int) []ast.TypeExpr {
		var out []ast.TypeExpr
		for range n + extra {
			out = append(out, g.typ(d))
		}
		return out
	}
	switch g.r.IntN(9) {
	case 0:
		return &ast.ListType{Elem: g.typ(d)}
	case 1:
		return &ast.OptionalType{Elem: g.typ(d)}
	case 2:
		return &ast.JoinType{Types: types(1)}
	case 3:
		return &ast.MeetType{Types: types(1)}
	case 4:
		return &ast.TupleType{Types: types(g.r.IntN(2) - 1)}
	case 5:
		return &ast.TypeApp{Name: "node", Args: []ast.TypeExpr{g.typ(d)}}
	case 6:
		st := &ast.StructType{}
		for i := range n {
			name := pick(g, "A", "B", "C") + fmt.Sprint(i)
			if g.wide && g.r.IntN(3) == 0 {
				name += g.name()
			}
			st.Fields = append(st.Fields, &ast.Field{Name: name, Type: g.typ(d)})
		}
		return st
	case 7:
		return &ast.TypeGroup{Type: g.typ(d)}
	default:
		return g.typ(0)
	}
}

func (g *astGen) action(d int) ast.ActionExpr {
	if d <= 0 {
		switch g.r.IntN(6) {
		case 0:
			return &ast.IntLit{Value: pick(g, 0, 1, -1, 7, -5, math.MinInt, math.MaxInt)}
		case 1:
			return &ast.StringLit{Value: g.str()}
		case 2:
			return &ast.BoolLit{Value: g.r.IntN(2) == 0}
		case 3:
			return &ast.NilLit{}
		case 4:
			return &ast.CaptureRef{Name: g.name()}
		default:
			return &ast.VarRef{Name: g.name()}
		}
	}
	d--
	if g.wide {
		switch g.r.IntN(14) {
		case 0:
			return &ast.Bind{Name: g.name(), Value: g.action(d)}
		case 1:
			return &ast.CaptureRef{Name: g.name(), Param: g.r.IntN(2) == 0}
		case 2:
			return &ast.FieldAccess{X: g.action(d), Name: pick(g, "x", "startPos", "children", "$y", "$true", "Foo", "message", "$p0")}
		case 3:
			return &ast.Binary{Op: pick(g, "??", "=", "and", "[]", "+"), L: g.action(d), R: g.action(d)}
		case 4:
			return &ast.Unary{Op: pick(g, "+", "~", "-", "!", "not"), X: g.action(d)}
		case 5:
			return &ast.Call{Func: g.name(), Args: []ast.ActionExpr{g.action(d)}}
		case 6:
			b := &ast.Block{}
			for range g.r.IntN(3) {
				b.Statements = append(b.Statements, g.action(d))
			}
			return b
		}
	}
	args := func() []ast.ActionExpr {
		var out []ast.ActionExpr
		for range g.r.IntN(3) {
			out = append(out, g.action(d))
		}
		return out
	}
	switch g.r.IntN(14) {
	case 0:
		return &ast.ActionGroup{Action: g.action(d)}
	case 1:
		return &ast.FieldAccess{X: g.action(d), Name: pick(g, "Start", "$x", "V")}
	case 2:
		c := &ast.Constructor{Type: g.typeName()}
		if g.r.IntN(2) == 0 {
			c.Args = args()
		} else {
			for i := range g.r.IntN(3) {
				name := "F" + fmt.Sprint(i)
				if g.wide && g.r.IntN(3) == 0 {
					name = g.name()
				}
				c.Fields = append(c.Fields, &ast.FieldInit{Name: name, Value: g.action(d)})
			}
		}
		return c
	case 3:
		return &ast.TupleLit{Elems: append(args(), g.action(d))}
	case 4:
		return &ast.ListConstructor{Elem: g.typ(d), Items: args()}
	case 5:
		return &ast.Call{Func: pick(g, "len", "f", "map"), Args: args()}
	case 6:
		var params []string
		for i := range g.r.IntN(3) {
			if g.wide && g.r.IntN(3) == 0 {
				params = append(params, g.name())
			} else {
				params = append(params, pick(g, "p", "q")+fmt.Sprint(i))
			}
		}
		return &ast.Lambda{Params: params, Body: g.action(d)}
	case 7, 8:
		return &ast.Binary{Op: pick(g, "||", "&&", "==", "!=", "<", "<=", ">", ">=", "+", "-", "*", "/", "%", "[]"), L: g.action(d), R: g.action(d)}
	case 9:
		return &ast.Unary{Op: pick(g, "-", "!"), X: g.action(d)}
	case 10:
		return &ast.Cast{X: g.action(d), Type: g.typ(d)}
	case 11:
		if !g.blocks && !g.wide {
			return g.action(0)
		}
		b := &ast.Block{}
		for range 1 + g.r.IntN(3) {
			if g.r.IntN(2) == 0 {
				b.Statements = append(b.Statements, &ast.Bind{Name: g.name(), Value: g.action(d)})
			} else {
				b.Statements = append(b.Statements, g.action(d))
			}
		}
		return b
	default:
		return g.action(0)
	}
}

func (g *astGen) operators(d int) *ast.OperatorsRule {
	o := &ast.OperatorsRule{}
	if g.r.IntN(3) == 0 {
		o.Skip = g.expr(d)
	}
	for range g.r.IntN(3) {
		op := &ast.OperatorsRuleOperand{Expr: g.expr(d)}
		switch g.r.IntN(3) {
		case 0:
			op.Action = g.action(d)
		case 1:
			op.DefaultAction = true
		}
		o.Operands = append(o.Operands, op)
	}
	for range g.r.IntN(3) {
		l := &ast.OperatorsRuleLevel{Name: pick(g, "", "add", g.name())}
		for range g.r.IntN(3) {
			op := &ast.OperatorsRuleOperator{Kind: pick(g, "prefix", "postfix", "infix", "bogus"), Expr: g.expr(d)}
			if op.Kind == "infix" || g.r.IntN(4) == 0 {
				op.Assoc = pick(g, "left", "right", "none", "", "bogus")
			}
			switch g.r.IntN(3) {
			case 0:
				op.Action = g.action(d)
			case 1:
				op.DefaultAction = true
			}
			l.Operators = append(l.Operators, op)
		}
		o.Levels = append(o.Levels, l)
	}
	return o
}

func (g *astGen) grammar(d int) *ast.Grammar {
	out := &ast.Grammar{}
	switch g.r.IntN(4) {
	case 0:
		out.File.Decls = append(out.File.Decls, &ast.TypeDef{Name: g.typeName(), Type: g.typ(d)})
	case 1:
		out.File.Decls = append(out.File.Decls, &ast.VarDef{
			Name:  "v",
			Type:  pick[ast.TypeExpr](g, nil, &ast.TypeRef{Name: "int"}),
			Value: pick[ast.ActionExpr](g, nil, &ast.IntLit{Value: pick(g, math.MinInt, -3, 4)}, &ast.StringLit{Value: g.str()}),
		})
	}
	if g.wide {
		switch g.r.IntN(4) {
		case 0:
			out.File.Decls = append(out.File.Decls, &ast.TypeAlias{Name: g.typeName(), Type: g.typ(d)})
		case 1:
			out.File.Decls = append(out.File.Decls, &ast.VarDef{Name: g.name(), Type: pick[ast.TypeExpr](g, nil, g.typ(d)), Value: pick[ast.ActionExpr](g, nil, g.action(d))})
		}
	}
	rule := &ast.RuleDef{Name: "main", Expr: g.expr(d)}
	if g.wide && g.r.IntN(3) == 0 {
		rule.Expr = g.operators(d)
		if g.r.IntN(2) == 0 {
			rule.Expr = &ast.Attributed{Expr: rule.Expr, Attrs: []*ast.Attribute{{Name: pick(g, "ignorecase", "missing", "foo"), Pragma: g.r.IntN(4) != 0}}}
		}
	}
	if g.wide && g.r.IntN(4) == 0 {
		rule.Attrs = []*ast.Attribute{{Name: pick(g, "start", "ignorecase", "error", "foo"), Pragma: g.r.IntN(2) == 0}}
	}
	switch g.r.IntN(3) {
	case 0:
		rule.Action = g.action(d)
	case 1:
		rule.DefaultAction = true
	}
	if g.r.IntN(3) == 0 {
		rule.Type = g.typ(d)
	}
	out.File.Decls = append(out.File.Decls, rule)
	return out
}

// normalTree returns the JSON tree of g without source positions and with the differences that
// the syntax does not spell taken out. With formatted, it also takes out those of a tree that is
// formatted rather than parsed: a one-item sequence gains an empty sequence, as the parser reads
// it, and a one-alternative choice is its alternative, or, when that is a sequence, gains a
// failing alternative.
func normalTree(g *ast.Grammar, formatted bool) any {
	data, err := ast.MarshalJSON(g)
	if err != nil {
		panic(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		panic(err)
	}
	return normalize(v, formatted)
}

func normalize(v any, formatted bool) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e, formatted)
		}
		return out
	case map[string]any:
		switch x["@type"] {
		case "Group":
			return normalize(x["expr"], formatted)
		case "ActionGroup":
			return normalize(x["action"], formatted)
		case "TypeGroup":
			return normalize(x["type"], formatted)
		}
		out := map[string]any{}
		for k, e := range x {
			if k != "source" && k != "skipSource" {
				out[k] = normalize(e, formatted)
			}
		}
		switch out["@type"] {
		case "Unary":
			// A negative integer constant reads back as the negation of its magnitude.
			if lit, ok := out["x"].(map[string]any); ok && out["op"] == "-" && lit["@type"] == "IntLit" {
				lit["value"] = negate(lit["value"])
				return lit
			}
		case "Predicate":
			if b, ok := out["action"].(map[string]any); ok && b["@type"] == "Block" {
				if st, ok := b["statements"].([]any); ok && len(st) == 1 {
					out["action"] = st[0]
				}
			}
		case "Seq":
			if items := out["items"].([]any); formatted && len(items) == 1 {
				out["items"] = append(items, map[string]any{"@type": "Top"})
			}
		case "Choice":
			if alts := out["alts"].([]any); formatted && len(alts) == 1 {
				if a, ok := alts[0].(map[string]any); ok && a["@type"] == "Seq" {
					out["alts"] = append(alts, map[string]any{"@type": "Bottom"})
				} else {
					return alts[0]
				}
			}
		}
		return out
	}
	return v
}

// negate negates the JSON integer v exactly.
func negate(v any) any {
	n, ok := v.(json.Number)
	switch {
	case !ok:
		panic(fmt.Sprintf("an integer constant of %T", v))
	case n == "0":
		return n
	case strings.HasPrefix(string(n), "-"):
		return n[1:]
	}
	return "-" + n
}

func firstDiff(path string, a, b any) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || x["@type"] != y["@type"] {
			break
		}
		for k, v := range x {
			if !reflect.DeepEqual(v, y[k]) {
				return firstDiff(fmt.Sprintf("%s/%v.%s", path, x["@type"], k), v, y[k])
			}
		}
		for k, v := range y {
			if _, ok := x[k]; !ok {
				return fmt.Sprintf("%s: extra %s=%s", path, k, short(v))
			}
		}
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			break
		}
		for i := range x {
			if !reflect.DeepEqual(x[i], y[i]) {
				return firstDiff(fmt.Sprintf("%s[%d]", path, i), x[i], y[i])
			}
		}
	}
	return fmt.Sprintf("%s: %s, parsed %s", path, short(a), short(b))
}

func short(v any) string {
	data, _ := json.Marshal(v)
	if len(data) > 160 {
		data = append(data[:160], "..."...)
	}
	return string(data)
}
