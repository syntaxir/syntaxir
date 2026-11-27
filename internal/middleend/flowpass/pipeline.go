// Package flowpass holds the passes of the Flow dialect of Code IR: the control-level decisions that
// choose the form of lowered grammar code (design 063).
package flowpass

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The passes of Flow IR as a pipeline (design 063): transformations of a lowered function that keep
// its behavior and that every backend printing from Flow IR then prints. A pass has the name of the
// flag that turns it off, and that name is distinct from the names of the Core passes, so that one
// name selects one pass in the flags and in the dumps. The unit is a module at the Flow stage that
// holds one function (flow.FuncOf): a rule, a framed body, a line of an Operators Rule or an outlined node,
// as the lowering made it.

// Unit is the unit of the Flow passes: a module at the Flow stage. A pass runs on the unit of one
// function (code.FlowFuncs).
type Unit = code.At[code.Flow]

// ObserveFeature reports that the parser reports the rules it enters and the alternatives that
// guards skip, which a pass must keep.
var ObserveFeature = pipe.NewFeature("observe", strconv.FormatBool, strconv.ParseBool)

// TargetFeature is what the backend that prints the functions can print (Target).
var TargetFeature = pipe.NewFeature("flow-target", Target.String, ParseTarget)

// FuncAttrs gives the attributes of the function of a unit of one function (flow.Attrs), which record
// how it was lowered (Attrs.Node, Document, Traced, Frame) and so where a pass applies; nil
// for a function without them. It reads them from the function, and the passes that change the
// function keep them current, as the function's own content.
var FuncAttrs = pass.NewAnalysisKey("flow-func-attrs", func(u Unit, _ *pass.AnalysisManager[Unit]) (*flow.Attrs, error) {
	return flow.AttrsOf(flow.FuncOf(u.Module())), nil
})

// Target is what the backend that prints the function can print, beyond the forms that every
// backend prints. A pass that makes a form applies only where the target prints it.
type Target struct {
	// Terminals: skipTerminal, a terminal region matched as one program, in a local function of
	// typed rules printed in the ordinary form.
	Terminals bool
	// ClassRuns: skipClasses, consecutive classes matched as one, in a function of typed rules printed
	// in the ordinary form.
	ClassRuns bool
	// ClassTables: matchClass, a class matched through its membership table, in a function of typed
	// rules printed in the ordinary form.
	ClassTables bool
	// GuardedClasses: a class that consumes the character a guard read, with the size the guard's
	// peek kept, in a function printed in the ordinary form.
	GuardedClasses bool
	// InlineLiterals: hasLit, a literal compared in place before matchLit.
	InlineLiterals bool
}

// Bytecode is the target of the bytecode VM, which runs the Flow functions of the Node table with an
// instruction for each Flow operation (spec/bytecode/README.md, Flow Bodies). It prints none of the
// forms of the passes, so the VM runs the operations that the lowering wrote; it charges what a
// target that prints the forms charges, since each form keeps the charges of what it replaces:
//   - Terminals, ClassRuns and ClassTables apply to typed rules only, which the VM does not run.
//   - GuardedClasses applies to functions without budget controls only; the VM's Node table has
//     them.
//   - InlineLiterals: no instruction compares a literal in place. hasLit charges what matchLit
//     charges for the match it reads and nothing for a miss, after which matchLit runs.
var Bytecode = Target{Terminals: false, ClassRuns: false, ClassTables: false, GuardedClasses: false, InlineLiterals: false}

// String returns the forms that t prints, by the names of the passes that make them, for the text
// form of the feature.
func (t Target) String() string {
	var on []string
	for _, f := range []struct {
		on   bool
		name string
	}{{t.Terminals, PassTerminals}, {t.ClassRuns, PassClassRuns}, {t.ClassTables, PassClassTables},
		{t.GuardedClasses, PassGuardedClasses}, {t.InlineLiterals, PassInlineLiterals}} {
		if f.on {
			on = append(on, f.name)
		}
	}
	return strings.Join(on, ",")
}

// ParseTarget reads the text form of a target that String writes: the names of the passes whose forms
// it prints, separated by commas. An empty text is the target that prints none of them.
func ParseTarget(s string) (Target, error) {
	var t Target
	if s == "" {
		return t, nil
	}
	for _, name := range strings.Split(s, ",") {
		switch name {
		case PassTerminals:
			t.Terminals = true
		case PassClassRuns:
			t.ClassRuns = true
		case PassClassTables:
			t.ClassTables = true
		case PassGuardedClasses:
			t.GuardedClasses = true
		case PassInlineLiterals:
			t.InlineLiterals = true
		default:
			return Target{}, fmt.Errorf("flow target: %q names no form", name)
		}
	}
	return t, nil
}

// Pipeline is the pipeline of the Flow passes on a module, each on every function of the module in
// turn (OnFuncs), in the order they run: each chooses the form of a construct that the lowering wrote
// in its one form (flow.Forms), where the target prints the form and the function allows it.
func Pipeline() pipe.Pipeline[Unit, Unit] { return OnFuncs(Passes...) }

// OnFuncs returns the pipeline that runs the passes ps on each function of a module, pass by pass
// (pipe.PerFunc on code.FlowFuncs).
func OnFuncs(ps ...*pipe.Pass[Unit, Unit]) pipe.Pipeline[Unit, Unit] {
	return pipe.PerFunc(code.FlowFuncs, pipe.Seq(code.FlowStage, ps...))
}

// Names returns the names of the Flow passes in the order they run.
func Names() []string { return names(Passes) }

// FormNames returns the names of the passes of forms in the order they run.
func FormNames() []string { return names(FormPasses) }

func names(ps []*pipe.Pass[Unit, Unit]) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name()
	}
	return out
}

// entry is the name of the lowering that makes a Flow module, flowlower, under which a run dumps the
// module as it reaches the first pass. The lowering's package tests these passes, so this package
// cannot import it.
const entry = "flowlower"

// RunOptions selects what a run of the Flow passes does besides running them.
type RunOptions struct {
	// Settings turns the passes on and off by their flags and gives the numbers of those that take
	// one; the zero value runs every pass with its default.
	Settings pass.Settings
	// Observe and Target are the features of the run (ObserveFeature, TargetFeature).
	Observe bool
	Target  Target
	// VerifyEach verifies the module after every pass.
	VerifyEach bool
	// Dump, if not nil, is called with "flowlower" before the first pass and with the name of each pass
	// that ran and the module of its function, after it.
	Dump func(pass string, m *code.Module)
}

// Run runs the enabled passes of Pipeline that apply on each function of the module m, which the
// lowering made and verified, pass by pass, and verifies each function again if a pass ran on it. It
// returns the module after them and the names of the passes that ran, once for each function. It
// consumes m: the passes change it in place, and a failure drops it (pipe.Run.Stop). The caller then
// calls Finish.
func Run(m *code.Module, o RunOptions) (*code.Module, []string, error) {
	if o.Dump != nil {
		o.Dump(entry, m)
	}
	ob := &ranObserver{dump: o.Dump}
	p := pipe.WithConst(ObserveFeature, o.Observe, pipe.WithConst(TargetFeature, o.Target, Pipeline()))
	r := &pipe.Run{Settings: o.Settings, VerifyEach: o.VerifyEach, Stop: true, Observers: []pipe.Observer{ob}}
	out, err := pipe.Execute(context.Background(), r, p, code.At[code.Flow]{}.With(m))
	if err != nil {
		return nil, ob.ran, err
	}
	return out.Module(), ob.ran, nil
}

// ranObserver records the passes that ran, and dumps the unit after each of them. The decisions
// that a pass asks for (pipe.Ctx.Allow) are not passes: they are left out.
type ranObserver struct {
	dump func(pass string, m *code.Module)
	ran  []string
}

func (o *ranObserver) Needs() pipe.Needs                       { return pipe.Needs{Units: o.dump != nil} }
func (o *ranObserver) Before(pipe.Event, func(io.Writer)) bool { return true }

func (o *ranObserver) After(e pipe.Event, r *pipe.Result) {
	if !r.Ran || r.Err != nil || e.Step.Pattern {
		return
	}
	o.ran = append(o.ran, e.Step.Instance)
	if o.dump != nil {
		o.dump(e.Step.Instance, r.Unit.(Unit).Module())
	}
}
