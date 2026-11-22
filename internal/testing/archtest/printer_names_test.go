package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// The names of the printers (record 074, Printers Take Exec-Stage Units): a printer prints the units
// of the Exec stage, so no file or declared name of its packages says that it prints the Flow stage.
// The rule covers the non-test files of printPackages: a file name or a declared identifier (a
// function, method, type, variable, constant, field, parameter, result or label) that has the word
// "flow" among its words is reported. Words split at underscores, dots and case changes, so that
// "overflow" and "Overflowed" are one word each and pass.

// nameWords returns the words of a Go identifier or file name: the parts between underscores and
// dots, split before an upper-case letter that follows a lower-case one or that starts a lower-case
// run after an upper-case run (so "HTTPFlow" is "HTTP" and "Flow").
func nameWords(s string) []string {
	var out []string
	var cur []rune
	rs := []rune(s)
	for i, r := range rs {
		if r == '_' || r == '.' {
			if len(cur) > 0 {
				out = append(out, string(cur))
				cur = nil
			}
			continue
		}
		if unicode.IsUpper(r) && len(cur) > 0 && (unicode.IsLower(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]))) {
			out = append(out, string(cur))
			cur = nil
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// namesFlow reports whether s has the word "flow" in any case.
func namesFlow(s string) bool {
	return slices.ContainsFunc(nameWords(s), func(w string) bool { return strings.EqualFold(w, "flow") })
}

// printerFlowNames returns the files of the packages prints of pkgs whose names have the word "flow",
// as "<file>", and the identifiers that their non-test files declare with that word, as
// "<file>: <identifier>", once per file and identifier.
func printerFlowNames(pkgs map[string]*sourcePackage, module string, prints []string) []string {
	var out []string
	for _, pr := range prints {
		sp := pkgs[module+"/"+pr]
		if sp == nil {
			out = append(out, fmt.Sprintf("%s: no such package", pr))
			continue
		}
		for i, f := range sp.files {
			file := pr + "/" + sp.fileNames[i]
			if namesFlow(sp.fileNames[i]) {
				out = append(out, file)
			}
			seen := map[string]bool{}
			add := func(ids ...*ast.Ident) {
				for _, id := range ids {
					if id != nil && namesFlow(id.Name) && !seen[id.Name] {
						seen[id.Name] = true
						out = append(out, file+": "+id.Name)
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					add(x.Name)
				case *ast.TypeSpec:
					add(x.Name)
				case *ast.ValueSpec:
					add(x.Names...)
				case *ast.Field:
					add(x.Names...)
				case *ast.AssignStmt:
					if x.Tok == token.DEFINE {
						for _, l := range x.Lhs {
							if id, ok := l.(*ast.Ident); ok {
								add(id)
							}
						}
					}
				case *ast.RangeStmt:
					if x.Tok == token.DEFINE {
						for _, e := range []ast.Expr{x.Key, x.Value} {
							if id, ok := e.(*ast.Ident); ok {
								add(id)
							}
						}
					}
				case *ast.LabeledStmt:
					add(x.Label)
				}
				return true
			})
		}
	}
	return out
}

// TestPrintersNameNoFlowStage checks the names of the printers.
func TestPrintersNameNoFlowStage(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range printerFlowNames(pkgs, modulePath, printPackages) {
		t.Errorf("%s names the Flow stage; a printer prints the units of the Exec stage", n)
	}
}

// The negative control of the name rule: testdata/printers, whose fake print package has a file
// flow_ops.go that declares names with the word at each kind of declaration, and names that only
// contain the letters (overflow, Overflowed, Workflows) and pass.
func TestPrinterNameRuleNegativeControl(t *testing.T) {
	const module = "example.com/print"
	pkgs, err := loadSources(filepath.Join("testdata", "printers"), module)
	if err != nil {
		t.Fatal(err)
	}
	const p = "internal/backend/codegen/golang/print"
	const f = p + "/flow_ops.go"
	got := printerFlowNames(pkgs, module, []string{p})
	want := []string{
		f,
		f + ": tsFlowType",
		f + ": FlowTarget",
		f + ": printFlow",
		f + ": flowArg",
		f + ": HTTPFlowResult",
		f + ": flowVar",
		f + ": flow",
		f + ": rangeFlow",
		f + ": Flow",
		f + ": flowConst",
	}
	if !slices.Equal(got, want) {
		t.Errorf("names:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, s := range []string{"overflow", "Overflowed", "Workflows", "exec_rules.go"} {
		if namesFlow(s) {
			t.Errorf("namesFlow(%q) = true, want false", s)
		}
	}
}
