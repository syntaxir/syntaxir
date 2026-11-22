package complexity

import (
	"os"
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// TestRunSitesInClassification checks that the classifier's marking of run sites is off by default,
// and that with it on a repetition whose scan a continuation restarts is not a scanning source,
// whether its element builds a value, captures, reads variables or recovers.
func TestRunSitesInClassification(t *testing.T) {
	defer func() { runSitesOn = false }()
	for _, c := range []struct {
		name, src string
		off, on   ComplexityClass
	}{
		{"memoized scan restarted by a repetition", "rule main = (a \"y\" / \"q\" a / .)* $$\nrule a = x+\nrule x = \"x\"\n", ClassPoly, ClassLinear},
		{"scan restarted through a memoized recursion", "rule main = t a? $$\nrule t = a t \"!\" / . t / \"\"\nrule a = x+\nrule x = \"x\"\n", ClassUnknown, ClassLinear},
		{"element with a capture", "rule main = (a \"y\" / \"q\" a / .)* $$\nrule a = (c:x)+ => 1\nrule x = \"x\"\n", ClassPoly, ClassLinear},
		{"element with #recover", "rule main = (a \"y\" / \"q\" a / .)* $$\nrule a = (x #recover(skip=[^y]*))+\nrule x = \"x\"\n", ClassPoly, ClassLinear},
		{"element with #error", "rule main = (a \"y\" / \"q\" a / .)* $$\nrule a = (x @error(message=\"m\"))+\nrule x = \"x\"\n", ClassPoly, ClassPoly},
	} {
		classOf := func() ComplexityClass { return classifySource(t, c.src).Class }
		runSitesOn = false
		if got := classOf(); got != c.off {
			t.Errorf("%s: off: class %v, want %v", c.name, got, c.off)
		}
		runSitesOn = true
		if got := classOf(); got != c.on {
			t.Errorf("%s: on: class %v, want %v", c.name, got, c.on)
		}
	}
}

// TestRunSitesMatchClassifier checks that the repetitions the engine marks as run sites (and the
// scans and skip-run sites it leaves aside) are those that the classifier counts as run sites,
// in the bundled grammars: the engine keeps records for every repetition the classifier relies on.
func TestRunSitesMatchClassifier(t *testing.T) {
	defer func() { runSitesOn = false }()
	for _, name := range []string{
		"testdata/grammars/cel/cel.sxir", "testdata/grammars/cue/cue.sxir", "testdata/grammars/duckdb/duckdb.sxir",
		"testdata/grammars/golang/golang.sxir", "testdata/grammars/python/python.sxir", "testdata/grammars/typescript/typescript.sxir",
		"testdata/grammars/xml/xml.sxir", "testdata/grammars/yaml/yaml.sxir", "examples/minilang/minilang.sxir", "examples/outline/outline.sxir",
	} {
		src, err := os.ReadFile("../../../" + name)
		if err != nil {
			t.Fatal(err)
		}
		g, err := parser.Parse(string(src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		runSitesOn = true
		k, _, err := prepareClassifier(g, "", nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// The run sites are those of the grammar, which the classifier of a view asks the
		// classifier of the grammar about (runSiteOf).
		if k.base != nil {
			k = newClassifier(k.g, k.base, k.facts, k.start, k.control)
		}
		classifier := 0
		for _, r := range k.rules {
			walkNodes(k.ruleBody(r.Name), func(x core.Node) {
				if rep, ok := x.(*core.Repeat); ok && !corepass.IsScan(rep) && k.runSiteOf(rep) {
					classifier++
				}
			})
		}
		runSitesOn = false
		prog, err := frontend.Check(g, check.Options{}, nil, desugar.InlineNames{}, checkfacts.Source{}, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %d run sites, %d skip-run sites", name, prog.Runtime().NumRunSites, prog.Runtime().NumSkipSites)
		if got := prog.Runtime().NumRunSites; got != classifier {
			t.Errorf("%s: the engine marks %d run sites, the classifier %d", name, got, classifier)
		}
	}
}
