package ast

import (
	"strings"
	"testing"
)

// lambdaGrammar is a rule whose action is a lambda with the parameters params
// and the body body.
func lambdaGrammar(params []string, body ActionExpr) *Grammar {
	return actionGrammar(&Lambda{Params: params, Body: body})
}

// TestValidateUnwritable checks that validation rejects the trees that grammar
// syntax cannot write, each at the node that has no spelling there.
func TestValidateUnwritable(t *testing.T) {
	block := func() *Block { return &Block{Statements: []ActionExpr{&IntLit{Value: 1}}} }
	bind := func() *Bind { return &Bind{Name: "x", Value: &IntLit{Value: 1}} }
	ops := func() *OperatorsRule {
		return &OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Ref{Name: "num"}}}}
	}
	operator := func(kind, assoc string) *Grammar {
		return expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: kind, Assoc: assoc, Expr: &Literal{Value: "+"}}}}}})
	}
	pragma := func(name string) *Attribute { return &Attribute{Name: name, Pragma: true} }
	for _, tc := range []struct {
		name, path, message string
		g                   *Grammar
	}{
		{"empty sequence", "$.file.decls[0].expr.items", "at least one item", expressionGrammar(&Seq{})},
		{"empty choice", "$.file.decls[0].expr.alts", "at least one alternative", expressionGrammar(&Choice{})},
		{"attributed without attributes", "$.file.decls[0].expr.attrs", "at least one attribute", expressionGrammar(&Attributed{Expr: &Top{}})},
		{"empty join", "$.file.decls[0].type.types", "at least two types", typeGrammar(&JoinType{})},
		{"one-type join", "$.file.decls[0].type.types", "at least two types", typeGrammar(&JoinType{Types: []TypeExpr{&TypeRef{Name: "int"}}})},
		{"one-type meet", "$.file.decls[0].type.types", "at least two types", typeGrammar(&MeetType{Types: []TypeExpr{&TypeRef{Name: "int"}}})},
		{"type application without arguments", "$.file.decls[0].type.args", "at least one type argument", typeGrammar(&TypeApp{Name: "node"})},
		{"bare node", "$.file.decls[0].type.name", "node takes a type argument", typeGrammar(&TypeRef{Name: "node"})},
		{"concrete tree type", "$.file.decls[0].type.name", "List is not a type; write node[_]", typeGrammar(&TypeRef{Name: "List"})},
		{"applied concrete tree type", "$.file.decls[0].type.name", "Match is not a type", typeGrammar(&TypeApp{Name: "Match", Args: []TypeExpr{&TypeRef{Name: "int"}}})},
		{"pragma on a declaration", "$.file.decls[0].attrs[0].pragma", "a declaration takes @ attributes", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Attrs: []*Attribute{pragma("ignorecase")}, Expr: &Top{}}}}}},
		{"pragma on the package", "$.file.pkg.attrs[0].pragma", "a declaration takes @ attributes", &Grammar{File: File{Pkg: Pkg{Name: "p", Attrs: []*Attribute{pragma("ignorecase")}}}}},
		{"unknown pragma", "$.file.decls[0].expr.attrs[0].name", "unknown pragma #atomic", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{pragma("atomic")}})},
		{"pragma with arguments", "$.file.decls[0].expr.attrs[0].args", "#missing takes no arguments", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "missing", Pragma: true, Args: []any{1}}}})},
		{"nested Operators Rule", "$.file.decls[0].expr.items[0]", "whole body of a rule", expressionGrammar(&Seq{Items: []Expr{ops(), &Top{}}})},
		{"grouped Operators Rule", "$.file.decls[0].expr.expr", "whole body of a rule", expressionGrammar(&Group{Expr: ops()})},
		{"literal attribute argument", "$.file.decls[0].expr.attrs[0].args[0]", "is a string: use the string", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{&Literal{Value: "m"}}}}})},
		{"literal attribute keyword argument", `$.file.decls[0].expr.attrs[0].kwArgs["message"]`, "is a string: use the string", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"message": &Literal{Value: "m"}}}}})},
		{"Operators Rule in an attribute argument", "$.file.decls[0].expr.attrs[0].args[0]", "whole body of a rule", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "x", Args: []any{ops()}}}})},
		{"Operators Rule with #missing", "$.file.decls[0].expr.expr", "no pragma other than #ignorecase", expressionGrammar(&Attributed{Expr: ops(), Attrs: []*Attribute{pragma("missing")}})},
		{"Operators Rule with an @ attribute", "$.file.decls[0].expr.expr", "no pragma other than #ignorecase", expressionGrammar(&Attributed{Expr: ops(), Attrs: []*Attribute{pragma("ignorecase"), {Name: "x"}}})},
		{"Operators Rule under two attributed nodes", "$.file.decls[0].expr.expr.expr", "whole body of a rule", expressionGrammar(&Attributed{Expr: &Attributed{Expr: ops(), Attrs: []*Attribute{pragma("ignorecase")}}, Attrs: []*Attribute{pragma("ignorecase")}})},
		{"operator kind", "$.file.decls[0].expr.levels[0].operators[0].kind", "prefix, postfix or infix", operator("circumfix", "")},
		{"infix without associativity", "$.file.decls[0].expr.levels[0].operators[0].assoc", "left, right or none", operator(Infix, "")},
		{"prefix with associativity", "$.file.decls[0].expr.levels[0].operators[0].assoc", "only an infix operator", operator(Prefix, AssocLeft)},
		{"block as an operand", "$.file.decls[0].action.l", "a block is the whole action", actionGrammar(&Binary{Op: "+", L: block(), R: &IntLit{Value: 1}})},
		{"block as a lambda body", "$.file.decls[0].action.body", "a block is the whole action", lambdaGrammar([]string{"p"}, block())},
		{"block in a block", "$.file.decls[0].action.statements[0]", "a block is the whole action", actionGrammar(&Block{Statements: []ActionExpr{block()}})},
		{"block as a variable value", "$.file.decls[0].value", "a block is the whole action", &Grammar{File: File{Decls: []Decl{&VarDef{Name: "v", Value: block()}}}}},
		{"binding as an action", "$.file.decls[0].action", "a binding is a statement", actionGrammar(bind())},
		{"binding as an operand", "$.file.decls[0].action.x", "a binding is a statement", actionGrammar(&Unary{Op: "!", X: bind()})},
		{"parameter read with $", "$.file.decls[0].action.body.param", "read without $", lambdaGrammar([]string{"p"}, &CaptureRef{Name: "p"})},
		{"parameter of an outer lambda read with $", "$.file.decls[0].action.body.body.param", "read without $", lambdaGrammar([]string{"p"}, &Lambda{Params: []string{"q"}, Body: &CaptureRef{Name: "p"}})},
		{"parameter reference outside its lambda", "$.file.decls[0].action.param", "no enclosing lambda", actionGrammar(&CaptureRef{Name: "p", Param: true})},
		{"parameter reference of another lambda", "$.file.decls[0].action.body.param", "no enclosing lambda", lambdaGrammar([]string{"q"}, &CaptureRef{Name: "p", Param: true})},
		{"variable named like a parameter", "$.file.decls[0].action.body.name", "parameter of an enclosing lambda", lambdaGrammar([]string{"p"}, &VarRef{Name: "p"})},
		{"variable named true", "$.file.decls[0].action.name", "it is a literal", actionGrammar(&VarRef{Name: "true"})},
		{"call of nil", "$.file.decls[0].action.func", "it is a literal", actionGrammar(&Call{Func: "nil"})},
		{"constructor of false", "$.file.decls[0].action.type", "it is a literal", actionGrammar(&Constructor{Type: "false"})},
		{"lowercase field", "$.file.decls[0].action.name", "a capture is read with .$x", actionGrammar(&FieldAccess{X: &CaptureRef{Name: "n"}, Name: "x"})},
		{"binary operator", "$.file.decls[0].action.op", "unknown binary operator", actionGrammar(&Binary{Op: "??", L: &IntLit{}, R: &IntLit{}})},
		{"prefix operator", "$.file.decls[0].action.op", "unknown prefix operator", actionGrammar(&Unary{Op: "+", X: &IntLit{}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.g)
			assertValidationError(t, err, tc.path)
			if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v; want a message with %q", err, tc.message)
			}
		})
	}
}

// TestValidateWritable checks the forms next to the rejected ones that grammar
// syntax writes, which validation accepts.
func TestValidateWritable(t *testing.T) {
	block := &Block{Statements: []ActionExpr{&Bind{Name: "x", Value: &IntLit{Value: 1}}, &VarRef{Name: "x"}}}
	ops := func() *OperatorsRule {
		return &OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Ref{Name: "num"}, Action: block}}, Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{
			{Kind: Prefix, Expr: &Literal{Value: "-"}, Action: block},
			{Kind: Postfix, Expr: &Literal{Value: "!"}},
			{Kind: Infix, Assoc: AssocNone, Expr: &Literal{Value: "=="}},
		}}}}
	}
	ignoreCase := &Attribute{Name: "ignorecase", Pragma: true}
	for name, g := range map[string]*Grammar{
		"one-item sequence":            expressionGrammar(&Seq{Items: []Expr{&Top{}}}),
		"two-type join":                typeGrammar(&JoinType{Types: []TypeExpr{&TypeRef{Name: "int"}, &TypeRef{Name: "string"}}}),
		"node type":                    typeGrammar(&TypeApp{Name: "node", Args: []TypeExpr{&TypeRef{Name: "int"}}}),
		"pragmas":                      expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{ignoreCase, {Name: "missing", Pragma: true}, {Name: "atomic"}}}),
		"Operators Rule":               expressionGrammar(ops()),
		"string argument":              expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{"m"}}}}),
		"ignore-case literal argument": expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "x", Args: []any{&Literal{Value: "m", IgnoreCase: true}}}}}),
		"grouped literal argument":     expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "x", KwArgs: map[string]any{"k": &Group{Expr: &Literal{Value: "m"}}}}}}),
		"literal sequence argument":    expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "x", Args: []any{&Seq{Items: []Expr{&Literal{Value: "a"}, &Literal{Value: "b"}}}}}}}),
		"Operators Rule #ignorecase":   expressionGrammar(&Attributed{Expr: ops(), Attrs: []*Attribute{ignoreCase, ignoreCase}}),
		"rule action block":            actionGrammar(block),
		"inline action block":          expressionGrammar(&InlineAction{Expr: &Top{}, Action: block}),
		"predicate block":              expressionGrammar(&Predicate{Action: block}),
		"predicate binding":            expressionGrammar(&Predicate{Action: &Bind{Name: "x", Value: &IntLit{}}}),
		"parameter":                    lambdaGrammar([]string{"p"}, &Lambda{Params: []string{"q"}, Body: &Binary{Op: "+", L: &CaptureRef{Name: "p", Param: true}, R: &CaptureRef{Name: "q", Param: true}}}),
		"capture next to a parameter":  lambdaGrammar([]string{"p"}, &Binary{Op: "[]", L: &CaptureRef{Name: "q"}, R: &VarRef{Name: "v"}}),
		"call named like a parameter":  lambdaGrammar([]string{"len"}, &Call{Func: "len", Args: []ActionExpr{&CaptureRef{Name: "len", Param: true}}}),
		"members":                      actionGrammar(&FieldAccess{X: &FieldAccess{X: &FieldAccess{X: &CaptureRef{Name: "n"}, Name: "$x"}, Name: "startPos"}, Name: "Value"}),
		"rule named true":              expressionGrammar(&Ref{Name: "true"}),
	} {
		if err := Validate(g); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestValidateSharedSubtreeInParameterScopes checks that a subtree shared by
// lambdas with different parameters is checked in each scope, and that a
// pointer cycle through a lambda is still found.
func TestValidateSharedSubtreeInParameterScopes(t *testing.T) {
	shared := &ActionGroup{Action: &CaptureRef{Name: "p", Param: true}}
	g := actionGrammar(&TupleLit{Elems: []ActionExpr{
		&Lambda{Params: []string{"p"}, Body: shared},
		&Lambda{Params: []string{"q"}, Body: shared},
	}})
	assertValidationError(t, Validate(g), "$.file.decls[0].action.elems[1].body.action.param")
	cycle := &Lambda{Params: []string{"p"}}
	cycle.Body = &ActionGroup{Action: cycle}
	assertValidationError(t, Validate(actionGrammar(cycle)), "$.file.decls[0].action.body.action")
}
