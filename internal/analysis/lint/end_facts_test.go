package lint

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// endFactsGrammars are the grammars of the differential test besides the repository's corpus:
// the constructs whose behavior at the end of the input differs (end of input, lookaheads,
// predicates, recovery, missing tokens, operators and calls).
var endFactsGrammars = []string{
	"rule main = $$",
	"rule main = !$$ \"x\"",
	"rule main = !$$ \"x\" / \"y\"",
	"rule main = &$$",
	"rule main = &$$ \"x\"",
	"rule main = !(\"x\" / $$)",
	"rule main = (\"x\" / $$) !\"y\"",
	"rule main = !\"a\"",
	"rule main = &\"a\" !\"b\"",
	"rule main = \"a\"? $$",
	"rule main = \"a\"* $$ / \"b\"",
	"rule main = \"a\" $$ / \"b\"+",
	"rule main = a\nrule a = $$ / \"x\"",
	"rule main = !a \"x\"\nrule a = $$",
	"rule main = !a \"x\"\nrule a = \"b\" / $$",
	"rule main = ((\"x\") \"y\" @recover(\"z\") / $$)",
	"rule main = \"x\" @missing(\"y\") $$",
	"rule main = !(!$$ \"x\") \"y\"",
	"rule main = !(\"x\" $$)",
	"rule main = a ! b\nrule a = \"x\" a / $$\nrule b = \"y\"",
	"rule main = \"x\" / main \"y\"",
	"rule main = _|_ / \"x\"",
	"rule main = !_|_ \"x\"",
	"rule main = { true } \"x\" / $$",
	"rule main = !.",
	"rule main = !(.) $$",
	"rule main = x:\"a\" !x:$$",
}

// TestEndFactsMatchTheAnalysis checks the Core facts NullableAtEnd, NullableBeforeEnd and CanMatch,
// of each rule and of its body, against the lint analysis they replace: the nullable and canMatch
// sets of the analysis and its nullableAt and mayMatch on the rule's expression.
func TestEndFactsMatchTheAnalysis(t *testing.T) {
	t.Parallel()
	var srcs []string
	for _, p := range factstest.Corpus {
		srcs = append(srcs, factstest.Read(t, p))
	}
	srcs = append(srcs, endFactsGrammars...)
	compared := 0
	defer func() {
		if compared < len(endFactsGrammars) {
			t.Errorf("compared %d grammars, want at least the %d inline ones", compared, len(endFactsGrammars))
		}
	}()
	for _, src := range srcs {
		parsed, err := parser.Parse(src)
		if err != nil {
			continue
		}
		res := checkedOrNil(parsed)
		if res == nil {
			continue
		}
		compared++
		// The analysis reads the grammar that the front end checked, as lint does.
		g := res.Grammar()
		an := newAnalysis(g)
		for _, r := range res.Module.Rules {
			if r == nil || r.Twin != nil {
				continue
			}
			rd := ruleDef(g, r.Name)
			if rd == nil {
				continue
			}
			f := res.Facts.Rules[r.ID]
			if want := an.nullable[1][r.Name]; f.NullableAtEnd != want {
				t.Errorf("%q rule %s: NullableAtEnd=%v, analysis nullable[end]=%v", src, r.Name, f.NullableAtEnd, want)
			}
			if want := an.nullable[0][r.Name]; f.NullableBeforeEnd != want {
				t.Errorf("%q rule %s: NullableBeforeEnd=%v, analysis nullable[before end]=%v", src, r.Name, f.NullableBeforeEnd, want)
			}
			if want := an.canMatch[r.Name]; f.CanMatch != want {
				t.Errorf("%q rule %s: CanMatch=%v, analysis canMatch=%v", src, r.Name, f.CanMatch, want)
			}
			if got, want := res.Facts.NullableAtEnd(r.Body), an.nullableAt(rd.Expr, true); got != want {
				t.Errorf("%q rule %s: body NullableAtEnd=%v, analysis nullableAt=%v", src, r.Name, got, want)
			}
			if got, want := res.Facts.NullableBeforeEnd(r.Body), an.nullableAt(rd.Expr, false); got != want {
				t.Errorf("%q rule %s: body NullableBeforeEnd=%v, analysis nullableAt=%v", src, r.Name, got, want)
			}
			if got, want := res.Facts.MayMatch(r.Body), an.mayMatch(rd.Expr); got != want {
				t.Errorf("%q rule %s: body MayMatch=%v, analysis mayMatch=%v", src, r.Name, got, want)
			}
		}
	}
}

// checkedOrNil returns the checked front end result of g, or nil when g does not check.
func checkedOrNil(g *ast.Grammar) (res *frontend.Result) {
	defer func() {
		if recover() != nil {
			res = nil
		}
	}()
	return checked(g)
}

// ruleDef returns the rule of g named name, or nil.
func ruleDef(g *ast.Grammar, name string) *ast.RuleDef {
	for _, rd := range g.Rules() {
		if rd.Name == name {
			return rd
		}
	}
	return nil
}
