package lint

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// endFactsGrammars are the grammars of the end-of-input tests besides the repository's corpus:
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

// TestEndFactsHoldForRuleBodies checks the Core facts NullableAtEnd, NullableBeforeEnd and CanMatch
// of each rule against the node queries on its body, which lint reads for every expression: at the
// least fixed point, a rule has a fact exactly when its body does. The answers that lint computed
// for these grammars on its own are pinned by TestAnalysisAnswers.
func TestEndFactsHoldForRuleBodies(t *testing.T) {
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
		facts := corepass.FactsOf(res.Analyses, res.Module)
		for _, r := range res.Module.Rules {
			if r == nil || r.Twin != nil {
				continue
			}
			f := facts.Rules[r.ID]
			if got := facts.NullableAtEnd(r.Body); f.NullableAtEnd != got {
				t.Errorf("%q rule %s: NullableAtEnd=%v, body NullableAtEnd=%v", src, r.Name, f.NullableAtEnd, got)
			}
			if got := facts.NullableBeforeEnd(r.Body); f.NullableBeforeEnd != got {
				t.Errorf("%q rule %s: NullableBeforeEnd=%v, body NullableBeforeEnd=%v", src, r.Name, f.NullableBeforeEnd, got)
			}
			if got := facts.MayMatch(r.Body); f.CanMatch != got {
				t.Errorf("%q rule %s: CanMatch=%v, body MayMatch=%v", src, r.Name, f.CanMatch, got)
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
