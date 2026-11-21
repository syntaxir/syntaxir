package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// The hooks that tests switch through package variables (the mutations of the compiler passes,
// the charge log) are process-wide: a program compiled or a parse run while one is set behaves
// otherwise, so a test that writes one must not run in parallel with other tests. Such a write in a
// parallel test made TestBudgetStreamDefaultIgnoresChunking fail now and then, when the hot spans
// of run sites were such variables: atHotSpans, called by the parallel
// TestCalleeBindingsUndoneAtReturn, lowered them, a run site of the stream's grammar turned hot, and
// the parse stayed under the budget that it must exceed. The settings of a parse are now its own
// (state.Tuning); the compiler's switches are still package state.
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
