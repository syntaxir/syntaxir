package print_test

import (
	"runtime"
	"slices"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestExpectationsOfTheLoweredTables checks that the expectation table numbered from the lowered
// Node and recognizer tables (execlower.Expectations) is the one that printing every rule of them
// numbers (the first pass of GenerateParser), across the corpus and the outputs that change which
// functions a table prints. The same expectations in the order of the Core module of the Node table
// (core.Module.Exps) must differ for some grammar, which shows that the comparison sees the order.
func TestExpectationsOfTheLoweredTables(t *testing.T) {
	modes := []struct {
		name string
		opts common.GenOptions
	}{
		{"plain", common.GenOptions{}},
		{"recognize", common.GenOptions{Recognize: true}},
		{"typed", common.GenOptions{Types: true, Recognize: true, LimitControls: true}},
		{"typed-only", common.GenOptions{Types: true, TypedOnly: true}},
		{"typed-only recognize", common.GenOptions{Types: true, TypedOnly: true, Recognize: true}},
		{"recognize-only", common.GenOptions{RecognizeOnly: true}},
		{"document", common.GenOptions{Document: true, LimitControls: true}},
		{"observability", common.GenOptions{Observability: true, Recognize: true}},
		{"without guards and scans", common.GenOptions{Types: true, NoOpt: []string{"guards", "scans"}}},
	}
	cases := append(codegentest.CorpusWithParsers(t), codegentest.CoreExtraGrammars...)
	var mu sync.Mutex
	compared, reordered := 0, 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, mode := range modes {
		for _, c := range cases {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				pg, err := parseTest(c.Src)
				if err != nil {
					return
				}
				var tables []*plandata.Table[code.Exec]
				var printed []string
				opts := mode.opts
				opts.Module = "example.com/p"
				h := &print.Hooks{
					Lowered: func(_ string, t *plandata.Table[code.Exec]) { tables = append(tables, t) },
					Reach:   func(l *print.LiveRules) { printed = l.Descs() },
				}
				if _, err := print.GenerateParser(pg, codegentest.Compile, codegentest.Passes(opts), codegentest.WithLib(opts), h); err != nil || printed == nil {
					return
				}
				got, err := execlower.Expectations(tables...)
				if err != nil {
					t.Errorf("%s: %s: %v", mode.name, c.Name, err)
					return
				}
				if !slices.Equal(got, printed) {
					t.Errorf("%s: %s: the lowered tables number\n%q\nbut printing them numbers\n%q", mode.name, c.Name, got, printed)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				compared++
				byCore := append([]string(nil), core.FixedDescs...)
				for _, d := range tables[0].IR.M.Exps {
					if !slices.Contains(byCore, d) {
						byCore = append(byCore, d)
					}
				}
				if !slices.Equal(byCore, printed) {
					reordered++
				}
			})
		}
	}
	wg.Wait()
	if compared < 500 {
		t.Fatalf("compared %d tables", compared)
	}
	if reordered == 0 {
		t.Fatal("the order of the Core module gives the printed table for every grammar")
	}
}
