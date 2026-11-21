package complexitytest

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/analysis/complexity"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestClassificationSnapshot pins every member of the classification, with the messages and
// positions of its causes, and the growing bindings, of every corpus grammar
// (internal/testing/factstest) from its default start rule, both as the grammar is written and with
// every rule a start rule, and of every control grammar (Controls) from main. TestClassGolden pins
// only the class and degree; this pin holds a change of the classifier's representation to the
// same findings, byte for byte.
//
// A large classification is pinned by its SHA-256 digest; -class-snapshot-dump writes every
// classification in full to a directory, so that a change can be compared with the dump of the base.
func TestClassificationSnapshot(t *testing.T) {
	t.Parallel()
	type entry struct {
		name  string
		g     func() (*ast.Grammar, error)
		start string
	}
	var entries []entry
	for _, path := range factstest.Corpus {
		parse := func() (*ast.Grammar, error) { return enginetest.ParseGrammar(factstest.Read(t, path)) }
		entries = append(entries,
			entry{path + " as written", parse, ""},
			entry{path + " every rule a start", func() (*ast.Grammar, error) {
				g, err := parse()
				if err != nil {
					return nil, err
				}
				return starttest.MarkAll(g), nil
			}, ""})
	}
	for _, c := range Controls() {
		entries = append(entries, entry{"control " + c.Name, func() (*ast.Grammar, error) {
			g, err := enginetest.ParseGrammar(c.Source)
			if err != nil {
				return nil, err
			}
			return starttest.MarkAll(g), nil
		}, "main"})
	}
	out := make([]string, len(entries))
	t.Run("grammars", func(t *testing.T) {
		for i, e := range entries {
			t.Run(e.name, func(t *testing.T) {
				t.Parallel()
				g, err := e.g()
				if err != nil {
					t.Fatal(err)
				}
				out[i] = classificationText(e.name, g, e.start)
			})
		}
	})
	if t.Failed() {
		return
	}
	var b strings.Builder
	for i, text := range out {
		if *snapshotDump != "" {
			file := filepath.Join(*snapshotDump, strings.NewReplacer("/", "__", " ", "_").Replace(entries[i].name)+".txt")
			if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(text) > snapshotInFull {
			text = fmt.Sprintf("# %s sha256 %x\n", entries[i].name, sha256.Sum256([]byte(text)))
		}
		b.WriteString(text)
	}
	factstest.GoldenFile(t, "class-snapshot.txt", b.String())
}

// snapshotInFull is the size of the largest classification that the golden file holds in full.
const snapshotInFull = 16 << 10

var snapshotDump = flag.String("class-snapshot-dump", "", "write the classifications of TestClassificationSnapshot in full to this directory")

// classificationText writes the classification and the growing bindings of g from start, or the
// errors of the classifier.
func classificationText(name string, g *ast.Grammar, start string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", name)
	c, err := complexity.ClassifyComplexity(context.Background(), g, start)
	if err != nil {
		fmt.Fprintf(&b, "error: %v\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "class %s degree=%d witnessed=%d combinations=%d\n", c.Class, c.Degree, c.WitnessedDegree, c.Combinations)
	fmt.Fprintf(&b, "memoized: %s\n", strings.Join(slices.Sorted(maps.Keys(trueKeys(c.Memoized))), " "))
	for _, list := range []struct {
		name   string
		causes []complexity.ComplexityCause
	}{{"cause", c.Causes}, {"reason", c.Reasons}, {"env", c.Env}} {
		for _, cs := range list.causes {
			fmt.Fprintf(&b, "%s %d:%d in %s: %s %s degree=%d: %s\n", list.name, cs.Pos.Line, cs.Pos.Col, cs.Rule,
				cs.Kind, cs.Class, cs.Degree, cs.Message)
		}
	}
	growingText := func(label string, gs []complexity.GrowingBinding) {
		for _, gb := range gs {
			fmt.Fprintf(&b, "%s %d:%d in %s: %s = %s\n", label, gb.Pos.Line, gb.Pos.Col, gb.Rule, gb.Var, gb.Text)
		}
	}
	growingText("growing", c.Growing)
	alone, err := complexity.GrowingBindings(context.Background(), g, start)
	if err != nil {
		fmt.Fprintf(&b, "growing bindings alone: error: %v\n", err)
	} else if !slices.Equal(alone, c.Growing) {
		growingText("growing alone", alone)
	}
	return b.String()
}

// trueKeys returns the set of the keys of m whose value is true.
func trueKeys(m map[string]bool) map[string]bool {
	s := map[string]bool{}
	for k, v := range m {
		if v {
			s[k] = true
		}
	}
	return s
}
