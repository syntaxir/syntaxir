package codegentest

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// irDumpHeader matches the header of a dump of the module of a table after the lowering that made it
// (GenOptions.Debug.PrintAfter "flowlower" and "execlower"); the listing of each function of the module
// follows it.
var irDumpHeader = regexp.MustCompile(`^-- after (\S+) \[(\S+)\] --$`)

// CheckIRPipelines checks the dumps that a generation with Debug.PrintAfter "flowlower" and "execlower" wrote: every function
// printed from Exec IR was lowered from a Flow IR function, so each function of each table must have
// as many Flow IR dumps as Exec IR dumps, and each table in tables must have some. A function whose
// Flow passes or Exec passes the generator skipped has no dump of that IR. It returns the number of
// Flow IR dumps of each table.
func CheckIRPipelines(t *testing.T, dump string, tables ...string) map[string]int {
	t.Helper()
	type key struct{ table, fn string }
	flows, execs := map[key]int{}, map[key]int{}
	perTable := map[string]int{}
	var count map[key]int // the counts of the dump that the lines belong to; nil outside one
	table, flowDump := "", false
	for _, line := range strings.Split(dump, "\n") {
		if m := irDumpHeader.FindStringSubmatch(line); m != nil {
			count, table, flowDump = nil, m[2], m[1] == flowlower.Name
			switch m[1] {
			case flowlower.Name:
				count = flows
			case execlower.Name:
				count = execs
			}
			continue
		}
		name, ok := strings.CutPrefix(line, "func ")
		if !ok || count == nil {
			continue
		}
		k := key{table, name[:strings.IndexByte(name, '(')]}
		count[k]++
		if flowDump {
			perTable[table]++
		}
	}
	for k, n := range flows {
		if execs[k] != n {
			t.Errorf("function %s of the %s table: %d Flow IR dumps, %d Exec IR dumps", k.fn, k.table, n, execs[k])
		}
	}
	for k, n := range execs {
		if _, ok := flows[k]; !ok {
			t.Errorf("function %s of the %s table: no Flow IR dump, %d Exec IR dumps", k.fn, k.table, n)
		}
	}
	for _, table := range tables {
		if perTable[table] == 0 {
			t.Errorf("no function of the %s table went through the Flow and Exec pipelines", table)
		}
	}
	return perTable
}

// BrokenFlowPass is a Flow pass that lists a variable twice, which flow.VerifyModule rejects. It is no
// registered optimization: it declares that it is a test pass.
var BrokenFlowPass = pipe.New(pipe.Def[flowpass.Unit, flowpass.Unit]{
	Decl: pipe.Decl{Name: "break-flow", Test: true, Obligation: "none: it breaks the function on purpose"},
	In:   code.FlowStage, Out: code.FlowStage,
	Run: func(_ *pipe.Ctx[flowpass.Unit], u flowpass.Unit) (flowpass.Unit, pass.PreservedSet, error) {
		a := flow.AttrsOf(flow.FuncOf(u.Module()))
		a.Vars = append(a.Vars, "broken", "broken")
		return u, pass.PreserveNone(), nil
	},
})

// BrokenExecPass is an Exec pass that declares the function of its unit twice, which code.Verify
// rejects. It is no registered optimization: it declares that it is a test pass.
var BrokenExecPass = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
	Decl: pipe.Decl{Name: "break-exec", Test: true, Obligation: "none: it breaks the module on purpose"},
	In:   code.ExecStage, Out: code.ExecStage,
	Run: func(_ *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
		m := u.Module()
		m.Funcs = append(m.Funcs, m.Funcs[0])
		return u, pass.PreserveNone(), nil
	},
})

// BrokenFlow returns the edit of a compilation (driver.Options.Edits) that runs the Flow passes with
// BrokenFlowPass added at their end on the functions of the table labeled table, and the Flow passes
// as they are on the others.
func BrokenFlow(table string) pipe.Edit {
	return pipe.Fill(driver.FlowSlot, table, pipe.Seq(code.FlowStage, append(slices.Clone(flowpass.Passes), BrokenFlowPass)...))
}

// BrokenExec is BrokenFlow for the Exec passes, with BrokenExecPass.
func BrokenExec(table string) pipe.Edit {
	return pipe.Fill(driver.ExecSlot, table, pipe.Seq(code.ExecStage, append(slices.Clone(codepass.Passes), BrokenExecPass)...))
}

// recordFeature is the function that RecordFlowPass calls, which RecordingFlow provides.
var recordFeature = pipe.NewFeature("test-flow-record", func(func(pipe.Label, *code.Module)) string { return "record" }, nil)

// RecordFlowPass is a Flow pass of the tests that calls the function of RecordingFlow with the label
// and the module of each function it runs on and changes nothing. It is no registered optimization:
// it declares that it is a test pass.
var RecordFlowPass = pipe.New(pipe.Def[flowpass.Unit, flowpass.Unit]{
	Decl: pipe.Decl{Name: "test-flow-record", Test: true, Obligation: "none: it records the function and changes nothing",
		Features: []pipe.FeatureID{recordFeature.ID()}},
	In: code.FlowStage, Out: code.FlowStage,
	Run: func(c *pipe.Ctx[flowpass.Unit], u flowpass.Unit) (flowpass.Unit, pass.PreservedSet, error) {
		pipe.Feature(c, recordFeature)(c.Label(), u.Module())
		return u, pass.PreserveAll(), nil
	},
})

// RecordingFlow returns the Flow pipeline with RecordFlowPass before the passes (first) or after them,
// on each function of a module, which calls record with the label and the module of each function it
// runs on. After the passes, it runs on a function only when the passes before it succeeded, before
// flow-finish ends the function.
func RecordingFlow(first bool, record func(l pipe.Label, m *code.Module)) pipe.Pipeline[flowpass.Unit, flowpass.Unit] {
	return pipe.WithConst(recordFeature, record, flowpass.OnFuncs(recordingPasses(first)...))
}

// recordingPasses returns the Flow passes with RecordFlowPass before them (first) or after them.
func recordingPasses(first bool) []*pipe.Pass[flowpass.Unit, flowpass.Unit] {
	if first {
		return append([]*pipe.Pass[flowpass.Unit, flowpass.Unit]{RecordFlowPass}, flowpass.Passes...)
	}
	return append(slices.Clone(flowpass.Passes), RecordFlowPass)
}

// FlowPasses returns an observer of a compilation (driver.Options.Observers) that calls record with
// the label and the module of the function after each Flow pass that runs on a function of a table.
// The Flow passes run on each function in their order and flow-finish ends it after them, so the last
// call for a function sees it after every pass that ran on it, with the forms that they replaced.
func FlowPasses(record func(l pipe.Label, m *code.Module)) pipe.Observer {
	return &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if u, ok := r.Unit.(flowpass.Unit); ok && r.Err == nil && e.Label.Func != "" && e.Step.In == code.FlowStage.Name {
			record(e.Label, u.Module())
		}
	}}
}
