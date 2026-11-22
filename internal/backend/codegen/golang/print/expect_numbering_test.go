package print_test

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// TestExpectationsOfTheLoweredTables checks that the expectation table numbered from the lowered
// Node and recognizer tables (execlower.NumberedExpectations), which the compilation numbers where it
// joins them and the printing receives (the hook Numbering), is the one that printing every rule of
// them numbers on first use, across the corpus and the outputs that change which functions a table
// prints. The printing numbers the table when the hook Numbering takes the numbering away, which the
// generation then reports as an expectation that the numbering lacks. The same expectations in the
// order of the Core module of the first numbered table (core.Module.Exps), which an observer of the
// compilation keeps, must differ for some grammar, which shows that the comparison sees the order.
func TestExpectationsOfTheLoweredTables(t *testing.T) {
	modes := []struct {
		name string
		opts common.GenOptions
	}{
		{"plain", common.GenOptions{NoLimitControls: true}},
		{"recognize", common.GenOptions{NoLimitControls: true, Recognize: true}},
		{"typed", common.GenOptions{Types: true, Recognize: true}},
		{"typed-only", common.GenOptions{NoLimitControls: true, Types: true, TypedOnly: true}},
		{"typed-only recognize", common.GenOptions{NoLimitControls: true, Types: true, TypedOnly: true, Recognize: true}},
		{"recognize-only", common.GenOptions{NoLimitControls: true, RecognizeOnly: true}},
		{"document", common.GenOptions{Document: true}},
		{"observability", common.GenOptions{NoLimitControls: true, Observability: true, Recognize: true}},
		{"without guards and scans", common.GenOptions{NoLimitControls: true, Types: true, NoOpt: []string{"guards", "scans"}}},
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
				var numbered, printed []string
				var cmu sync.Mutex
				cores := map[string]*core.Module{}
				keep := &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
					if m, ok := r.Unit.(*core.Module); ok && r.Err == nil {
						cmu.Lock()
						cores[e.Label.Table] = m
						cmu.Unlock()
					}
				}}
				opts := mode.opts
				opts.Module = "example.com/p"
				opts.KeepUnreachable = true
				h := &print.Hooks{
					Numbering: func(descs []string) []string { numbered = append([]string(nil), descs...); return nil },
					Printed:   func(descs []string) { printed = descs },
				}
				_, err = generateGo(pg, opts, driver.Options{Observers: []pipe.Observer{keep}}, h)
				if printed == nil {
					return
				}
				if len(printed) > len(core.FixedDescs) && (err == nil || !strings.Contains(err.Error(), "which the numbering of their lowered tables does not")) {
					t.Errorf("%s: %s: the printing numbers expectations that the numbering lacks, and the generation gives %v", mode.name, c.Name, err)
					return
				}
				got := append(slices.Clone(core.FixedDescs), numbered...)
				if !slices.Equal(got, printed) {
					t.Errorf("%s: %s: the lowered tables number\n%q\nbut printing them numbers\n%q", mode.name, c.Name, got, printed)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				compared++
				byCore := append([]string(nil), core.FixedDescs...)
				first := cores["rules"]
				if first == nil {
					first = cores["recRules"]
				}
				if first == nil {
					t.Errorf("%s: %s: the compilation built no Core module of the Node or the recognizer table", mode.name, c.Name)
					return
				}
				for _, d := range first.Exps {
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
