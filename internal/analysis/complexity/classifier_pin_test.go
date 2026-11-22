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
	"syntaxir.dev/sxir/internal/ir/core"
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
	k, out := classifiedForPin(name, src)
	if k == nil {
		return out
	}
	answers, _ := writtenHelperAnswers(t, k, name)
	return answers
}

// classifiedForPin returns the classifier of the grammar src, named name, after it classified the
// grammar from its default start rule, every rule being a start rule, or nil and the line saying
// that the grammar is not classified.
func classifiedForPin(name, src string) (*classifier, string) {
	g, err := parser.Parse(src)
	if err != nil {
		return nil, fmt.Sprintf("# %s: does not parse\n", name)
	}
	k, _, err := prepareClassifier(starttest.MarkAll(g), "", nil)
	if err != nil {
		return nil, fmt.Sprintf("# %s: not classified: %v\n", name, err)
	}
	k.classify()
	return k, ""
}

// writtenAnswer is the answers of the helpers for the node that an expression of the written view
// stands for.
type writtenAnswer struct {
	label   string
	node    core.Node
	answers string
}

// writtenHelperAnswers returns the answers of the helpers of k, named by the expressions of the
// written view, and the answers of each expression with the node it stands for.
func writtenHelperAnswers(t *testing.T, k *classifier, name string) (string, []writtenAnswer) {
	t.Helper()
	var rows []writtenAnswer
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
	w := writtenOf(t, k)
	for _, r := range w.rules {
		i := 0
		astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
			pos, n := exprPos(e), w.nodes[e]
			var left []string
			for _, id := range k.facts.source.Facts.LeftCalls(n, false) {
				left = append(left, k.rules[id].Name)
			}
			label := fmt.Sprintf("%s #%d %T %d:%d", r.Name, i, e, pos.Line, pos.Col)
			answers := fmt.Sprintf("null=%s an=%s succ=%s bounded=%s first=%s left=%s",
				bitsOf(k.nullableOf(n)), bitsOf(k.facts.source.Facts.Nullable(n)),
				bitsOf(k.succeedsOf(n)), bitsOf(k.boundedOf(n)), charsText(k.firstOf(n)),
				strings.Join(left, ","))
			fmt.Fprintf(&b, "%s: %s\n", label, answers)
			rows = append(rows, writtenAnswer{label: label, node: n, answers: answers})
			i++
		})
	}
	return b.String(), rows
}

// TestClassifierNodeHelperAnswers pins what the classifier's helpers answer for every node of the
// rule bodies of the cost view of every corpus grammar (internal/testing/factstest) from its
// default start rule, every rule being a start rule as in TestClassGolden: whether the node is
// nullable as the classifier and as the rule analysis see it, whether it certainly succeeds, its
// first characters, whether it is bounded and its left calls. The classification is built on these
// answers, so a change in how the classifier computes them must leave them as they are.
//
// The answers of a large grammar are pinned by their SHA-256 digest; -classifier-helper-dump
// writes them in full to a directory, so that a change can be compared with the dump of the base.
//
// A node is named by its rule, its rank in a preorder walk of the rule's body (core.Walk), its
// kind and the position of the expression the classifier spells it by in messages.
func TestClassifierNodeHelperAnswers(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, path := range factstest.Corpus {
		answers := classifierNodeAnswers(path, factstest.Read(t, path))
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
	factstest.GoldenFile(t, "classifier-node-helpers.txt", b.String())
}

// classifierNodeAnswers returns the answers of the helpers of the classifier of the grammar src,
// named name, after it classified the grammar, or a line saying that the grammar is not
// classified.
func classifierNodeAnswers(name, src string) string {
	k, out := classifiedForPin(name, src)
	if k == nil {
		return out
	}
	answers, _ := nodeHelperAnswers(k, name)
	return answers
}

// nodeHelperAnswers returns the answers of the helpers of k for every node of the bodies of its
// rules, and the answers of each node.
func nodeHelperAnswers(k *classifier, name string) (string, map[core.Node]string) {
	byNode := map[core.Node]string{}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s from %s\n", name, k.start)
	rulesWhere := func(holds map[string]bool) string {
		var names []string
		for _, r := range k.rules {
			if holds[r.Name] {
				names = append(names, r.Name)
			}
		}
		return strings.Join(names, " ")
	}
	fmt.Fprintf(&b, "nullable: %s\n", rulesWhere(k.an.Nullable))
	fmt.Fprintf(&b, "succeeding: %s\n", rulesWhere(k.an.Succeeding))
	for _, r := range k.rules {
		i := 0
		core.Walk(r.Body, func(n core.Node) {
			var left []string
			for _, id := range k.facts.source.Facts.LeftCalls(n, false) {
				left = append(left, k.rules[id].Name)
			}
			answers := fmt.Sprintf("null=%s an=%s succ=%s bounded=%s first=%s left=%s",
				bitsOf(k.nullableOf(n)), bitsOf(k.facts.source.Facts.Nullable(n)),
				bitsOf(k.succeedsOf(n)), bitsOf(k.boundedOf(n)), charsText(k.firstOf(n)),
				strings.Join(left, ","))
			pos := k.posOf(n)
			fmt.Fprintf(&b, "%s #%d %T %d:%d: %s\n", r.Name, i, n, pos.Line, pos.Col, answers)
			byNode[n] = answers
			i++
		})
	}
	return b.String(), byNode
}

// TestClassifierHelperAnswersAgree requires the answers that TestClassifierNodeHelperAnswers pins
// for the nodes of the cost view to be the answers that TestClassifierHelperAnswers pins for the
// expressions of the written view: for every corpus grammar, the classifier answers each expression
// of the written view as it answers the node that the expression stands for, and the walk of the
// nodes reaches that node.
func TestClassifierHelperAnswersAgree(t *testing.T) {
	t.Parallel()
	compared := 0
	for _, path := range factstest.Corpus {
		k, _ := classifiedForPin(path, factstest.Read(t, path))
		if k == nil {
			continue
		}
		_, written := writtenHelperAnswers(t, k, path)
		_, byNode := nodeHelperAnswers(k, path)
		for _, w := range written {
			got, ok := byNode[w.node]
			switch {
			case !ok:
				t.Errorf("%s: %s: the walk of the nodes does not reach its %T", path, w.label, w.node)
			case got != w.answers:
				t.Errorf("%s: %s: the expression answers %q, its %T %q", path, w.label, w.answers, w.node, got)
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no expression compared")
	}
	t.Logf("compared %d expressions", compared)
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
