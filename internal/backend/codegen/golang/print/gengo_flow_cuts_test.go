package print_test

import (
	"fmt"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// hasCut reports a cut in e.
func hasCut(e ast.Expr) (found bool) {
	astinspect.WalkExpr(e, func(e ast.Expr) {
		if _, ok := e.(*ast.Cut); ok {
			found = true
		}
	})
	return
}

// TestFlowCutsPrintFromFlow checks the route of the rules with cuts: over the corpus and the ten grammars, in
// the ordinary and the budget-controlled forms, a rule with a cut takes
// the Flow route in the Node and recognizer tables (a rule the printer does not print fails the generation).
func TestFlowCutsPrintFromFlow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(codegentest.Representative(codegentest.GenCorpus(t)), codegentest.RepresentativeGrammars(codegentest.GrammarCases(t))...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			t.Parallel()
			taken := map[string]int{}
			var mu sync.Mutex
			gh := &print.Hooks{}
			codegentest.SetGenHooks(t, gh)
			gh.FramedRoute = func(table string, r *common.Rule) {
				mu.Lock()
				defer mu.Unlock()
				if hasCut(r.BodyExpr()) {
					taken[table]++
				}
			}
			codegentest.EachParallel(t, codegentest.ControlsCases(t, cases, controls), func(t *testing.T, c codegentest.Case) {
				g, err := parseTest(c.Src)
				if err != nil {
					return
				}
				if _, err := codegentest.Generate(t, g, common.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Lib: common.LibImport, Recognize: true, NoLimitControls: !controls}); err != nil {
					t.Fatalf("%s: %v", c.Name, err)
				}
			})
			for _, table := range []string{"rules", "recRules"} {
				if taken[table] == 0 {
					t.Errorf("no rule with a cut takes the Flow route in the %s table (%v)", table, taken)
				}
			}
		})
	}
}
