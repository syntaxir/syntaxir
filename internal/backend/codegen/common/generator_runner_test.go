package common_test

import (
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// The optimizations of a generator are the settings of its Runner, the only source of them (Passes only validates), so that
// the printers (Generator.Opt) and the passes that the Runner runs cannot disagree.
func TestGeneratorOptIsTheRunnerSettings(t *testing.T) {
	t.Parallel()
	p := codegentest.Passes(common.GenOptions{NoOpt: []string{"method-sharing"}})
	r := p.Runner(nil)
	g := &common.Generator{Runner: r}
	if g.Opt().On("method-sharing") || r.Settings().On("method-sharing") {
		t.Fatal("NoOpt method-sharing is on in the generator or in the runner")
	}
	if got, want := g.Opt().On("literal-sharing"), r.Settings().On("literal-sharing"); got != want || !got {
		t.Fatalf("literal-sharing: generator %v, runner %v, want both on", got, want)
	}
}

// A generator without a Runner is a programming error that fails with a message that says so.
func TestGeneratorWithoutRunnerPanicsClearly(t *testing.T) {
	t.Parallel()
	for name, use := range map[string]func(g *common.Generator){
		"Opt":        func(g *common.Generator) { g.Opt() },
		"Core":       func(g *common.Generator) { _, _ = g.Core("rules", nil) },
		"FlowPasses": func(g *common.Generator) { _, _ = g.FlowPasses(lowerdata.Unit{}) },
		"ExecPasses": func(g *common.Generator) { _, _ = g.ExecPasses(nil, common.TargetPipeline{}) },
		"LowerTable": func(g *common.Generator) { _, _ = g.LowerTable(&plandata.Plan{}, nil, common.TargetPipeline{}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				v := recover()
				if s, ok := v.(string); !ok || !strings.Contains(s, "no Runner") {
					t.Fatalf("panic %v, want a message about the missing Runner", v)
				}
			}()
			use(&common.Generator{})
		})
	}
}

// failingRunner is a Runner whose run of a table fails in a pass and returns the table with the
// failure, as the driver does.
type failingRunner struct {
	common.Runner
	table *plandata.Table[code.Exec]
}

var errPass = errors.New("internal error: a pass failed")

func (r failingRunner) LowerTable(string, plandata.Planned, bool, flowpass.Target, common.TargetPipeline) (*plandata.Table[code.Exec], error) {
	return r.table, errPass
}

// A failure of a pass in the run of a table is the first internal error of the generation (FlowErr),
// which fails it, and the table comes back for the printing to go on.
func TestLowerTableKeepsAPassFailure(t *testing.T) {
	t.Parallel()
	x, err := plandata.NewTable(nil, nil, code.At[code.Exec]{}.With(&code.Module{Name: "m", Stage: execlower.Stage}))
	if err != nil {
		t.Fatal(err)
	}
	g := &common.Generator{Runner: failingRunner{table: x}, Table: "rules", Flow: &common.FlowState{TableIR: &plandata.TableIR{}}}
	got, err := g.LowerTable(&plandata.Plan{}, nil, common.TargetPipeline{})
	if got != x || err != nil {
		t.Fatalf("LowerTable = %v, %v; want the table and no error", got, err)
	}
	if !errors.Is(g.FlowErr, errPass) || g.FlowErr.Error() != "module m of table rules: "+errPass.Error() {
		t.Errorf("FlowErr = %v, want the failure of the pass", g.FlowErr)
	}
}
