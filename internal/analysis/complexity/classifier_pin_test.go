package complexity

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestClassifierHelperAnswers pins what the classifier's helpers answer for every expression of
// the cost view of every corpus grammar (internal/testing/factstest) from its default start rule,
// every rule being a start rule as in TestClassGolden: whether the expression is nullable as the
// classifier and as the rule analysis see it, whether it certainly succeeds, its first characters,
// whether it is bounded and its left calls. The classification is built on these answers, so a
// change in how the classifier computes them must leave them as they are.
//
// The answers of a large grammar are pinned by their SHA-256 digest; -classifier-helper-dump
// writes them in full to a directory, so that a change can be compared with the dump of the base.
//
// An expression is named by its rule, its rank in a preorder walk of the rule's body, its kind
// and its position.
func TestClassifierHelperAnswers(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, path := range factstest.Corpus {
		answers := classifierHelperAnswers(t, path, factstest.Read(t, path))
		if *helperDump != "" {
			file := filepath.Join(*helperDump, strings.ReplaceAll(path, "/", "__")+".txt")
			if err := os.WriteFile(file, []byte(answers), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(answers) > helperPinnedInFull {
			answers = fmt.Sprintf("# %s sha256 %x\n", path, sha256.Sum256([]byte(answers)))
		}
		b.WriteString(answers)
	}
	factstest.GoldenFile(t, "classifier-helpers.txt", b.String())
}

// helperPinnedInFull is the size of the largest answers that the golden file holds in full.
const helperPinnedInFull = 64 << 10

var helperDump = flag.String("classifier-helper-dump", "", "write the answers of TestClassifierHelperAnswers in full to this directory")

// classifierHelperAnswers returns the answers of the helpers of the classifier of the grammar src,
// named name, after it classified the grammar, or a line saying that the grammar is not
// classified.
func classifierHelperAnswers(t *testing.T, name, src string) string {
	t.Helper()
	g, err := parser.Parse(src)
	if err != nil {
		return fmt.Sprintf("# %s: does not parse\n", name)
	}
	k, _, err := prepareClassifier(starttest.MarkAll(g), "", nil)
	if err != nil {
		return fmt.Sprintf("# %s: not classified: %v\n", name, err)
	}
	k.classify()
	var b strings.Builder
	fmt.Fprintf(&b, "# %s from %s\n", name, k.start)
	set := func(holds map[string]bool) string {
		var names []string
		for _, r := range k.rules {
			if holds[r.Name] {
				names = append(names, r.Name)
			}
		}
		return strings.Join(names, " ")
	}
	fmt.Fprintf(&b, "nullable: %s\n", set(k.an.Nullable))
	fmt.Fprintf(&b, "succeeding: %s\n", set(k.an.Succeeding))
	for _, r := range k.rules {
		i := 0
		astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
			pos := exprPos(e)
			fmt.Fprintf(&b, "%s #%d %T %d:%d: null=%s an=%s succ=%s bounded=%s first=%s left=%s\n",
				r.Name, i, e, pos.Line, pos.Col, bitsOf(k.nullable(e)), bitsOf(k.an.isNullable(e)),
				bitsOf(k.an.AlwaysSucceeds(e)), bitsOf(k.bounded(e)), charsText(k.first(e)),
				strings.Join(k.an.LeftCalls(e, nil), ","))
			i++
		})
	}
	return b.String()
}

// bitsOf writes v as 1 or 0.
func bitsOf(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// charsText writes the ranges of s as hexadecimal code points.
func charsText(s charset) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		if r.Lo == r.Hi {
			fmt.Fprintf(&b, "%x", r.Lo)
		} else {
			fmt.Fprintf(&b, "%x-%x", r.Lo, r.Hi)
		}
	}
	return b.String()
}
