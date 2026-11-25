package grammar_test

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

const tooDeep = "nesting too deep"

// nestedASTs make grammar ASTs whose text, as Format writes it, nests n levels, one for each kind
// of level.
var nestedASTs = []struct {
	name string
	ast  func(n int) *ast.Grammar
}{
	{"predicates", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n, func(e ast.Expr) ast.Expr { return &ast.Not{Expr: e} }))
	}},
	{"groups", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n, func(e ast.Expr) ast.Expr { return &ast.Group{Expr: e} }))
	}},
	{"captures", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n, func(e ast.Expr) ast.Expr { return &ast.Capture{Name: "c", Expr: e} }))
	}},
	{"options", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n, func(e ast.Expr) ast.Expr { return &ast.Optional{Expr: e} }))
	}},
	{"repetitions", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n, func(e ast.Expr) ast.Expr { return &ast.Repeat{Expr: e, Min: 1, Max: -1} }))
	}},
	{"stars", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n, func(e ast.Expr) ast.Expr { return &ast.Repeat{Expr: e, Min: 0, Max: -1} }))
	}},
	{"negations", func(n int) *ast.Grammar {
		// The "=>" of the action opens the first level.
		var x ast.ActionExpr = &ast.BoolLit{Value: true}
		for range n - 1 {
			x = &ast.Unary{Op: "!", X: x}
		}
		g := ruleOf(&ast.Literal{Value: "a"})
		g.File.Decls[0].(*ast.RuleDef).Action = x
		return g
	}},
	{"list types", func(n int) *ast.Grammar {
		var t ast.TypeExpr = &ast.TypeRef{Name: "string"}
		for range n {
			t = &ast.ListType{Elem: t}
		}
		return &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.TypeAlias{Name: "T", Type: t}}}}
	}},
}

func ruleOf(e ast.Expr) *ast.Grammar {
	return &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "main", Expr: e}}}}
}

func wrapExpr(n int, wrap func(ast.Expr) ast.Expr) ast.Expr {
	var e ast.Expr = &ast.Literal{Value: "a"}
	for range n {
		e = wrap(e)
	}
	return e
}

// TestValidateBoundsNesting checks that a valid grammar AST is one that the parser reads back:
// validation accepts an AST whose formatted text nests as deep as the parser reads, and rejects
// one level more, whose text the parser rejects as nesting too deep.
func TestValidateBoundsNesting(t *testing.T) {
	for _, c := range nestedASTs {
		t.Run(c.name, func(t *testing.T) {
			at := c.ast(ast.MaxNestingLevels)
			if err := grammar.Validate(at); err != nil {
				t.Errorf("%d levels: %v", ast.MaxNestingLevels, err)
			}
			if _, err := grammar.Load(grammar.FromString("g.sxir", grammar.Format(at))); err != nil {
				t.Errorf("reading the text of %d levels: %.200v", ast.MaxNestingLevels, err)
			}
			over := c.ast(ast.MaxNestingLevels + 1)
			if err := grammar.Validate(over); err == nil || !strings.Contains(err.Error(), tooDeep) {
				t.Errorf("%d levels: validation gives %v, want %q", ast.MaxNestingLevels+1, err, tooDeep)
			}
			if _, err := grammar.Load(grammar.FromString("g.sxir", grammar.Format(over))); err == nil || !strings.Contains(err.Error(), tooDeep) {
				t.Errorf("reading the text of %d levels: %.200v, want %q", ast.MaxNestingLevels+1, err, tooDeep)
			}
		})
	}
}

// TestValidateBoundsCommentNesting checks the comment strings of a grammar AST. Format writes a
// string without a final line break, a trailing string over several lines and a header string
// as a block comment, and validation rejects one that would nest more block comments than the
// parser reads, counting only the block comments of the string that pair up. A string that
// Format writes as line comments nests nothing. Validation checks the strings of every node,
// including those that Format writes only from their layout.
func TestValidateBoundsCommentNesting(t *testing.T) {
	// nested opens n levels: the comment and n-1 pairs nested in it, around an unpaired "*/" and
	// "/*", which Format breaks apart.
	nested := func(n int) string {
		return strings.Repeat("/*", n-1) + " */ /* " + strings.Repeat("*/", n-1)
	}
	for _, c := range []struct {
		name string
		set  func(r *ast.RuleDef, s string)
		// written says whether Format writes the string as a block comment.
		written, block bool
	}{
		{"leading", func(r *ast.RuleDef, s string) { r.LeadingComments = s }, true, true},
		{"detached", func(r *ast.RuleDef, s string) { r.LeadingDetachedComments = []string{s} }, true, true},
		{"trailing", func(r *ast.RuleDef, s string) { r.TrailingComments = []string{s} }, true, true},
		{"trailing over lines", func(r *ast.RuleDef, s string) { r.TrailingComments = []string{"a\n" + s + "\n"} }, true, true},
		{"header", func(r *ast.RuleDef, s string) { r.HeaderComments = []string{s} }, true, true},
		{"keyword", func(r *ast.RuleDef, s string) {
			// Format writes the keyword comments after the attributes of the declaration.
			r.Attrs, r.KeywordComments = []*ast.Attribute{{Name: "a"}}, s
		}, true, true},
		{"of a node", func(r *ast.RuleDef, s string) { r.Expr.(*ast.Literal).LeadingComments = s }, false, true},
		{"line comments", func(r *ast.RuleDef, s string) { r.LeadingComments = s + "\n" }, true, false},
		{"trailing line comment", func(r *ast.RuleDef, s string) { r.TrailingComments = []string{s + "\n"} }, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, n := range []int{ast.MaxNestingLevels, ast.MaxNestingLevels + 1} {
				g := ruleOf(&ast.Literal{Value: "a"})
				c.set(g.File.Decls[0].(*ast.RuleDef), nested(n))
				deep := c.block && n > ast.MaxNestingLevels
				verr := grammar.Validate(g)
				if deep != (verr != nil) || verr != nil && !strings.Contains(verr.Error(), tooDeep) {
					t.Errorf("%d levels: validation gives %v, want an error %v", n, verr, deep)
				}
				if !c.written {
					continue
				}
				_, perr := grammar.Load(grammar.FromString("g.sxir", grammar.Format(g)))
				if deep != (perr != nil) || perr != nil && !strings.Contains(perr.Error(), tooDeep) {
					t.Errorf("%d levels: reading the text gives %.200v, want an error %v", n, perr, deep)
				}
			}
		})
	}
}

// nestedSources make sources that nest n levels as the parser counts them, one for each kind of
// level, and whose text Format may write differently.
var nestedSources = []struct {
	name string
	src  func(n int) string
}{
	{"parentheses", func(n int) string { return "rule a = " + strings.Repeat("(", n) + "x" + strings.Repeat(")", n) }},
	{"lookaheads", func(n int) string { return "rule a = " + strings.Repeat("!", n) + "x" }},
	{"captures", func(n int) string { return "rule a = " + strings.Repeat("a:", n) + "x" }},
	{"options", func(n int) string { return "rule a = x" + strings.Repeat("?", n) }},
	{"repetitions", func(n int) string { return "rule a = x" + strings.Repeat("+", n) }},
	{"stars", func(n int) string { return "rule a = x" + strings.Repeat("*", n) }},
	{"repeat pragmas", func(n int) string {
		return "rule a = " + strings.Repeat("(", n-1) + "x #repeat(1)" + strings.Repeat(")", n-1)
	}},
	{"unbounded repeat pragmas", func(n int) string {
		return "rule a = " + strings.Repeat("(", n-1) + "x #repeat(0,) y" + strings.Repeat(")", n-1)
	}},
	{"negations", func(n int) string { return "rule a = x => " + strings.Repeat("!", n-1) + "true" }},
	{"negative constants", func(n int) string { return "rule a = x => " + strings.Repeat("- ", n-1) + "1" }},
	{"lambdas", func(n int) string { return "var f = " + strings.Repeat("(y) => ", n) + "y" }},
	{"list types", func(n int) string { return "type T ~ " + strings.Repeat("[]", n) + "string" }},
	{"optional types", func(n int) string { return "type T ~ " + strings.Repeat("*", n) + "string" }},
	{"struct types", func(n int) string { return "type T = " + strings.Repeat("{A ", n) + "string" + strings.Repeat("}", n) }},
	{"calls", func(n int) string {
		return "rule a = x => " + strings.Repeat("f(", n-1) + "1" + strings.Repeat(")", n-1)
	}},
	{"constructors", func(n int) string {
		return "rule a = x => " + strings.Repeat("T{a: ", n-2) + "1" + strings.Repeat("}", n-2)
	}},
	{"attributes", func(n int) string {
		return "rule a = " + strings.Repeat("(x @a(", n/2) + "x" + strings.Repeat("))", n/2)
	}},
	{"multi-line body", func(n int) string {
		return "rule a = (\n  x\n  " + strings.Repeat("(", n-1) + "y" + strings.Repeat(")", n-1) + "\n)"
	}},
	{"block comments", func(n int) string { return strings.Repeat("/*", n) + strings.Repeat("*/", n) + "\nrule a = x" }},
	{"block comments in parentheses", func(n int) string {
		return "rule a = " + strings.Repeat("(", n) + strings.Repeat("/*", n) + strings.Repeat("*/", n) + " x" + strings.Repeat(")", n)
	}},
}

// TestParsedGrammarsValidate checks that the AST of every source that the parser reads is valid,
// at the deepest nesting that it reads, so that validation never rejects a parsed grammar.
func TestParsedGrammarsValidate(t *testing.T) {
	for _, c := range nestedSources {
		t.Run(c.name, func(t *testing.T) {
			src := c.src(ast.MaxNestingLevels)
			g, err := grammar.Load(grammar.FromString("g.sxir", src))
			if err != nil {
				t.Fatalf("Load: %.300v", err)
			}
			if err := grammar.Validate(g); err != nil {
				t.Errorf("Validate: %v", err)
			}
			if _, err := grammar.Load(grammar.FromString("g.sxir", grammar.Format(g))); err != nil {
				t.Errorf("reading the formatted text: %.300v", err)
			}
		})
	}
}

// TestValidateNestingAgreesWithParser checks the level count of validation against the parser's
// on random valid grammars: wrapped in the most groups that validation accepts, the text of a rule
// body parses, and in one group more the parser rejects it as nesting too deep.
func TestValidateNestingAgreesWithParser(t *testing.T) {
	t.Parallel()
	g := &astGen{r: rand.New(rand.NewPCG(31, 0)), blocks: true}
	checked := 0
	for i := 0; i < 400 && checked < nestingAgreementRules; i++ {
		base := g.grammar(1 + g.r.IntN(5))
		if ast.Validate(base) != nil {
			continue
		}
		for d, decl := range base.File.Decls {
			r, ok := decl.(*ast.RuleDef)
			if !ok || r.Expr == nil {
				continue
			}
			if _, operators := r.Expr.(*ast.OperatorsRule); operators {
				continue
			}
			checked++
			// wrap returns the grammar with the body of rule d in k groups.
			wrap := func(k int) *ast.Grammar {
				body := r.Expr
				for range k {
					body = &ast.Group{Expr: body}
				}
				copy := *r
				copy.Expr = body
				decls := append([]ast.Decl(nil), base.File.Decls...)
				decls[d] = &copy
				return &ast.Grammar{File: ast.File{Pkg: base.File.Pkg, Decls: decls}}
			}
			parses := func(g *ast.Grammar) bool {
				_, err := grammar.Load(grammar.FromString("g.sxir", grammar.Format(g)))
				if err != nil && !strings.Contains(err.Error(), tooDeep) {
					t.Fatalf("case %d: reading the text: %.300v", i, err)
				}
				return err == nil
			}
			// Validation accepts the body in 0 groups and rejects it in MaxNestingLevels+1, as
			// each group opens a level; find the most groups that it accepts, and check that the
			// parser reads the text there and rejects it in one group more.
			lo, hi := 0, ast.MaxNestingLevels+1
			if grammar.Validate(wrap(hi)) == nil {
				t.Fatalf("case %d, rule %s: valid in %d groups", i, r.Name, hi)
			}
			for hi-lo > 1 {
				mid := (lo + hi) / 2
				if grammar.Validate(wrap(mid)) == nil {
					lo = mid
				} else {
					hi = mid
				}
			}
			if !parses(wrap(lo)) {
				t.Errorf("case %d, rule %s: valid in %d groups, whose text does not parse", i, r.Name, lo)
			}
			if parses(wrap(hi)) {
				t.Errorf("case %d, rule %s: invalid in %d groups, whose text parses", i, r.Name, hi)
			}
		}
	}
	if checked < nestingAgreementRules {
		t.Errorf("only %d rules checked", checked)
	}
}

// deepASTs make grammar ASTs whose tree is n nodes deep, as ast.MaxNestingDepth counts the
// depth, with chains whose text opens few levels. The default tier checks the first two; the long
// tier checks them all, as each reads a text of 50,000 nodes.
var deepASTs = []struct {
	name string
	ast  func(n int) *ast.Grammar
}{
	{"pragmas in groups", func(n int) *ast.Grammar {
		// A group adds no depth: every 200th pragma is in one, about 250 in all.
		var e ast.Expr = &ast.Literal{Value: "a"}
		for i := range n - 2 {
			e = &ast.Atomic{Expr: e}
			if i%200 == 199 {
				e = &ast.Group{Expr: e}
			}
		}
		return ruleOf(e)
	}},
	{"sum of a negative constant", func(n int) *ast.Grammar {
		// A negative constant is 2 deep: it reads back as a negation of its magnitude.
		var x ast.ActionExpr = &ast.IntLit{Value: -1}
		for range n - 3 {
			x = &ast.Binary{Op: "+", L: x, R: &ast.IntLit{Value: 1}}
		}
		return ruleWithAction(x)
	}},
	{"pragmas", func(n int) *ast.Grammar {
		return ruleOf(wrapExpr(n-2, func(e ast.Expr) ast.Expr { return &ast.Atomic{Expr: e} }))
	}},
	{"sums", func(n int) *ast.Grammar {
		var x ast.ActionExpr = &ast.IntLit{Value: 1}
		for range n - 2 {
			x = &ast.Binary{Op: "+", L: x, R: &ast.IntLit{Value: 1}}
		}
		return ruleWithAction(x)
	}},
	{"field accesses", func(n int) *ast.Grammar {
		var x ast.ActionExpr = &ast.CaptureRef{Name: "x"}
		for range n - 2 {
			x = &ast.FieldAccess{X: x, Name: "B"}
		}
		return ruleWithAction(x)
	}},
}

// ruleWithAction returns a grammar of one rule that captures x and has the action x.
func ruleWithAction(x ast.ActionExpr) *ast.Grammar {
	g := ruleOf(&ast.Capture{Name: "x", Expr: &ast.Literal{Value: "a"}})
	g.File.Decls[0].(*ast.RuleDef).Action = x
	return g
}

// TestValidateBoundsDepth checks the structural depth bound of validation against the parser's:
// an AST of ast.MaxNestingDepth nodes is valid and its text reads back as the same grammar, and
// one node deeper is invalid, as is its text.
func TestValidateBoundsDepth(t *testing.T) {
	families := deepASTs
	if !depthAllShapes {
		families = families[:2]
	}
	for _, c := range families {
		t.Run(c.name, func(t *testing.T) {
			at := c.ast(ast.MaxNestingDepth)
			if err := grammar.Validate(at); err != nil {
				t.Fatalf("depth %d: %v", ast.MaxNestingDepth, err)
			}
			text := grammar.Format(at)
			back, err := grammar.Load(grammar.FromString("g.sxir", text))
			if err != nil {
				t.Fatalf("reading the text of depth %d: %.200v", ast.MaxNestingDepth, err)
			}
			if grammar.Format(back) != text {
				t.Errorf("the text of depth %d reads back as another grammar", ast.MaxNestingDepth)
			}
			over := c.ast(ast.MaxNestingDepth + 1)
			if err := grammar.Validate(over); err == nil || !strings.Contains(err.Error(), tooDeep) {
				t.Errorf("depth %d: validation gives %v, want %q", ast.MaxNestingDepth+1, err, tooDeep)
			}
			if _, err := grammar.Load(grammar.FromString("g.sxir", grammar.Format(over))); err == nil || !strings.Contains(err.Error(), tooDeep) {
				t.Errorf("reading the text of depth %d: %.200v, want %q", ast.MaxNestingDepth+1, err, tooDeep)
			}
		})
	}
}

// TestValidateBoundsSharedDepth checks that a subtree shared at two depths counts at the
// deeper one, though validation checks it once.
func TestValidateBoundsSharedDepth(t *testing.T) {
	shared := wrapExpr(ast.MaxNestingDepth-10, func(e ast.Expr) ast.Expr { return &ast.Atomic{Expr: e} })
	shallow := &ast.Seq{Items: []ast.Expr{shared, &ast.Literal{Value: "b"}}}
	for _, c := range []struct {
		above int
		valid bool
	}{{6, true}, {7, true}, {8, false}} {
		deep := wrapExpr(c.above, func(e ast.Expr) ast.Expr { return &ast.Atomic{Expr: e} })
		// Replace the innermost operand of deep, a literal, with the shared chain.
		inner := deep
		for range c.above - 1 {
			inner = inner.(*ast.Atomic).Expr
		}
		inner.(*ast.Atomic).Expr = shared
		g := ruleOf(&ast.Choice{Alts: []ast.Expr{shallow, deep}})
		if err := grammar.Validate(g); (err == nil) != c.valid {
			t.Errorf("%d pragmas above the shared chain: validation gives %v, want valid %v", c.above, err, c.valid)
		}
	}
}

// TestValidateSharedBodyOnce checks that validation walks a rule body that shares its sequences
// and choices once for each of them: the body below has 2^60 paths from its root to its literal.
func TestValidateSharedBodyOnce(t *testing.T) {
	var e ast.Expr = &ast.Literal{Value: "a"}
	for i := range 60 {
		if i%2 == 0 {
			e = &ast.Seq{Items: []ast.Expr{e, e}}
		} else {
			e = &ast.Choice{Alts: []ast.Expr{e, e}}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := ast.ValidateContext(ctx, ruleOf(e)); err != nil {
		t.Fatal(err)
	}
}
