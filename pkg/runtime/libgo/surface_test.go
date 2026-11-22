package libgo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// exportedNames returns the exported top-level names of the non-test files of the package in dir
// (other than methods), with the doc comment of each. Constants and variables have the doc
// comment of their own spec or, if none, of their declaration; it is only a mark for functions
// and types, since a group of constants shares one.
func exportedNames(t *testing.T, dir string) map[string]string {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	valueNames = map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil && d.Name.IsExported() {
						names[d.Name.Name] = d.Doc.Text()
					}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						doc := d.Doc
						switch s := s.(type) {
						case *ast.TypeSpec:
							if s.Doc != nil {
								doc = s.Doc
							}
							if s.Name.IsExported() {
								names[s.Name.Name] = doc.Text()
							}
						case *ast.ValueSpec:
							if s.Doc != nil {
								doc = s.Doc
							}
							for _, n := range s.Names {
								if n.IsExported() {
									valueNames[n.Name] = true
									names[n.Name] = doc.Text()
								}
							}
						}
					}
				}
			}
		}
	}
	return names
}

// sameNameAsRuntime are the names that the library shares with a runtime package without
// duplicating its meaning. The library's Parser is the state of a parse that the generated rule
// functions run on, and its Document the concrete type behind runtime.Document: a generated
// parser hands a Document out only as runtime.Document (or as the parser package's own wrapper
// of it), and re-exports neither name (TestAPIReExportsNoLibraryName in the print package), so
// neither is user API.
var sameNameAsRuntime = []string{"Document", "Parser"}

// valueNames are the constants and variables that the last call of exportedNames found.
var valueNames map[string]bool

var protocolName = regexp.MustCompile(`^Protocol[0-9]+$`)

// TestSupportSurface holds the library to the surface of support for generated parsers: every
// exported name is marked as for generated parsers (not a stable API) or is a protocol constant,
// and no name repeats an exported name of the runtime packages, which define the user API once.
func TestSupportSurface(t *testing.T) {
	lib := exportedNames(t, ".")
	if len(lib) == 0 {
		t.Fatal("no exported names found")
	}
	values := valueNames
	for name, doc := range lib {
		if values[name] || protocolName.MatchString(name) || strings.Contains(doc, "generated parsers") {
			continue
		}
		t.Errorf("%s is a function or type of the library that is not marked as for generated parsers (not a stable API)", name)
	}
	for _, dir := range []string{"..", "../input", "../value", "../instrument"} {
		for name := range exportedNames(t, dir) {
			if _, ok := lib[name]; ok && !slices.Contains(sameNameAsRuntime, name) {
				t.Errorf("%s is exported by the library and by %s: a user-facing definition belongs to the runtime package only", name, dir)
			}
		}
	}
}

// TestSupportSurfaceNegativeControl checks the test's own tools: a name without the mark and a
// name that a runtime package also exports are both found.
func TestSupportSurfaceNegativeControl(t *testing.T) {
	rt := exportedNames(t, "..")
	for _, name := range []string{"WithLimits", "ParseOption", "Limits"} {
		if _, ok := rt[name]; !ok {
			t.Errorf("package runtime exports no %s: the duplicate check would not see a library %s", name, name)
		}
	}
	if doc, ok := exportedNames(t, ".")["NewGrammar"]; !ok || !strings.Contains(doc, "generated parsers") {
		t.Error("NewGrammar is not found with its mark")
	}
}
