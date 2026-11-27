package codegentest

import (
	"fmt"
	"strconv"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Replace a source-valid placeholder with a public-AST value. Only matching
// expressions are visited: string constants in actions have separate semantics.
func FirstLiteral(e ast.Expr) *ast.Literal {
	switch e := e.(type) {
	case *ast.Group:
		return FirstLiteral(e.Expr)
	case *ast.Literal:
		return e
	case *ast.Seq:
		for _, item := range e.Items {
			if lit := FirstLiteral(item); lit != nil {
				return lit
			}
		}
	case *ast.Choice:
		for _, item := range e.Alts {
			if lit := FirstLiteral(item); lit != nil {
				return lit
			}
		}
	case *ast.Capture:
		return FirstLiteral(e.Expr)
	case *ast.Atomic:
		return FirstLiteral(e.Expr)
	case *ast.Repeat:
		return FirstLiteral(e.Expr)
	}
	return nil
}

// ReplaceFirstLiteral sets the value of the first literal of e and returns it, or nil if there is none.
func ReplaceFirstLiteral(e ast.Expr, val string) *ast.Literal {
	lit := FirstLiteral(e)
	if lit != nil {
		lit.Value = val
	}
	return lit
}

// RawLiteralValues are literal values that are not valid UTF-8. Validation rejects a grammar AST
// that holds one; the literal of their replacement characters matches each of their bytes.
func RawLiteralValues() []string {
	return []string{"\xc3", "\x80", "\xff", "\xc0\xaf", "\xed\xa0\x80", "é\xc3x", "abcdefghijk\xffxyz"}
}

// ReplacementLiteralCorpus returns the cases of the generators with the literals of the replacement
// characters of RawLiteralValues, in several shapes, each with the raw values among its inputs.
func ReplacementLiteralCorpus() []Case {
	var canonicals []string
	raws := map[string][]string{}
	for _, raw := range RawLiteralValues() {
		canonical := string([]rune(raw))
		if raws[canonical] == nil {
			canonicals = append(canonicals, canonical)
		}
		raws[canonical] = append(raws[canonical], raw)
	}
	var cases []Case
	for _, canonical := range canonicals {
		for _, shape := range []string{"exact", "suffix", "choice", "predicate"} {
			expr := strconv.Quote(canonical)
			predicate := ""
			switch shape {
			case "suffix":
				expr = "(" + expr + " .) #atomic"
			case "predicate":
				predicate = " {text($t)==" + strconv.Quote(canonical) + "}"
			case "choice":
				expr = "(" + expr + ` / "other")`
			}
			var inputs []string
			for _, raw := range raws[canonical] {
				inputs = append(inputs, raw, raw+"x")
			}
			inputs = append(inputs, canonical, canonical+"x", "é", "éx", "other", "", "\xffx", "\x80x")
			cases = append(cases, Case{Name: fmt.Sprintf("replacement literal/%x/%s", canonical, shape),
				Src:    "type N = { T node[string] }\nrule main N = t:" + expr + predicate + " $$ => N{T:$t}",
				Inputs: inputs})
		}
	}
	return cases
}

// InvalidLiteralCorpus returns the cases of literals that match bytes which are not valid UTF-8.
func InvalidLiteralCorpus() []Case {
	cases := []Case{
		{Name: "text predicate", Src: `type N = { T node[string] }
rule main N = t:"�" {len(text($t)) == len($t)} $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "é"}},
		{Name: "replacement", Src: `type N = { T node[string] }
rule main N = t:"�" $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "é", "", "\xffx"}},
		{Name: "multiple", Src: `type N = { T node[string] }
rule main N = t:"���" $$ => N{T: $t}`, Inputs: []string{"\xed\xa0\x80", "\xe0\x80\x80", "���", "\xff", "\xff\x80\x80x"}},
		{Name: "mixed", Src: `type N = { T node[string] }
rule main N = t:"é�x" $$ => N{T: $t}`, Inputs: []string{"é\xffx", "é\xc3x", "é�x", "é\xffy", "é\xff", "éx"}},
		{Name: "long", Src: `type N = { T node[string] }
rule main N = t:"abcdefghijk�xyz" $$ => N{T: $t}`, Inputs: []string{"abcdefghijk\xffxyz", "abcdefghijk�xyz", "abcdefghijk\xc3xyz", "abcdefghijk\xffxyq"}},
		{Name: "choice", Src: `type N = { T node[string] }
rule main N = t:("x" / "�") $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "x", "y"}},
		{Name: "class", Src: `type N = { T node[string] }
rule main N = t:[�] $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "é"}},
	}
	return append(cases, ReplacementLiteralCorpus()...)
}
