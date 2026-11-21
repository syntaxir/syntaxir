package lint

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestAnalysisAnswers pins what the analysis answers for every rule and every expression of the
// rules of the corpus grammars, the inline grammars of the end-of-input differential test and the
// grammars in testdata and examples: the rule sets and, for each expression, its nullability at and
// away from the end of the input, the engine's nullability, whether it may match, whether it
// certainly succeeds, its loose cuts, its prefix with whether the prefix folds case and its left
// calls. The checks are built on these answers, so a change in how the analysis computes them must
// leave them as they are.
//
// The answers of a large grammar are pinned by their SHA-256 digest; -lint-analysis-dump writes
// them in full to a directory, so that a change can be compared with the dump of the base.
//
// An expression is named by its rule, its position, its kind and its rank among the expressions of
// the rule with the same position and kind, so that the answers do not depend on the grouping
// parentheses of the grammar or on how its pragmas are spelled.
func TestAnalysisAnswers(t *testing.T) {
	t.Parallel()
	var paths []string
	paths = append(paths, factstest.Corpus...)
	root := factstest.Root(t)
	for _, pattern := range []string{"testdata/grammars/*/*.sxir", "examples/*/*.sxir"} {
		files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			rel, err := filepath.Rel(root, f)
			if err != nil {
				t.Fatal(err)
			}
			if rel = filepath.ToSlash(rel); !slices.Contains(paths, rel) {
				paths = append(paths, rel)
			}
		}
	}
	var b strings.Builder
	pin := func(name, answers string) {
		if *analysisDump != "" {
			file := filepath.Join(*analysisDump, strings.NewReplacer("/", "__", " ", "_", "\"", "").Replace(name)+".txt")
			if err := os.WriteFile(file, []byte(answers), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(answers) > pinnedInFull {
			answers = fmt.Sprintf("# %s sha256 %x\n", name, sha256.Sum256([]byte(answers)))
		}
		b.WriteString(answers)
	}
	for _, p := range paths {
		pin(p, analysisAnswers(t, p, factstest.Read(t, p)))
	}
	for i, src := range endFactsGrammars {
		pin(fmt.Sprintf("inline %d", i), analysisAnswers(t, fmt.Sprintf("inline %d %q", i, src), src))
	}
	factstest.GoldenFile(t, "lint-analysis.txt", b.String())
}

// pinnedInFull is the size of the largest answers that the golden file holds in full.
const pinnedInFull = 64 << 10

var analysisDump = flag.String("lint-analysis-dump", "", "write the answers of TestAnalysisAnswers in full to this directory")

// analysisAnswers returns the answers of the analysis of the grammar src, named name, as lint
// computes them, or a line saying that the grammar is not linted.
func analysisAnswers(t *testing.T, name, src string) string {
	t.Helper()
	parsed, err := parser.Parse(src)
	if err != nil {
		return fmt.Sprintf("# %s: does not parse\n", name)
	}
	g, _, err := desugar.LowerInlineActions(parsed)
	if err != nil {
		return fmt.Sprintf("# %s: inline actions do not lower\n", name)
	}
	res := checkedOrNil(g)
	if res == nil {
		return fmt.Sprintf("# %s: does not check\n", name)
	}
	a := newAnalysisControlled(g, nil, tablesOf(res))
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", name)
	set := func(m map[string]bool) string {
		var names []string
		for _, r := range a.order {
			if m[r.Name] {
				names = append(names, r.Name)
			}
		}
		return strings.Join(names, " ")
	}
	fmt.Fprintf(&b, "nullable: %s\n", set(a.nullable[0]))
	fmt.Fprintf(&b, "nullable at end: %s\n", set(a.nullable[1]))
	fmt.Fprintf(&b, "can match: %s\n", set(a.canMatch))
	fmt.Fprintf(&b, "base case: %s\n", set(a.baseCase))
	for _, r := range a.order {
		rank := map[string]int{}
		walkExpr(r.Expr, func(e ast.Expr) {
			if _, ok := e.(*ast.Group); ok {
				return // the walk visits the grouped expression next
			}
			x := analysisExpr(e, nil)
			pos := posOf(x)
			key := fmt.Sprintf("%s %d:%d %T", r.Name, pos.Line, pos.Col, x)
			rank[key]++
			p := a.prefix(x)
			fmt.Fprintf(&b, "%s #%d: null=%s eng=%s match=%s succ=%s always=%s cut=%s prefix=%q exact=%s fold=%s left=%s\n",
				key, rank[key], bits(a.nullableAt(x, false), a.nullableAt(x, true)), bits(a.isEngineNullable(x)),
				bits(a.mayMatch(x)), bits(a.succeeds(x)), bits(a.alwaysSucceeds(x)), bits(a.hasLooseCut(x)),
				p.s, bits(p.exact), bits(p.fold), strings.Join(a.leftCalls(x, nil), ","))
		})
	}
	return b.String()
}

// bits writes each of vs as 1 or 0.
func bits(vs ...bool) string {
	var b strings.Builder
	for _, v := range vs {
		if v {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}
