package print_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// hasRecovery reports a recovery in e, read from the expression itself.
func hasRecovery(e ast.Expr) (found bool) {
	astinspect.WalkExpr(e, func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Recovery:
			found = true
		case *ast.Attributed:
			for _, a := range e.Attrs {
				found = found || a.Name == "recover"
			}
		}
	})
	return
}

// TestFlowRecoveryPrintsFromFlow checks the route of the rules with recovery: over the corpus and the ten
// grammars, in the ordinary and the budget-controlled forms, every rule, Operators Rule line and stream start rule of the
// Node and recognizer tables is printed from Flow IR (otherwise the generation fails), and the rules with a recovery (alone, with a cut and
// with a leader) take the Flow route.
// recoveryBothForms are the cases that run in both forms in the default tier: the recovery of a leader
// takes the Flow route only in the case typed/LR_recovery, and the form without the budget controls is
// the only one where it does, so a split that gave that case to the other form would fail the check.
var recoveryBothForms = []string{"typed/LR_recovery"}

func TestFlowRecoveryPrintsFromFlow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(append(codegentest.Representative(codegentest.GenCorpus(t)), codegentest.RepresentativeGrammars(codegentest.GrammarCases(t))...), observeShapes...), streamCorpus...)
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
				if !hasRecovery(r.BodyExpr()) {
					return
				}
				taken[table+" recovery"]++
				if hasCut(r.BodyExpr()) {
					taken[table+" recovery with a cut"]++
				}
				if r.Leader {
					taken[table+" recovery with a leader"]++
				}
			}
			gh.OperatorsRoute = func(table string, r *common.Rule) {
				mu.Lock()
				defer mu.Unlock()
				taken[table+" operators line"]++
			}
			codegentest.EachParallel(t, codegentest.ControlsCases(t, cases, controls, recoveryBothForms...), func(t *testing.T, c codegentest.Case) {
				g, err := parseTest(c.Src)
				if err != nil {
					return
				}
				for _, stream := range []bool{false, true} {
					if _, err := codegentest.Generate(t, g, common.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Lib: common.LibImport, Recognize: true, Stream: stream, NoLimitControls: !controls}); err != nil && (!stream || strings.Contains(err.Error(), "the printer does not print")) {
						t.Fatalf("%s: %v", c.Name, err)
					}
				}
			})
			for _, table := range []string{"rules", "recRules"} {
				for _, k := range []string{" recovery", " recovery with a cut", " recovery with a leader", " operators line"} {
					if taken[table+k] == 0 {
						t.Errorf("no %s%s takes the Flow route (%v)", table, k, taken)
					}
				}
			}
		})
	}
}
