package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// publicAliasAllowlist holds the exported aliases of public packages that name an internal type
// today, in the form "<file>: <name>", each with the change that removes it.
var publicAliasAllowlist = []string{}

// internalAliases returns the exported types of src, a Go file of a public package, that are
// aliases of a type of an internal package of the module: the definition of a public type is in
// a public package, so an internal refactor cannot change the public API silently. It reports
// "type X = pkg.Y" and its generic form, where pkg is imported from a path under internal/.
func internalAliases(filename string, src any) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	internal := map[string]bool{} // the names under which the file imports internal packages
	for _, im := range f.Imports {
		p := strings.Trim(im.Path.Value, `"`)
		rest, ok := strings.CutPrefix(p, modulePath+"/")
		if !ok || !(strings.HasPrefix(rest, "internal/") || strings.Contains(rest, "/internal/")) {
			continue
		}
		name := filepath.Base(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		internal[name] = true
	}
	var out []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, s := range g.Specs {
			ts := s.(*ast.TypeSpec)
			if !ts.Assign.IsValid() || !ts.Name.IsExported() {
				continue
			}
			ast.Inspect(ts.Type, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && internal[id.Name] {
						out = append(out, ts.Name.Name)
					}
				}
				return true
			})
		}
	}
	return out, nil
}

// TestNoPublicAliasOfInternalType checks that no exported type of a public package (pkg/**) is an
// alias of a type of an internal package.
func TestNoPublicAliasOfInternalType(t *testing.T) {
	var found, checked int
	used := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(moduleRoot, "pkg"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "/testdata/") {
			return err
		}
		checked++
		names, err := internalAliases(p, nil)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(moduleRoot, p)
		for _, n := range names {
			key := filepath.ToSlash(rel) + ": " + n
			used[key] = true
			found++
			if !slices.Contains(publicAliasAllowlist, key) {
				t.Errorf("%s is an alias of an internal type: define the type in the public package", key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no files under pkg were checked")
	}
	for _, key := range publicAliasAllowlist {
		if !used[key] {
			t.Errorf("the allowlist holds %s, which is not an alias of an internal type", key)
		}
	}
	t.Logf("%d files checked, %d allowlisted", checked, found)
}

// TestInternalAliasNegativeControl checks that the scan reports an alias of an internal type, in
// its plain and generic forms, and neither a defined type, nor an alias of a public type, nor an
// unexported alias.
func TestInternalAliasNegativeControl(t *testing.T) {
	src := `package p

import (
	in "` + modulePath + `/internal/runtime/engine"
	"` + modulePath + `/pkg/runtime/value"
)

type A = in.Node
type B in.Node
type C = value.Node
type d = in.Node
type E[T any] = in.Data[T]
`
	got, err := internalAliases("p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "A" || got[1] != "E" {
		t.Errorf("found %v, want [A E]", got)
	}
}
