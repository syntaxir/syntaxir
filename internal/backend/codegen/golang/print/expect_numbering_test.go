package print_test

import (
	"runtime"
	"slices"
	"strings"
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
// Node and recognizer tables (execlower.Expectations), which GenerateParser numbers before it prints,
// is the one that printing every rule of them numbers on first use, across the corpus and the outputs
// that change which functions a table prints. The printing numbers the table when the hook Numbering
// takes the numbering away, which the generation then reports as an expectation that the numbering
// lacks. The same expectations in the order of the Core module of the Node table (core.Module.Exps)
// must differ for some grammar, which shows that the comparison sees the order.
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
				opts.KeepUnreachable = true
				h := &print.Hooks{
					Lowered:   func(_ string, t *plandata.Table[code.Exec]) { tables = append(tables, t) },
					Numbering: func([]string) []string { return nil },
					Printed:   func(descs []string) { printed = descs },
				}
				_, err = print.GenerateParser(pg, codegentest.Compile, codegentest.Passes(opts), codegentest.WithLib(opts), h)
				if printed == nil {
					return
				}
				if len(printed) > len(core.FixedDescs) && (err == nil || !strings.Contains(err.Error(), "which the numbering of their lowered tables does not")) {
					t.Errorf("%s: %s: the printing numbers expectations that the numbering lacks, and the generation gives %v", mode.name, c.Name, err)
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
