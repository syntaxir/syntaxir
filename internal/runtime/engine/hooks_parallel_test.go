package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hooks that tests switch through package variables (the mutations of the compiler passes,
// the charge log) are process-wide: a program compiled or a parse run while one is set behaves
// otherwise, so a test that writes one must not run in parallel with other tests. Such a write in a
// parallel test made TestBudgetStreamDefaultIgnoresChunking fail now and then, when the hot spans
// of run sites were such variables: statetest.AtHotSpans, called by the parallel
// TestCalleeBindingsUndoneAtReturn, lowered them, a run site of the stream's grammar turned hot, and
// the parse stayed under the budget that it must exceed. The settings of a parse are now its own
// (state.Tuning, TestRuntimeHasNoPackageSettings); the compiler's switches are still package state.
//
// This test finds the tests that call t.Parallel and write a hook, directly or through a function of
// the package that writes one.
func TestParallelTestsDoNotWriteProcessHooks(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var funcs []*ast.FuncDecl
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Body != nil {
				funcs = append(funcs, fd)
			}
		}
	}
	hooks := map[string]bool{
		"corepass.EnvFreeMutation": true, "corepass.RunKeyMutation": true, "state.ChargeLog": true,
	}
	isHook := func(e ast.Expr) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		x, ok := sel.X.(*ast.Ident)
		return ok && hooks[x.Name+"."+sel.Sel.Name]
	}
	// writers holds the functions that write a hook, directly or by calling one that does.
	writers := map[string]string{}
	for changed := true; changed; {
		changed = false
		for _, fd := range funcs {
			if _, ok := writers[fd.Name.Name]; ok {
				continue
			}
			var why string
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.AssignStmt:
					for _, l := range n.Lhs {
						if isHook(l) {
							why = "assigns " + l.(*ast.SelectorExpr).X.(*ast.Ident).Name + "." + l.(*ast.SelectorExpr).Sel.Name
						}
					}
				case *ast.CallExpr:
					if id, ok := n.Fun.(*ast.Ident); ok {
						if w, ok := writers[id.Name]; ok && id.Name != fd.Name.Name {
							why = "calls " + id.Name + ", which " + w
						}
					}
				}
				return why == ""
			})
			if why != "" {
				writers[fd.Name.Name] = why
				changed = true
			}
		}
	}
	checked := 0
	for _, fd := range funcs {
		if !strings.HasPrefix(fd.Name.Name, "Test") {
			continue
		}
		parallel := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
					parallel = true
				}
			}
			return true
		})
		if !parallel {
			continue
		}
		checked++
		if why, ok := writers[fd.Name.Name]; ok {
			t.Errorf("%s calls t.Parallel and %s: a hook is process-wide, so the test must be serial", fd.Name.Name, why)
		}
	}
	if checked == 0 || len(writers) == 0 {
		t.Fatalf("the scan found %d parallel tests and %d writers of hooks: it does not see the tests", checked, len(writers))
	}
}

// TestRuntimeHasNoPackageSettings checks that the runtime of the engine and the Go runtime library
// of generated parsers keep no setting of a parse in an exported package variable of a number or a
// truth value, which any code in the process could write: the thresholds of the charge model are
// constants, the same on every backend ("Run Sites" and "Scan Sites" in spec/runtime/parsing.md),
// and the test-only settings belong to each parse (state.Tuning).
func TestRuntimeHasNoPackageSettings(t *testing.T) {
	t.Parallel()
	found := 0
	for _, dir := range []string{"../state", "../../../pkg/runtime/libgo"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.VAR {
					continue
				}
				for _, spec := range g.Specs {
					v := spec.(*ast.ValueSpec)
					for i, name := range v.Names {
						found++
						if !name.IsExported() {
							continue
						}
						if basicSetting(v.Type) || i < len(v.Values) && basicLiteral(v.Values[i]) {
							t.Errorf("%s/%s: the exported variable %s holds a number or a truth value", dir, e.Name(), name.Name)
						}
					}
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("the scan found no package variable: it does not see the sources")
	}
}

// basicSetting reports whether a declared type is a number or a truth value.
func basicSetting(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Name {
	case "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64":
		return true
	}
	return false
}

// basicLiteral reports whether an initial value is a number or a truth value written out.
func basicLiteral(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Kind == token.INT || e.Kind == token.FLOAT
	case *ast.Ident:
		return e.Name == "true" || e.Name == "false"
	case *ast.UnaryExpr:
		return basicLiteral(e.X)
	case *ast.BinaryExpr:
		return basicLiteral(e.X) && basicLiteral(e.Y)
	case *ast.ParenExpr:
		return basicLiteral(e.X)
	}
	return false
}
