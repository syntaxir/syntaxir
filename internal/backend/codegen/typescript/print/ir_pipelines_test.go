package print_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// TestEveryFlowFunctionRunsTheIRPipelines generates TypeScript parsers whose functions take every route
// from Flow IR (flat and framed rules, lines of an Operators Rule, outlined nodes) in the Node and
// recognizer tables, and checks from the dumps of "flowlower" and "execlower" that every function printed from
// Exec IR went through the Flow pipeline and the Exec pipeline once per lowering.
func TestEveryFlowFunctionRunsTheIRPipelines(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "grammars", "cel", "cel.sxir"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseTest(string(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		opts common.GenOptions
	}{
		{"ordinary", common.GenOptions{NoLimitControls: true, Recognize: true}},
		{"types", common.GenOptions{NoLimitControls: true, Recognize: true, Types: true}},
		{"controls+observability", common.GenOptions{Recognize: true, Observability: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var dump bytes.Buffer
			o := c.opts
			o.Debug = common.Debug{PrintAfter: []string{"flowlower", "execlower"}, VerifyEach: true, Output: &dump}
			if _, err := generateTS(g, o, driver.Options{}, nil); err != nil {
				t.Fatal(err)
			}
			codegentest.CheckIRPipelines(t, dump.String(), "rules", "recRules")
		})
	}
}

// A Flow pass and an Exec pass that break the functions of one table are caught by the verifiers of
// Flow IR and Exec IR in each table: at the end of the run without VerifyEachPass, and by name right
// after the pass with it.
func TestVerifyEachPassNamesTheBreakingFlowAndExecPass(t *testing.T) {
	pg, err := parseTest(`rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"rules", "recRules"} {
		for _, ir := range []string{"flow", "exec"} {
			t.Run(table+"/"+ir, func(t *testing.T) {
				x := driver.Options{Edits: []pipe.Edit{codegentest.BrokenFlow(table)}}
				if ir == "exec" {
					x.Edits = []pipe.Edit{codegentest.BrokenExec(table)}
				}
				// The broken pass runs on the functions of the table.
				seen := 0
				x.Observers = []pipe.Observer{&pipetest.Watch{Fn: func(e pipe.Event, _ *pipe.Result) {
					if e.Step.Name == "break-"+ir && e.Label.Table == table {
						seen++
					}
				}}}
				opts := common.GenOptions{Recognize: true}
				_, err := generateTS(pg, opts, x, nil)
				if err == nil || !strings.Contains(err.Error(), "["+table+"/") || !strings.Contains(err.Error(), "internal error: verify code@"+ir+" [") || strings.Contains(err.Error(), "break-"+ir) {
					t.Fatalf("without VerifyEachPass: %v", err)
				}
				if seen == 0 {
					t.Fatalf("no function of the %s table ran through the %s pipeline", table, ir)
				}
				opts.Debug.VerifyEach = true
				_, err = generateTS(pg, opts, x, nil)
				if err == nil || !strings.Contains(err.Error(), "["+table+"/") || !strings.Contains(err.Error(), "internal error: break-"+ir+" [") {
					t.Fatalf("with VerifyEachPass: %v", err)
				}
			})
		}
	}
}
