package complexity

import "testing"

// TestClassifyMissingInRecovery classifies grammars with one expression of #missing and
// #recover, in either order and under a label: the recovery is a scanning source whose unit can
// fail after it, whichever attribute is outermost, and the classifier finds it at the node of
// each attribute.
func TestClassifyMissingInRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		class     ComplexityClass
		degree    int
	}{
		{"missing then recover", "rule main = (x / \"a\")* $$\nrule x = (\"b\" #missing #recover(skip=s)) \"c\"\nrule s = [a]+\n", ClassPoly, 2},
		{"labelled", "rule main = (x / \"a\")* $$\nrule x = (\"b\" #missing #recover(skip=s) @error(message=\"b\")) \"c\"\nrule s = [a]+\n", ClassPoly, 2},
		{"label inside", "rule main = (x / \"a\")* $$\nrule x = (\"b\" #missing @error(message=\"b\") #recover(skip=s)) \"c\"\nrule s = [a]+\n", ClassPoly, 2},
		{"reused skip", "rule main = (x / \"a\")* $$\nrule x = (\"b\" #missing #recover(skip=[a]+)) \"c\"\n", ClassLinear, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := classifySource(t, tc.src)
			if c.Class != tc.class || classDegree(c) != tc.degree {
				t.Fatalf("class %v degree %d, want %v degree %d\ncauses %v\nreasons %v", c.Class, c.Degree, tc.class, tc.degree, c.Causes, c.Reasons)
			}
		})
	}
}
