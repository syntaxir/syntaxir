package print_test

import (
	"bytes"
	"io"
	"regexp"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestBisectionModeParserBehaves checks that a TypeScript parser generated under a bisection limit
// (common.Debug.OptBisectLimit) gives the results of the default parser. Under a limit the parser
// keeps the rules of the plan's reach that its code never enters, with the features of the library,
// helpers and literal tables they use; here the unentered rule is left-recursive, so it needs
// features that the default parser leaves out. The parsers differ, and the results agree.
func TestBisectionModeParserBehaves(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node := tooltest.RequireNode(t)
	cases := []codegentest.Case{{
		Name:   "unentered left recursion",
		Src:    "@start\nrule main = !\"\"* L / \"x\"\nrule L = L \"a\" / \"b\"\n",
		Inputs: []string{"", "x", "xa", "b", "ba", "baa", "a", "xx"},
	}}
	beyond := 1 << 30
	limited := common.GenOptions{Debug: common.Debug{OptBisectLimit: &beyond, Output: io.Discard}}
	def := codegentest.TSBundles(t, cases, common.GenOptions{})
	bisect := codegentest.TSBundles(t, cases, limited)
	if bytes.Equal(def[0], bisect[0]) {
		t.Fatal("the parser under a limit equals the default one, so the test compares nothing")
	}
	// The parser under a limit keeps the rule L, which the default parser leaves out: it declares
	// more rule functions (R<id>).
	rules := regexp.MustCompile(`(?m)^(?:export )?(?:function\*?|const|let|var|class) R\d+\b`)
	if b, d := len(rules.FindAll(bisect[0], -1)), len(rules.FindAll(def[0], -1)); b <= d {
		t.Fatalf("the parser under a limit declares %d rule functions, the default one %d: want more", b, d)
	}
	got := codegentest.TSOutputs(t, node, cases, bisect, nil)
	want := codegentest.TSOutputs(t, node, cases, def, nil)
	if got != want {
		t.Errorf("the parser under a limit gives other results:\n%s\nwant\n%s", got, want)
	}
}
