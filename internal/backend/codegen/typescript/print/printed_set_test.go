package print

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/typescript/tsprim"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// The printer and the analysis ts-printable read one set of the primitives that the printer prints
// (tsprim.Printed) and one of the types of the variables it declares (tsprim.Declared). These tests
// tie the printer to both sets in both directions: the printer's dispatch names exactly the printed
// primitives, a primitive outside the set reaches the panic of an admitted construct, and the printer
// declares exactly the declared types.

// constValues returns the string constants that the Go file path declares, by name.
func constValues(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v, _ := strconv.Unquote(lit.Value)
					out[n.Name] = v
				}
			}
		}
	}
	return out
}

// dispatchFuncs are the functions of the printer that choose the form of a call of a primitive by its
// name: the statements, the values, the assignments and the hooks of the printing of a body.
var dispatchFuncs = map[string]bool{"stmtOp": true, "expr": true, "setText": true, "bodyTarget": true}

// The primitives that the printer's dispatch names are exactly the printed ones.
func TestPrinterDispatchNamesThePrintedPrimitives(t *testing.T) {
	primConsts := constValues(t, filepath.Join("..", "..", "..", "..", "ir", "code", "exec", "prim", "names.go"))
	tsConsts := constValues(t, filepath.Join("..", "tsprim", "tsprim.go"))
	named := map[string]bool{}
	for _, file := range []string{"prim_ops.go", "exec_rules.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !dispatchFuncs[fd.Name.Name] {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				x, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch {
				case x.Name == "prim" && primConsts[sel.Sel.Name] != "" && strings.HasPrefix(sel.Sel.Name, "Op"):
					named[primConsts[sel.Sel.Name]] = true
				case x.Name == "tsprim" && strings.HasSuffix(sel.Sel.Name, "Op"):
					named[tsConsts[sel.Sel.Name]] = true
				}
				return true
			})
		}
	}
	all := append(prim.OpNames(), tsprim.MergeOp, tsprim.FailedOp)
	var printed []string
	for _, op := range all {
		if tsprim.Printed(op) {
			printed = append(printed, op)
		}
	}
	slices.Sort(printed)
	if got := slices.Sorted(maps.Keys(named)); !slices.Equal(got, printed) {
		t.Errorf("the printer's dispatch names\n%s\nthe printed primitives are\n%s", strings.Join(got, "\n"), strings.Join(printed, "\n"))
	}
}

// admittedPanic runs f and returns the message of the panic of an admitted construct that it raises,
// or "".
func admittedPanic(f func()) (msg string) {
	defer func() {
		if e := recover(); e != nil {
			s, _ := e.(string)
			if strings.HasPrefix(s, "tsgen: the analysis admitted ") {
				msg = s
				return
			}
			panic(e)
		}
	}()
	f()
	return ""
}

// A primitive outside the printed set reaches the panic of an admitted construct, as a statement and
// as a value.
func TestPrinterPanicsOnUnprintedPrimitives(t *testing.T) {
	n := 0
	for _, op := range prim.OpNames() {
		if tsprim.Printed(op) {
			continue
		}
		n++
		c := &code.Call{Callee: op}
		p := &tsPrimPrinter{}
		want := "tsgen: the analysis admitted the operation " + op
		if got := admittedPanic(func() { p.stmtOp(c, func() []string { return nil }) }); got != want {
			t.Errorf("stmtOp(%s) panics %q, want %q", op, got, want)
		}
		if got := admittedPanic(func() { p.expr(c, func() []string { return nil }) }); got != want {
			t.Errorf("expr(%s) panics %q, want %q", op, got, want)
		}
	}
	if n == 0 {
		t.Fatal("every primitive is printed: the test checks nothing")
	}
}

// A demand of input prints only in a resumable function, the generator that can suspend: in another
// function, which the resumable analysis (prim.Attrs.Resumable) found reaches no demand, it reaches the
// panic of an admitted construct, and in a resumable function it prints the loop that yields.
func TestPrinterDemandsOnlyInResumableFunctions(t *testing.T) {
	for _, d := range []prim.Demand{{Char: true}, {Lit: prim.Lit{Text: "aé"}}} {
		c := &code.Call{Callee: prim.OpInputDemand, Imm: d}
		p := &tsPrimPrinter{}
		want := "tsgen: the analysis admitted a demand of input outside a resumable function"
		if got := admittedPanic(func() { p.stmtOp(c, func() []string { return nil }) }); got != want {
			t.Errorf("a demand %+v outside a resumable function panics %q, want %q", d, got, want)
		}
		r := &tsPrimPrinter{gen: true}
		if got := admittedPanic(func() { r.stmtOp(c, func() []string { return nil }) }); got != "" {
			t.Errorf("a demand %+v in a resumable function panics %q", d, got)
		}
		if out := r.b.String(); !strings.Contains(out, "yield lacks;") {
			t.Errorf("a demand %+v in a resumable function prints %q, which does not yield", d, out)
		}
	}
}

// The printer declares a variable of each declared type but a mark, which it declares as the four
// variables of its parts (printTSFunc), and of no other type.
func TestPrinterDeclaresTheDeclaredTypes(t *testing.T) {
	for _, typ := range prim.Types() {
		got := admittedPanic(func() { tsPrimType(typ) })
		switch {
		case typ == prim.TypeMark:
			if !tsprim.Declared(typ) {
				t.Errorf("a mark is not declared")
			}
		case tsprim.Declared(typ) && got != "":
			t.Errorf("tsPrimType(%s) panics %q, but the type is declared", typ, got)
		case !tsprim.Declared(typ) && got != "tsgen: the analysis admitted a variable of the type "+typ.String():
			t.Errorf("tsPrimType(%s) panics %q, but the type is not declared", typ, got)
		}
	}
}
