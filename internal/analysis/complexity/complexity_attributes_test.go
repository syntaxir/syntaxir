package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/testing/starttest"
)

// TestAttributesOfOneExpression checks that attributes reads the nodes of the attributes of one
// expression down to its operand, with the innermost #recover, the one that recovers the operand,
// and that attributes written around a parenthesized expression of attributes are read as
// attributes of that expression, as the front end writes them.
func TestAttributesOfOneExpression(t *testing.T) {
	g, err := parser.Parse("rule one = \"b\" #missing #recover(skip=s) @error(message=\"m\")\n" +
		"rule nested = (\"b\" #missing) #recover(skip=s)\n" +
		"rule two = \"b\" #recover(skip=s) #recover(skip=\"x\")\n" +
		"rule s = [a]+\n")
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := prepareClassifier(starttest.MarkAll(g), "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		rule       string
		operand    func(core.Node) bool
		skip       string
		hasMissing bool
	}{
		{"one", isLit, "s", true},
		{"nested", isLit, "s", true},
		{"two", isLit, "s", false},
	} {
		operand, rec, missing := attributes(k.ruleBody(tc.rule))
		if !tc.operand(operand) {
			t.Errorf("%s: operand %T", tc.rule, operand)
		}
		if rec == nil {
			t.Errorf("%s: no #recover", tc.rule)
		} else if got := k.showOf(rec.Skip); got != tc.skip {
			t.Errorf("%s: skip %s, want %s", tc.rule, got, tc.skip)
		}
		if missing != tc.hasMissing {
			t.Errorf("%s: missing %v, want %v", tc.rule, missing, tc.hasMissing)
		}
	}
	if operand, rec, missing := attributes(k.ruleBody("s")); operand != k.ruleBody("s") || rec != nil || missing {
		t.Errorf("a node of no attribute: %T %v %v", operand, rec, missing)
	}
}

func isLit(n core.Node) bool { _, ok := n.(*core.Lit); return ok }
