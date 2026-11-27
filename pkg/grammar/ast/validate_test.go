package ast

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func expressionGrammar(e Expr) *Grammar {
	return &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Expr: e}}}}
}
func actionGrammar(t ActionExpr) *Grammar {
	return &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Expr: &Literal{Value: "a"}, Action: t}}}}
}
func typeGrammar(t TypeExpr) *Grammar {
	return &Grammar{File: File{Decls: []Decl{&TypeAlias{Name: "T", Type: t}}}}
}

func TestValidateRequiredChildren(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"statement", &Grammar{File: File{Decls: []Decl{nil}}}, "$.file.decls[0]"},
		{"type spec", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T"}}}}, "$.file.decls[0].type"},
		{"rule body", expressionGrammar(nil), "$.file.decls[0].expr"},
		{"struct field", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{Fields: []*Field{nil}}}}}}, "$.file.decls[0].type.fields[0]"},
		{"field type", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{Fields: []*Field{{Name: "F"}}}}}}}, "$.file.decls[0].type.fields[0].type"},
		{"alias", typeGrammar(nil), "$.file.decls[0].type"},
		{"type group", typeGrammar(&TypeGroup{}), "$.file.decls[0].type.type"},
		{"action group", actionGrammar(&ActionGroup{}), "$.file.decls[0].action.action"},
		{"list", typeGrammar(&ListType{}), "$.file.decls[0].type.elem"},
		{"optional type", typeGrammar(&OptionalType{}), "$.file.decls[0].type.elem"},
		{"join", typeGrammar(&JoinType{Types: []TypeExpr{nil, &TypeRef{Name: "int"}}}), "$.file.decls[0].type.types[0]"},
		{"sequence", expressionGrammar(&Seq{Items: []Expr{nil}}), "$.file.decls[0].expr.items[0]"},
		{"choice", expressionGrammar(&Choice{Alts: []Expr{nil}}), "$.file.decls[0].expr.alts[0]"},
		{"repeat", expressionGrammar(&Repeat{}), "$.file.decls[0].expr.expr"},
		{"optional", expressionGrammar(&Optional{}), "$.file.decls[0].expr.expr"},
		{"and", expressionGrammar(&And{}), "$.file.decls[0].expr.expr"},
		{"not", expressionGrammar(&Not{}), "$.file.decls[0].expr.expr"},
		{"atomic", expressionGrammar(&Atomic{}), "$.file.decls[0].expr.expr"},
		{"discard", expressionGrammar(&Discard{}), "$.file.decls[0].expr.expr"},
		{"capture", expressionGrammar(&Capture{Name: "value"}), "$.file.decls[0].expr.expr"},
		{"predicate", expressionGrammar(&Predicate{}), "$.file.decls[0].expr.action"},
		{"attributed expression", expressionGrammar(&Attributed{Attrs: []*Attribute{{Name: "x"}}}), "$.file.decls[0].expr.expr"},
		{"attribute", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{nil}}), "$.file.decls[0].expr.attrs[0]"},
		{"attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{&Repeat{}}}}}), "$.file.decls[0].expr.attrs[0].args[0].expr"},
		{"attribute value", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{&IntLit{Value: 1}}}}}), "$.file.decls[0].expr.attrs[0].args[0]"},
		{"operand", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{nil}}), "$.file.decls[0].expr.operands[0]"},
		{"operand expression", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{}}}), "$.file.decls[0].expr.operands[0].expr"},
		{"level", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{nil}}), "$.file.decls[0].expr.levels[0]"},
		{"operator", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{nil}}}}), "$.file.decls[0].expr.levels[0].operators[0]"},
		{"operator expression", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: Infix, Assoc: AssocLeft}}}}}), "$.file.decls[0].expr.levels[0].operators[0].expr"},
		{"member", actionGrammar(&FieldAccess{Name: "Value"}), "$.file.decls[0].action.x"},
		{"field initializer", actionGrammar(&Constructor{Type: "T", Fields: []*FieldInit{nil}}), "$.file.decls[0].action.fields[0]"},
		{"field value", actionGrammar(&Constructor{Type: "T", Fields: []*FieldInit{{Name: "F"}}}), "$.file.decls[0].action.fields[0].value"},
		{"call", actionGrammar(&Call{Func: "len", Args: []ActionExpr{nil}}), "$.file.decls[0].action.args[0]"},
		{"tuple literal", actionGrammar(&TupleLit{Elems: []ActionExpr{nil}}), "$.file.decls[0].action.elems[0]"},
		{"lambda", actionGrammar(&Lambda{}), "$.file.decls[0].action.body"},
		{"binary left", actionGrammar(&Binary{Op: "+", R: &IntLit{Value: 1}}), "$.file.decls[0].action.l"},
		{"binary right", actionGrammar(&Binary{Op: "+", L: &IntLit{Value: 1}}), "$.file.decls[0].action.r"},
		{"unary", actionGrammar(&Unary{Op: "-"}), "$.file.decls[0].action.x"},
		{"bind", expressionGrammar(&Predicate{Action: &Bind{Name: "x"}}), "$.file.decls[0].expr.action.value"},
		{"conflicting rule default", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "main", Expr: &Top{}, DefaultAction: true, Action: &NilLit{}}}}}, "$.file.decls[0].action"},
		{"conflicting operand default", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}, DefaultAction: true, Action: &NilLit{}}}}), "$.file.decls[0].expr.operands[0].action"},
		{"conflicting operator default", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: Infix, Assoc: AssocLeft, Expr: &Top{}, DefaultAction: true, Action: &NilLit{}}}}}}), "$.file.decls[0].expr.levels[0].operators[0].action"},
		{"recovery body", expressionGrammar(&Recovery{Skip: &Top{}}), "$.file.decls[0].expr.expr"},
		{"recovery skip", expressionGrammar(&Recovery{Expr: &Top{}}), "$.file.decls[0].expr.skip"},
		{"stream body", expressionGrammar(&Stream{}), "$.file.decls[0].expr.expr"},
		{"negative minimum", expressionGrammar(&Repeat{Expr: &Top{}, Min: -1, Max: -1}), "$.file.decls[0].expr.min"},
		{"reversed bounds", expressionGrammar(&Repeat{Expr: &Top{}, Min: 2, Max: 1}), "$.file.decls[0].expr.max"},
		{"maximum below -1", expressionGrammar(&Repeat{Expr: &Top{}, Max: -2}), "$.file.decls[0].expr.max"},
		{"least maximum", expressionGrammar(&Repeat{Expr: &Top{}, Max: math.MinInt}), "$.file.decls[0].expr.max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertValidationError(t, Validate(tc.g), tc.path)
			data, err := MarshalJSON(tc.g)
			if err != nil {
				t.Fatal(err)
			}
			got, err := UnmarshalJSON(data)
			if got != nil {
				t.Fatal("invalid grammar returned")
			}
			assertValidationError(t, err, tc.path)
		})
	}
}

func assertValidationError(t *testing.T, err error, path string) {
	t.Helper()
	var e *ValidationError
	if !errors.As(err, &e) || e.Path != path {
		t.Fatalf("got %v; want ValidationError at %s", err, path)
	}
}

func TestValidateTypedNil(t *testing.T) {
	// Cover every registered interface node, including leaf nodes whose
	// pointers were previously dereferenced by formatting or compilation.
	for name, typ := range registry {
		node := reflect.Zero(reflect.PointerTo(typ)).Interface()
		var g *Grammar
		switch n := node.(type) {
		case Decl:
			g = &Grammar{File: File{Decls: []Decl{n}}}
		case TypeExpr:
			g = typeGrammar(n)
		case Expr:
			g = expressionGrammar(n)
		case ActionExpr:
			g = actionGrammar(n)
		default:
			t.Fatalf("uncovered node type %s", name)
		}
		t.Run(name, func(t *testing.T) {
			var e *ValidationError
			if !errors.As(Validate(g), &e) || !strings.Contains(e.Msg, "nil") {
				t.Fatal("typed nil was not rejected")
			}
		})
	}
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"attribute expression", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "recover", KwArgs: map[string]any{"skip": (*Ref)(nil)}}}}), `$.file.decls[0].expr.attrs[0].kwArgs["skip"]`},
		{"rule type", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Type: (*TypeRef)(nil), Expr: &Top{}}}}}, "$.file.decls[0].type"},
		{"skip", expressionGrammar(&OperatorsRule{Skip: (*Top)(nil)}), "$.file.decls[0].expr.skip"},
		{"operand action", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}, Action: (*NilLit)(nil)}}}), "$.file.decls[0].expr.operands[0].action"},
		{"operator action", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: Prefix, Expr: &Top{}, Action: (*NilLit)(nil)}}}}}), "$.file.decls[0].expr.levels[0].operators[0].action"},
	} {
		t.Run(tc.name, func(t *testing.T) { assertValidationError(t, Validate(tc.g), tc.path) })
	}
}

func TestValidateCyclesAndSharing(t *testing.T) {
	expr := &Optional{}
	expr.Expr = expr
	assertValidationError(t, Validate(expressionGrammar(expr)), "$.file.decls[0].expr.expr")
	typ := &ListType{}
	typ.Elem = typ
	assertValidationError(t, Validate(typeGrammar(typ)), "$.file.decls[0].type.elem")
	actionExpr := &Unary{Op: "-"}
	actionExpr.X = &FieldAccess{X: actionExpr, Name: "F"}
	assertValidationError(t, Validate(actionGrammar(actionExpr)), "$.file.decls[0].action.x.x")
	groupType := &TypeGroup{}
	groupType.Type = groupType
	assertValidationError(t, Validate(typeGrammar(groupType)), "$.file.decls[0].type.type")
	groupAction := &ActionGroup{}
	groupAction.Action = groupAction
	assertValidationError(t, Validate(actionGrammar(groupAction)), "$.file.decls[0].action.action")
	shared := &Optional{Expr: &Literal{Value: "a"}}
	g := expressionGrammar(&Seq{Items: []Expr{shared, shared}})
	before, err := MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(g); err != nil {
		t.Fatal(err)
	}
	if after, err := MarshalJSON(g); err != nil || string(after) != string(before) {
		t.Fatalf("validation mutated grammar: %v", err)
	}
}

func TestValidatePositionAndOrder(t *testing.T) {
	g := expressionGrammar(&Seq{Items: []Expr{&Repeat{SourceInfo: SourceInfo{Pos: Pos{Line: 4, Col: 7}}, Min: -1}, nil}})
	assertValidationError(t, Validate(g), "$.file.decls[0].expr.items[0].min")
	e := Validate(g).(*ValidationError)
	if e.Pos != (Pos{Line: 4, Col: 7}) || !strings.HasPrefix(e.Error(), "4:7: ") {
		t.Fatal(e)
	}
	rd := g.File.Decls[0].(*RuleDef)
	rd.Pos = Pos{Line: 2, Col: 3}
	rd.Expr = &Optional{}
	if e := Validate(g).(*ValidationError); e.Pos != rd.Pos {
		t.Fatal(e)
	}
}

func TestValidateCompatibility(t *testing.T) {
	for _, g := range []*Grammar{
		{},
		expressionGrammar(&Repeat{Expr: &Top{}, Max: -1}),
		expressionGrammar(&Repeat{Expr: &Top{}, Min: 0, Max: 0}),
		expressionGrammar(&Repeat{Expr: &Top{}, Min: 2, Max: 2}),
		expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}}}, Levels: []*OperatorsRuleLevel{{}}}),
		sampleGrammar(),
	} {
		if err := Validate(g); err != nil {
			t.Fatal(err)
		}
	}
	// Validation is iterative, independent of nesting depth. A chain of pragmas
	// nests deep in the tree and opens no levels in its text.
	var expr Expr = &Top{}
	for i := 0; i < 49000; i++ {
		expr = &Atomic{Expr: expr}
	}
	if err := Validate(expressionGrammar(expr)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateASCIIIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"empty rule", &Grammar{File: File{Decls: []Decl{&RuleDef{Expr: &Top{}}}}}, "$.file.decls[0].name"},
		{"empty reference", expressionGrammar(&Ref{}), "$.file.decls[0].expr.name"},
		{"empty capture", expressionGrammar(&Capture{Expr: &Top{}}), "$.file.decls[0].expr.name"},
		{"empty attribute", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{}}}), "$.file.decls[0].expr.attrs[0].name"},
		{"empty attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"": "x"}}}}), "$.file.decls[0].expr.attrs[0].kwArgs[\"\"]"},
		{"empty lambda parameter", actionGrammar(&Lambda{Params: []string{""}, Body: &IntLit{Value: 1}}), "$.file.decls[0].action.params[0]"},
		{"package", &Grammar{File: File{Pkg: Pkg{Name: "名前"}}}, "$.file.pkg.name"},
		{"rule", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "café", Expr: &Top{}}}}}, "$.file.decls[0].name"},
		{"reference", expressionGrammar(&Ref{Name: "é"}), "$.file.decls[0].expr.name"},
		{"level", expressionGrammar(&Ref{Name: "expr", Level: "名"}), "$.file.decls[0].expr.level"},
		{"attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"méssage": "x"}}}}), "$.file.decls[0].expr.attrs[0].kwArgs[\"méssage\"]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertValidationError(t, Validate(tc.g), tc.path)
		})
	}
	if err := Validate(expressionGrammar(&Literal{Value: "名前😀"})); err != nil {
		t.Fatalf("Unicode literal rejected: %v", err)
	}
	if err := Validate(expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}}}, Levels: []*OperatorsRuleLevel{{}}})); err != nil {
		t.Fatalf("unnamed level rejected: %v", err)
	}
}

func TestValidateInvalidUTF8(t *testing.T) {
	const bad = "a\xc3"
	// line is a layout line break whose trailing comment is not valid UTF-8; at is its field.
	line := func() *LineBreak { return &LineBreak{Trailing: &Comment{Text: "//" + bad}} }
	const at = ".trailing.text"
	lit := func() *Literal { return &Literal{Value: "a"} }
	rule := func(r *RuleDef) *Grammar {
		if r.Name == "" {
			r.Name = "p"
		}
		if r.Expr == nil {
			r.Expr = &Literal{Value: "a"}
		}
		return &Grammar{File: File{Decls: []Decl{r}}}
	}
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"literal", expressionGrammar(&Literal{Value: bad}), "$.file.decls[0].expr.value"},
		{"ignorecase literal", expressionGrammar(&Literal{Value: bad, IgnoreCase: true}), "$.file.decls[0].expr.value"},
		{"action string", actionGrammar(&StringLit{Value: bad}), "$.file.decls[0].action.value"},
		{"attribute keyword argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"message": bad}}}}), "$.file.decls[0].expr.attrs[0].kwArgs[\"message\"]"},
		{"attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{1, bad}}}}), "$.file.decls[0].expr.attrs[0].args[1]"},
		{"leading comment", rule(&RuleDef{SourceInfo: SourceInfo{LeadingComments: bad}}), "$.file.decls[0].source.leadingComments"},
		{"trailing comment", rule(&RuleDef{SourceInfo: SourceInfo{TrailingComments: []string{"ok", bad}}}), "$.file.decls[0].source.trailingComments[1]"},
		{"detached comment", rule(&RuleDef{SourceInfo: SourceInfo{LeadingDetachedComments: []string{bad}}}), "$.file.decls[0].source.leadingDetachedComments[0]"},
		{"expression comment", expressionGrammar(&Literal{Value: "a", SourceInfo: SourceInfo{LeadingComments: bad}}), "$.file.decls[0].expr.source.leadingComments"},
		{"keyword comment", rule(&RuleDef{KeywordComments: bad}), "$.file.decls[0].keywordComments"},
		{"keyword detached comment", rule(&RuleDef{KeywordDetachedComments: []string{bad}}), "$.file.decls[0].keywordDetachedComments[0]"},
		{"header comment", rule(&RuleDef{HeaderComments: []string{bad}}), "$.file.decls[0].headerComments[0]"},
		{"file leading comment", &Grammar{File: File{SourceInfo: SourceInfo{LeadingComments: "// " + bad + "\n"}}}, "$.file.source.leadingComments"},
		{"file trailing comment", &Grammar{File: File{SourceInfo: SourceInfo{TrailingComments: []string{"// " + bad + "\n"}}}}, "$.file.source.trailingComments[0]"},
		{"file detached comment", &Grammar{File: File{SourceInfo: SourceInfo{LeadingDetachedComments: []string{"// " + bad + "\n"}}}}, "$.file.source.leadingDetachedComments[0]"},
		{"package comment", &Grammar{File: File{Pkg: Pkg{Name: "p", SourceInfo: SourceInfo{LeadingComments: bad}}}}, "$.file.pkg.source.leadingComments"},
		{"file end comment text", &Grammar{File: File{EndBreak: &LineBreak{Comments: []*Comment{{Text: "//" + bad, Content: "ok"}}}}}, "$.file.endBreak.comments[0].text"},
		{"file end comment content", &Grammar{File: File{EndBreak: &LineBreak{Comments: []*Comment{{Text: "// ok", Content: bad}}}}}, "$.file.endBreak.comments[0].content"},
		{"file header comment", &Grammar{File: File{HeaderComments: []*Comment{{Text: "/*" + bad + "*/"}}}}, "$.file.headerComments[0].text"},
		{"package break", &Grammar{File: File{Pkg: Pkg{Name: "p", Break: &LineBreak{Trailing: &Comment{Text: "//" + bad}}}}}, "$.file.pkg.break.trailing.text"},
		{"rule break", rule(&RuleDef{Break: &LineBreak{Comments: []*Comment{{Text: "// ok"}, {Text: "//" + bad}}}}), "$.file.decls[0].break.comments[1].text"},
		{"comment run", rule(&RuleDef{CloseBreak: &LineBreak{Trailing: &Comment{Text: "/* a */ // b", Run: []*Comment{{Text: "/* a */"}, {Text: "//" + bad}}}}}), "$.file.decls[0].closeBreak.trailing.run[1].text"},
		{"group close break", expressionGrammar(&Group{Expr: &Top{}, CloseBreak: &LineBreak{Last: &Comment{Text: "//" + bad}}}), "$.file.decls[0].expr.closeBreak.last.text"},
		{"sequence break", expressionGrammar(&Seq{Items: []Expr{&Top{}, &Top{}}, Breaks: []*LineBreak{nil, {Comments: []*Comment{{Text: "//" + bad}}}}}), "$.file.decls[0].expr.breaks[1].comments[0].text"},
		{"layout gap", expressionGrammar(&Literal{Value: "a", SourceInfo: SourceInfo{Layout: &SourceLayout{Close: &Gap{Comments: []*Comment{{Text: "/*" + bad + "*/"}}}}}}), "$.file.decls[0].expr.source.layout.close.comments[0].text"},
		{"repeat gap", expressionGrammar(&Repeat{Expr: &Top{}, Min: 1, Max: 2, Spelling: &RepeatSpelling{HasMin: true, Gaps: []RepeatGap{{}, {Comments: []*Comment{{Text: "/*" + bad + "*/"}}}}}}), "$.file.decls[0].expr.spelling.gaps[1].comments[0].text"},
		// Each node kind that holds layout of its own, built by hand. The parser fills each of
		// these layouts, but the insertion property of the parser reaches only those its corpus
		// has, so these cases pin the check of every kind.
		{"variable break", &Grammar{File: File{Decls: []Decl{&VarDef{Name: "v", Value: &IntLit{Value: 1}, Break: line()}}}}, "$.file.decls[0].break" + at},
		{"action group", actionGrammar(&ActionGroup{Action: &IntLit{Value: 1}, OpenBreak: line()}), "$.file.decls[0].action.openBreak" + at},
		{"tuple", actionGrammar(&TupleLit{Elems: []ActionExpr{&IntLit{Value: 1}, &IntLit{Value: 2}}, BodyBreak: line()}), "$.file.decls[0].action.bodyBreak" + at},
		{"list constructor", actionGrammar(&ListConstructor{Elem: &TypeRef{Name: "int"}, CloseBreak: line()}), "$.file.decls[0].action.closeBreak" + at},
		{"call", actionGrammar(&Call{Func: "f", CloseBreak: line()}), "$.file.decls[0].action.closeBreak" + at},
		{"binary", actionGrammar(&Binary{Op: "+", L: &IntLit{Value: 1}, R: &IntLit{Value: 2}, BodyBreak: line()}), "$.file.decls[0].action.bodyBreak" + at},
		{"block", expressionGrammar(&Predicate{Action: &Block{Statements: []ActionExpr{&BoolLit{Value: true}}, Breaks: []*LineBreak{line()}}}), "$.file.decls[0].expr.action.breaks[0]" + at},
		{"predicate", expressionGrammar(&Predicate{Action: &BoolLit{Value: true}, OpenBreak: line()}), "$.file.decls[0].expr.openBreak" + at},
		{"type group", typeGrammar(&TypeGroup{Type: &TypeRef{Name: "int"}, CloseBreak: line()}), "$.file.decls[0].type.closeBreak" + at},
		{"tuple type", typeGrammar(&TupleType{Types: []TypeExpr{&TypeRef{Name: "int"}, &TypeRef{Name: "bool"}}, OpenBreak: line()}), "$.file.decls[0].type.openBreak" + at},
		{"type application", typeGrammar(&TypeApp{Name: "node", Args: []TypeExpr{&TypeRef{Name: "int"}}, BodyBreak: line()}), "$.file.decls[0].type.bodyBreak" + at},
		{"attribute arguments", expressionGrammar(&Attributed{Expr: lit(), Attrs: []*Attribute{{Name: "error", Args: []any{"m"}, ArgsLayout: &ListLayout{Elems: []*SourceLayout{{Trail: []*Comment{{Text: "//" + bad}}}}}}}}), "$.file.decls[0].expr.attrs[0].argsLayout.elems[0].trail[0].text"},
		{"attributed", expressionGrammar(&Attributed{Expr: lit(), Attrs: []*Attribute{{Name: "foo"}}, Breaks: []*LineBreak{line()}}), "$.file.decls[0].expr.breaks[0]" + at},
		{"level call", expressionGrammar(&Ref{Name: "p", Level: "l", LevelGaps: &LevelGaps{After: &Gap{Comments: []*Comment{{Text: "/*" + bad + "*/"}}}}}), "$.file.decls[0].expr.levelGaps.after.comments[0].text"},
		{"stream", expressionGrammar(&Stream{Expr: lit(), Break: line()}), "$.file.decls[0].expr.break" + at},
		{"recovery", expressionGrammar(&Recovery{Expr: lit(), Skip: lit(), Break: line()}), "$.file.decls[0].expr.break" + at},
		{"atomic", expressionGrammar(&Atomic{Expr: lit(), Break: line()}), "$.file.decls[0].expr.break" + at},
		{"type break", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &TypeRef{Name: "int"}, Break: line()}}}}, "$.file.decls[0].break" + at},
		{"type alias break", &Grammar{File: File{Decls: []Decl{&TypeAlias{Name: "T", Type: &TypeRef{Name: "int"}, Break: line()}}}}, "$.file.decls[0].break" + at},
		{"constructor", actionGrammar(&Constructor{Type: "T", OpenBreak: line()}), "$.file.decls[0].action.openBreak" + at},
		{"lambda", actionGrammar(&Lambda{Params: []string{"x"}, Body: &NilLit{}, ParamsLayout: &ListLayout{CloseBreak: line()}}), "$.file.decls[0].action.paramsLayout.closeBreak" + at},
		{"struct", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{CloseBreak: line()}}}}}, "$.file.decls[0].type.closeBreak" + at},
		{"struct field", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{Fields: []*Field{{Name: "f", Type: &TypeRef{Name: "int"}, Break: line()}}}}}}}, "$.file.decls[0].type.fields[0].break" + at},
		{"struct field header", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{Fields: []*Field{{Name: "f", Type: &TypeRef{Name: "int"}, HeaderTokens: []*Comment{{Text: "/*" + bad + "*/"}}}}}}}}}, "$.file.decls[0].type.fields[0].headerTokens[0].text"},
		{"choice", expressionGrammar(&Choice{Alts: []Expr{lit(), lit()}, Breaks: []*LineBreak{nil, line()}}), "$.file.decls[0].expr.breaks[1]" + at},
		{"operand", rule(&RuleDef{Expr: &OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: lit(), Break: line()}}}}), "$.file.decls[0].expr.operands[0].break" + at},
		{"level", rule(&RuleDef{Expr: &OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: lit()}}, Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: "prefix", Expr: lit()}}, CloseBreak: line()}}}}), "$.file.decls[0].expr.levels[0].closeBreak" + at},
		{"operator", rule(&RuleDef{Expr: &OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: lit()}}, Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: "prefix", Expr: lit(), Break: line()}}}}}}), "$.file.decls[0].expr.levels[0].operators[0].break" + at},
		{"operators open comment", rule(&RuleDef{Expr: &OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Literal{Value: "a"}}}, OpenComments: []*Comment{{Text: "//" + bad}}}}), "$.file.decls[0].expr.openComments[0].text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.g)
			assertValidationError(t, err, tc.path)
			if !strings.Contains(err.Error(), "not valid UTF-8") {
				t.Fatalf("got %v; want an invalid UTF-8 error", err)
			}
		})
	}
	// The replacement character itself, and every other scalar value, stays valid.
	for _, s := range []string{"\uFFFD", "名前😀", "\x00", "\U0010FFFF"} {
		if err := Validate(expressionGrammar(&Literal{Value: s})); err != nil {
			t.Fatalf("literal %q rejected: %v", s, err)
		}
		if err := Validate(actionGrammar(&StringLit{Value: s})); err != nil {
			t.Fatalf("string constant %q rejected: %v", s, err)
		}
	}
}

func TestValidateEmptyCharClass(t *testing.T) {
	for _, negated := range []bool{false, true} {
		for _, ranges := range [][]CharRange{nil, {}} {
			assertValidationError(t, Validate(expressionGrammar(&CharClass{Ranges: ranges, Negated: negated})), "$.file.decls[0].expr.ranges")
		}
	}
}

func TestJSONMalformedAndOverflow(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"null", "$"},
		{`{"file":{"decls":[{"@type":"RuleDef","name":"p"}]}}`, "$.file.decls[0].expr"},
		{`{"file":{"decls":[{"@type":"TypeDef","name":"T","type":{"@type":"StructType","fields":[null]}}]}}`, "$.file.decls[0].type.fields[0]"},
		{`{"file":{"decls":[{"@type":"RuleDef","name":"p","expr":{"@type":"CharClass","ranges":[{"lo":4294967296,"hi":97}]}}]}}`, "$.file.decls[0].expr.ranges[0].lo"},
	} {
		if _, err := UnmarshalJSON([]byte(tc.input)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("got %v; want %s", err, tc.want)
		}
	}
	if _, err := UnmarshalJSON([]byte("{\"statements\":[]} \n\t")); err != nil {
		t.Fatal(err)
	}
	// Existing permissive unknown-field policy is preserved.
	if _, err := UnmarshalJSON([]byte(`{"unknown":true,"statements":[]}`)); err != nil {
		t.Fatal(err)
	}
}
