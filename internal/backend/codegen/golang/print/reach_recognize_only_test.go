package print_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"maps"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestParsersWithoutNodeRulesPrintTheirOwnBodies checks that a parser without Node rules, which
// generates the Node bodies for their frame layouts and does not print them, prints the bodies that
// its recognizer would share with them: the recognizer of textPredicate keeps the value of name,
// whose body is that of the Node table. The output calls nothing that it does not define, has no
// Node rule body, and declares no function that nothing refers to, so the recognizer's copies are
// the only bodies printed.
func TestParsersWithoutNodeRulesPrintTheirOwnBodies(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testParsersWithoutNodeRulesPrintTheirOwnBodies)
}

func testParsersWithoutNodeRulesPrintTheirOwnBodies(t *testing.T) {
	src := "@start\n" + textPredicate
	for _, tc := range []struct {
		name string
		opts common.GenOptions
	}{
		{"recognize-only", common.GenOptions{RecognizeOnly: true}},
		{"typed-only with Recognize", common.GenOptions{Types: true, TypedOnly: true, Recognize: true}},
	} {
		code := generateReach(t, src, tc.opts)
		if got := undefinedGenerated(t, code); len(got) > 0 {
			t.Errorf("%s: the output calls undefined functions %v", tc.name, got)
		}
		if strings.Contains(string(code), "\trules[") && strings.Contains(string(code), "].Body = ") {
			for _, l := range strings.Split(string(code), "\n") {
				if strings.HasPrefix(l, "\trules[") && strings.Contains(l, "].Body = ") {
					t.Errorf("%s: the output sets the Node rule body %q", tc.name, strings.TrimSpace(l))
				}
			}
		}
		if got := unreferencedFuncs(t, code); len(got) > 0 {
			t.Errorf("%s: the output declares functions that nothing refers to: %v", tc.name, got)
		}
	}
}

// unreferencedFuncs returns the generated functions of src (named as undefinedGenerated matches them)
// that no identifier of src refers to.
func unreferencedFuncs(t *testing.T, src []byte) []string {
	t.Helper()
	f, err := goparser.ParseFile(token.NewFileSet(), "parser.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[*ast.Ident]bool{}
	var funcs []string
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && generatedName.MatchString(fd.Name.Name) {
			declared[fd.Name] = true
			funcs = append(funcs, fd.Name.Name)
		}
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !declared[id] {
			used[id.Name] = true
		}
		return true
	})
	var unused []string
	for _, name := range funcs {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	return unused
}

// TestCheckDiscarded checks that the generator fails on any file it prints for the grammar (the parser,
// its API and its types) that refers to a function it discarded, and only on an identifier of one.
func TestCheckDiscarded(t *testing.T) {
	t.Parallel()
	gone := []string{"e3"}
	clean := map[string]string{
		"parser.go":      "package p\n\nfunc e30(p *Parser) {\n\t_ = \"e3\" // e3\n}\n",
		"api.go":         "package p\n\nfunc Recognize(s string) error { return nil }\n",
		"types/types.go": "package types\n\ntype Main struct{}\n",
	}
	if err := print.CheckDiscarded(gone, clean); err != nil {
		t.Errorf("files that only spell the name in other tokens: %v", err)
	}
	for _, tc := range []struct{ path, text string }{
		{"parser.go", "package p\n\nfunc q1(p *Parser) {\n\tv, ok := e3(p, 0)\n}\n"},
		{"parser.go", "package p\n\nfunc init() {\n\trecRules[0].Body = e3\n}\n"},
		{"api.go", "package p\n\nvar recognizeMain = e3\n"},
		{"types/types.go", "package types\n\nvar _ = e3\n"},
	} {
		files := maps.Clone(clean)
		files[tc.path] = tc.text
		if err := print.CheckDiscarded(gone, files); err == nil || !strings.Contains(err.Error(), tc.path+" refers to e3") {
			t.Errorf("%s naming a discarded function: err = %v, want an internal error naming the file and e3", tc.path, err)
		}
	}
	if err := print.CheckDiscarded(nil, map[string]string{"parser.go": "package p\n\nvar _ = e3\n"}); err != nil {
		t.Errorf("a generator that discarded nothing: %v", err)
	}
}
