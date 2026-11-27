package print

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestLibNodesEqualComparesValues holds the TypeScript library's comparison of step results to the
// engine's: two trees are equal by their values, whatever their fresh flags say about whether the
// rule that made a node may still change it, and unequal where a value differs. As a negative
// control, the library whose comparison also takes fresh into account fails it.
func TestLibNodesEqualComparesValues(t *testing.T) {
	node := tooltest.RequireNode(t)
	// go test runs in the package directory, from which libDir leads to the library.
	source, err := os.ReadFile(filepath.Join(libDir, tsLibFileName))
	if err != nil {
		t.Fatal(err)
	}
	// The library calls the helpers of the budget module, which the generator places before it.
	helpers, err := os.ReadFile(filepath.Join(libDir, "budget.ts"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(lib string) (string, error) {
		path := filepath.Join(t.TempDir(), "nodes_equal.ts")
		if err := os.WriteFile(path, []byte(string(helpers)+lib+nodesEqualTypeScriptTest), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := tooltest.NodeCombinedOutput(t, node, filepath.Dir(path), filepath.Base(path))
		return string(out), err
	}
	if out, err := run(string(source)); err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	const cmp = "a.text !== b.text || "
	if !strings.Contains(string(source), cmp) {
		t.Fatalf("the library has no %q", cmp)
	}
	if out, err := run(strings.Replace(string(source), cmp, cmp+"a.fresh !== b.fresh || ", 1)); err == nil {
		t.Errorf("the library whose comparison takes fresh into account passes:\n%s", out)
	}
}

const nodesEqualTypeScriptTest = `

const p = new Parser(mkGrammar(100_000, true, false), "", CodePoints, 1, 100);
const term = (fresh: boolean, text = "a") => new Node("string", "", 0, 1, text, noNodes, noFields, fresh, true);
const tree = (fresh: boolean, kid: Node) => new Node("tuple", "", 0, 1, "", [kid], noFields, fresh, false);
const eq = (a: Node, b: Node) => p.nodesEqual(a, b, { n: 100 });
const cases: [string, boolean, boolean][] = [
  ["a fresh terminal and a final one", eq(term(true), term(false)), true],
  ["trees whose terminals differ in fresh only", eq(tree(false, term(true)), tree(false, term(false))), true],
  ["a fresh tree and a final one", eq(tree(true, term(true)), tree(false, term(true))), true],
  ["terminals of other texts", eq(term(false, "a"), term(false, "b")), false],
  ["trees whose terminals differ in text", eq(tree(false, term(true, "a")), tree(false, term(true, "b"))), false],
];
for (const [name, got, want] of cases) {
  if (got !== want) {
    throw new Error(name + ": nodesEqual is " + got + ", want " + want);
  }
}
`
