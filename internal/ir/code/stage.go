package code

import (
	"fmt"
	"io"

	"zeroparse.dev/zp/internal/pass/pipe"
)

// Modules at a stage as units of the pipeline (design record 074, Units and Kinds).
//
// The stage of a module is part of the Go type of a unit: At[Flow] and At[Exec] are two kinds, so a
// composition of passes that skips the conversion between the stages does not compile. The run-time
// stage of the module (Module.Stage) stays, since the dumps and the verifier read it; With keeps a
// unit at its stage and panics on a module of another one, so no pass moves a module between stages.

// Stage is a stage of a Code IR module; its values are marker types.
type Stage interface {
	stage() string
}

// Flow is the Flow stage: the module of a table as flowlower makes it.
type Flow struct{}

// Exec is the Exec stage: the module of a table as execlower makes it.
type Exec struct{}

func (Flow) stage() string { return "flow" }
func (Exec) stage() string { return "exec" }

// stageName returns the run-time name of the stage S (Module.Stage).
func stageName[S Stage]() string {
	var s S
	return s.stage()
}

// At is a module at the stage S. Only EnterFlow and EnterExec make one at a stage; a pass keeps the
// stage with With.
type At[S Stage] struct{ m *Module }

// Module returns the module of the unit.
func (a At[S]) Module() *Module { return a.m }

// With returns the unit of the module m at the stage of a. It panics unless m is at that stage: a
// pass cannot move a module between stages.
func (a At[S]) With(m *Module) At[S] {
	if want := stageName[S](); m == nil || m.Stage != want {
		panic(fmt.Sprintf("code: With of a module at the %q stage as a unit at the %q stage", stageOf(m), want))
	}
	return At[S]{m}
}

func stageOf(m *Module) string {
	if m == nil {
		return "<nil>"
	}
	return m.Stage
}

// enter sets the stage of m to S and returns the unit. m must be at no stage or at S already.
func enter[S Stage](m *Module) At[S] {
	want := stageName[S]()
	if m.Stage != "" && m.Stage != want {
		panic(fmt.Sprintf("code: a module at the %q stage cannot enter the %q stage", m.Stage, want))
	}
	m.Stage = want
	return At[S]{m}
}

// EnterFlow makes m a unit at the Flow stage. Only flowlower calls it.
func EnterFlow(m *Module) At[Flow] { return enter[Flow](m) }

// EnterExec makes m a unit at the Exec stage. Only execlower calls it.
func EnterExec(m *Module) At[Exec] { return enter[Exec](m) }

// stageChecks holds, by stage, the invariants of a module at the stage beyond Verify, which the
// dialect of the stage registers (RegisterStageCheck).
var stageChecks = map[string]func(*Module) error{}

// RegisterStageCheck makes check part of the verifier of the kind of the stage S, after Verify. The
// dialect package of the stage calls it once, from its init function; a second call panics.
func RegisterStageCheck[S Stage](check func(*Module) error) {
	name := stageName[S]()
	if stageChecks[name] != nil {
		panic(fmt.Sprintf("code: the check of the %s stage is registered twice", name))
	}
	stageChecks[name] = check
}

// stageIR declares the kind of a module at the stage S: verified by Verify, the stage of the module
// and the stage's own check, printed as the listing of each function, and copied by Clone.
func stageIR[S Stage](name string) *pipe.IR[At[S]] {
	stage := stageName[S]()
	return pipe.RegisterIR(&pipe.IR[At[S]]{
		Name: name,
		Verify: func(a At[S]) error {
			m := a.m
			if m.Stage != stage {
				return fmt.Errorf("the module %s is at the %q stage, not the %q stage", m.Name, m.Stage, stage)
			}
			if err := Verify(m); err != nil {
				return err
			}
			if check := stageChecks[stage]; check != nil {
				return check(m)
			}
			return nil
		},
		Print: func(w io.Writer, a At[S]) { PrintFuncs(w, a.m) },
		Copy:  func(a At[S]) At[S] { return At[S]{a.m.Clone()} },
	})
}

// FlowStage and ExecStage are the unit kinds of a module at the Flow and the Exec stage.
var (
	FlowStage = stageIR[Flow]("code@flow")
	ExecStage = stageIR[Exec]("code@exec")
)

// stageFuncs declares the view of a module at the stage S as its functions, each a module at the
// stage that declares what the module declares and holds that function alone (FuncUnit), for the
// passes that run on one function at a time (pipe.PerFunc).
func stageFuncs[S Stage](name string, k *pipe.IR[At[S]]) *pipe.Funcs[At[S], At[S]] {
	return pipe.RegisterFuncs(k, k, &pipe.Funcs[At[S], At[S]]{
		Name: name,
		Len:  func(a At[S]) int { return len(a.m.Funcs) },
		Func: func(a At[S], i int) string { return a.m.Funcs[i].Name },
		Get:  func(a At[S], i int) At[S] { return At[S]{a.m.FuncUnit(i)} },
		Put: func(a At[S], i int, u At[S]) (At[S], error) {
			return a, a.m.PutFuncUnit(i, u.m)
		},
	})
}

// FlowFuncs and ExecFuncs view a module at the Flow and the Exec stage as its functions.
var (
	FlowFuncs = stageFuncs("funcs@flow", FlowStage)
	ExecFuncs = stageFuncs("funcs@exec", ExecStage)
)
