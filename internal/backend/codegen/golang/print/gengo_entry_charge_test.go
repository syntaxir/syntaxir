package print_test

import (
	"bytes"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestGeneratedRuleEntryCharge checks, on the checked-in parsers generated with limit controls, that every
// matcher function of the Node and typed tables (rule bodies, framed bodies, flat-rule methods and the
// lines and skips of Operators Rules) begins with its entry and has no other Spend before its first
// loop (the steps that the code of an action expression charges, isActionStep, are not the rule's). The entry is one
// Spend for the evaluation of the body, or two in a row for the flat method of a plain rule that a
// call site calls directly: the unit of the call, then that of the body (spec/runtime/parsing.md#cost).
// A typed wrapper u<N> or i<N> that only saves and restores state around a call of another matcher
// function charges nothing but the call, and the Document-dispatch wrapper s<N> of a plain rule hands
// the call to DocumentCall or charges it and calls the body ds<N> that charges the evaluation. The
// lowered function of Flow IR charges no entry of its own (flow.Attrs.Entered); the method that holds it
// does, so a rule whose method lost the charge would consume input uncharged without Verify noticing.
func TestGeneratedRuleEntryCharge(t *testing.T) {
	t.Parallel()
	name := regexp.MustCompile(`^(ds|[eiqrsuv])[0-9]+$`)
	wrapper := regexp.MustCompile(`^[isu][0-9]+$`) // typed wrappers: state or dispatch around a call of the charging function
	var paths []string
	for _, pattern := range []string{"../../../../bench/parsers/*/parser.go"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range m {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(src, []byte(".Spend()")) {
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		t.Fatal("no checked-in parser with limit controls found")
	}
	for _, path := range paths {
		f, err := goparser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, d := range f.Decls {
			fn, ok := d.(*goast.FuncDecl)
			if !ok || fn.Recv != nil || !name.MatchString(fn.Name.Name) || fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
				continue
			}
			n++
			if !startsWithStep(fn.Body) && !(wrapper.MatchString(fn.Name.Name) && delegates(fn.Body, name)) {
				t.Errorf("%s: %s does not begin with p.Spend() or delegate to a matcher function that does", path, fn.Name.Name)
			}
			steps := 0
			goast.Inspect(fn.Body, func(x goast.Node) bool {
				if _, loop := x.(*goast.ForStmt); loop {
					return false
				}
				if isStepCall(x) && !isActionStep(x) {
					steps++
				}
				return true
			})
			if lead := leadingSteps(fn.Body); steps > 1 && (steps != lead || lead > 2) {
				t.Errorf("%s: %s charges %d steps outside its loops, %d of them at its start, want the entry only", path, fn.Name.Name, steps, lead)
			}
		}
		if n == 0 {
			t.Errorf("%s: no matcher functions found", path)
		}
	}
}

func isStepCall(x goast.Node) bool {
	c, ok := x.(*goast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*goast.SelectorExpr)
	return ok && s.Sel.Name == "Spend"
}

// isActionStep reports whether the step call x is c.P.Spend(): the step that the code of an action expression, printed
// inside the matcher function, charges for an element it makes or a field it sets, as the library's
// constructors charge it. It bounds the work of the action expression, not the entry of the rule.
func isActionStep(x goast.Node) bool {
	s := x.(*goast.CallExpr).Fun.(*goast.SelectorExpr)
	inner, ok := s.X.(*goast.SelectorExpr)
	if !ok || inner.Sel.Name != "P" {
		return false
	}
	id, ok := inner.X.(*goast.Ident)
	return ok && id.Name == "c"
}

func startsWithStep(b *goast.BlockStmt) bool {
	for _, s := range b.List {
		if e, ok := s.(*goast.ExprStmt); ok {
			return isStepCall(e.X)
		}
		if _, ok := s.(*goast.DeclStmt); ok {
			continue
		}
		return false
	}
	return false
}

// leadingSteps returns the number of rule steps (not those of an action expression) that b begins with, in a row.
func leadingSteps(b *goast.BlockStmt) int {
	n := 0
	for _, s := range b.List {
		e, ok := s.(*goast.ExprStmt)
		if !ok || !isStepCall(e.X) || isActionStep(e.X) {
			break
		}
		n++
	}
	return n
}

// delegates reports whether b calls a function named like a matcher function: the typed wrappers of a rule
// (the wrappers u<N> and i<N>) save and restore state around a call of the function that charges.
func delegates(b *goast.BlockStmt, name *regexp.Regexp) bool {
	found := false
	goast.Inspect(b, func(x goast.Node) bool {
		if c, ok := x.(*goast.CallExpr); ok {
			if id, ok := c.Fun.(*goast.Ident); ok && name.MatchString(id.Name) {
				found = true
			}
		}
		return !found
	})
	return found
}
