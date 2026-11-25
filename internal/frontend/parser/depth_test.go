package parser

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// depthFamilies make sources whose tree is n nodes deep, as ast.MaxNestingDepth counts the
// depth, with chains that open no levels in the text: the rule is 1 deep, and each pragma,
// operator or postfix action one more down to the operand. The default tier checks the first;
// the long tier checks them all, as each parses a source of 50,000 nodes.
var depthFamilies = []struct {
	name string
	src  func(n int) string
}{
	{"pragmas", func(n int) string { return "rule a = x" + strings.Repeat(" #atomic", n-2) }},
	{"sums", func(n int) string { return "rule a = x => 1" + strings.Repeat(" + 1", n-2) }},
	{"field accesses", func(n int) string { return "rule a = x:x => $x" + strings.Repeat(".B", n-2) }},
	{"indexes", func(n int) string { return "rule a = x:x => $x" + strings.Repeat("[0]", n-2) }},
	{"streams", func(n int) string { return "rule a = x" + strings.Repeat(" #stream", n-2) }},
}

// TestDepthLimit checks the structural depth bound of the parsers at the boundary: the tree of
// ast.MaxNestingDepth nodes is accepted, and one node more fails with "nesting too deep", which
// ParsePartial reports with the declaration left out.
func TestDepthLimit(t *testing.T) {
	families := depthFamilies
	if !depthAllFamilies {
		families = families[:1]
	}
	for _, f := range families {
		t.Run(f.name, func(t *testing.T) {
			src := f.src(ast.MaxNestingDepth)
			if _, err := Parse(src); err != nil {
				t.Errorf("Parse of depth %d: %.200v", ast.MaxNestingDepth, err)
			}
			if _, err := ParseReference(src); err != nil {
				t.Errorf("ParseReference of depth %d: %.200v", ast.MaxNestingDepth, err)
			}
			src = f.src(ast.MaxNestingDepth + 1)
			for name, err := range map[string]error{"Parse": errOf(Parse(src)), "ParseReference": errOf(ParseReference(src))} {
				if err == nil || !strings.Contains(err.Error(), msgNestingTooDeep) {
					t.Errorf("%s of depth %d: %.200v, want %q", name, ast.MaxNestingDepth+1, err, msgNestingTooDeep)
				}
			}
			for name, parse := range map[string]func(string) (*ast.Grammar, ErrorList){"ParsePartial": ParsePartial, "ParsePartialReference": ParsePartialReference} {
				g, errs := parse(src)
				if errs == nil || !strings.Contains(errs.Error(), msgNestingTooDeep) || g == nil || len(g.File.Decls) != 0 {
					t.Errorf("%s of depth %d: %.200v, want %q and no declaration", name, ast.MaxNestingDepth+1, errs, msgNestingTooDeep)
				}
			}
		})
	}
}
