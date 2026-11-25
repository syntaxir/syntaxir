package print_test

import (
	goast "go/ast"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A generated parser has one form of its rule bodies, and no body starts with a branch that chooses
// between forms at run time. With limit controls its bodies count compute units and neither charge
// steps nor read a budget control, in a Document too; without limit controls no body charges, counts
// or mentions a control. A Document calls no helper
// that has an Ordinary form (NewNodeOrdinary, NextOrdinary, ...), which belongs to the forms outside
// Documents.
func TestGeneratedRuleBodiesHaveOneForm(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedRuleBodiesHaveOneForm)
}

const oneFormGrammar = `type Text = { Text string }
rule main Text = (a:("a"* #atomic) "b") #recover(skip="?") => Text{Text: text($a)}
rule recursive Text = t:(recursive "+" "a" / "a") #atomic => Text{Text: text($t)}
rule expr Text = #operators {
 operand main
 level sum { infix left "+" => $lhs }
}`

// oneFormProblems checks the rule bodies of the generated source src against the form that
// controls (limit controls) and document select, and reports the bodies it found, the bodies that
// charge steps or count units, and what contradicts the form.
func oneFormProblems(t *testing.T, src []byte, controls, document bool) (problems []string, bodies, charging int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "generated.go", src, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	logical := controls
	for _, decl := range file.Decls {
		fn, ok := decl.(*goast.FuncDecl)
		if !ok || fn.Body == nil || len(fn.Body.List) == 0 || fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
			continue
		}
		if _, isBody := parserFunctionKey(fn); !isBody {
			continue
		}
		bodies++
		if guard, isIf := fn.Body.List[0].(*goast.IfStmt); isIf && guard.Else == nil {
			var cond strings.Builder
			if err := printer.Fprint(&cond, fset, guard.Cond); err != nil {
				t.Fatal(err)
			}
			switch cond.String() {
			case "p.Control == nil", "p.Document == nil", "p.Control == nil && p.Document == nil":
				problems = append(problems, fn.Name.Name+" starts with the branch "+cond.String()+" between forms")
			}
		}
		charges, counts, controlRead, ordinaryCall := false, false, false, ""
		goast.Inspect(fn.Body, func(n goast.Node) bool {
			if sel, ok := n.(*goast.SelectorExpr); ok {
				charges = charges || sel.Sel.Name == "Spend"
				counts = counts || sel.Sel.Name == "Fuel"
				controlRead = controlRead || sel.Sel.Name == "Control"
				if strings.HasSuffix(sel.Sel.Name, "Ordinary") {
					ordinaryCall = sel.Sel.Name
				}
			}
			return true
		})
		if logical && counts || !logical && charges {
			charging++
		}
		switch {
		case !controls && (charges || counts || controlRead):
			problems = append(problems, fn.Name.Name+" charges, counts or tests a control without limit controls")
		case logical && (charges || controlRead):
			problems = append(problems, fn.Name.Name+" charges or tests a budget control with compute units")
		}
		if document && ordinaryCall != "" {
			problems = append(problems, fn.Name.Name+" calls the ordinary helper "+ordinaryCall+" in a Document form")
		}
	}
	switch {
	case logical && charging == 0:
		problems = append(problems, "no rule body counts compute units with limit controls")
	}
	return problems, bodies, charging
}

func testGeneratedRuleBodiesHaveOneForm(t *testing.T) {
	g, err := parseTest(oneFormGrammar)
	if err != nil {
		t.Fatal(err)
	}
	for _, types := range []bool{false, true} {
		for _, document := range []bool{false, true} {
			opts := common.GenOptions{Package: "ordinaryfixture", Types: types, Document: document, Recognize: true, Start: "main"}
			src, err := codegentest.GenerateCode(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			problems, bodies, charging := oneFormProblems(t, src, codegentest.FixtureBudgetControls(t), document)
			for _, p := range problems {
				t.Errorf("types=%v document=%v: %s", types, document, p)
			}
			if bodies == 0 {
				t.Fatalf("types=%v document=%v: no rule bodies found", types, document)
			}
			t.Logf("types=%v document=%v: %d rule bodies, %d charging or counting", types, document, bodies, charging)
		}
	}
}

// TestGeneratedRuleBodiesHaveOneFormNegativeControls checks that the check notices the other form:
// the ordinary form judged as controlled, the controlled form as ordinary, forms with compute units
// that charge a step, outside a Document and in one, and Document forms that call an ordinary helper.
func TestGeneratedRuleBodiesHaveOneFormNegativeControls(t *testing.T) {
	t.Parallel()
	g, err := parseTest(oneFormGrammar)
	if err != nil {
		t.Fatal(err)
	}
	gen := func(controls, document bool) []byte {
		files, err := codegentest.Generate(t, g, common.GenOptions{Package: "ordinaryfixture", Module: "example.com/generated", Lib: common.LibImport, Types: true, Document: document, Recognize: true, Start: "main", NoLimitControls: !controls})
		if err != nil {
			t.Fatal(err)
		}
		return codegentest.MergeGenerated(files)
	}
	plain, controlled, doc, controlledDoc := gen(false, false), gen(true, false), gen(false, true), gen(true, true)
	withBody := func(src []byte, body string) []byte {
		return append(src[:len(src):len(src)], []byte("\nfunc badBody(p *Parser) (*Node, bool) { "+body+" }\n")...)
	}
	for _, c := range []struct {
		name               string
		src                []byte
		controls, document bool
		want               string
	}{
		{"the ordinary form as controlled", plain, true, false, "no rule body counts compute units"},
		{"the controlled form as ordinary", controlled, false, false, "charges, counts or tests a control"},
		{"the controlled form with a step charge", withBody(controlled, "p.Spend(); return nil, true"), true, false, "charges or tests a budget control"},
		{"the controlled Document form with a step charge", withBody(controlledDoc, "p.Spend(); return nil, true"), true, true, "charges or tests a budget control"},
		{"the plain form as a Document form", plain, false, true, "calls the ordinary helper"},
		{"the controlled Document form with an Ordinary helper", withBody(controlledDoc, `return p.NewNodeOrdinary("x", 0, nil), true`), true, true, "calls the ordinary helper"},
	} {
		problems, _, _ := oneFormProblems(t, c.src, c.controls, c.document)
		found := false
		for _, p := range problems {
			found = found || strings.Contains(p, c.want)
		}
		if !found {
			t.Errorf("%s: problems %q lack %q", c.name, problems, c.want)
		}
	}
	if problems, _, _ := oneFormProblems(t, doc, false, true); len(problems) != 0 {
		t.Errorf("the Document form: %q", problems)
	}
	if problems, _, _ := oneFormProblems(t, controlledDoc, true, true); len(problems) != 0 {
		t.Errorf("the Document form with limit controls: %q", problems)
	}
}

// parserFunction identifies a generated execution function by the parser type of its first
// parameter, which is named p.
type parserFunction struct{ receiver, name string }

// parserFunctionKey recognizes a rule body or other generated function whose first parameter
// is the parser p, a *Parser or a *TParser.
func parserFunctionKey(fn *goast.FuncDecl) (parserFunction, bool) {
	if fn.Recv != nil || fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return parserFunction{}, false
	}
	first := fn.Type.Params.List[0]
	ptr, ok := first.Type.(*goast.StarExpr)
	if !ok {
		return parserFunction{}, false
	}
	// The finished source names the library's types with its package, the execution source
	// without.
	var name string
	switch x := ptr.X.(type) {
	case *goast.Ident:
		name = x.Name
	case *goast.SelectorExpr:
		name = x.Sel.Name
	}
	if (name != "Parser" && name != "TParser") || len(first.Names) != 1 || first.Names[0].Name != "p" {
		return parserFunction{}, false
	}
	return parserFunction{name, fn.Name.Name}, true
}
