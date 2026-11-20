package code

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestWithChecksTheStage(t *testing.T) {
	m := &Module{Name: "m"}
	u := EnterFlow(m)
	if got := u.With(m).Module(); got != m {
		t.Errorf("With of a module at the Flow stage returned %p, want %p", got, m)
	}
	x := &Module{Name: "x"}
	EnterExec(x)
	for name, f := range map[string]func(){
		"a module at another stage": func() { u.With(x) },
		"a module at no stage":      func() { u.With(&Module{Name: "y"}) },
		"no module":                 func() { u.With(nil) },
		"entering another stage":    func() { EnterExec(m) },
		"entering its stage again":  func() { EnterFlow(m) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}

func TestNoLoweringDoesNotCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	cmd := exec.Command("go", "build", "./testdata/nolowering")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("a pipeline of Exec passes after Flow passes without a lowering compiles")
	}
	for _, s := range []string{"code.At[code.Flow]", "code.At[code.Exec]"} {
		if !strings.Contains(string(out), s) {
			t.Errorf("the error does not name the stage %s:\n%s", s, out)
		}
	}
}
func TestStageKindVerifiesTheStage(t *testing.T) {
	// No dialect registers a check of the Exec stage in this package's tests: the test does.
	RegisterStageCheck[Exec](func(m *Module) error {
		if m.Name == "bad" {
			return errors.New("bad module")
		}
		return nil
	})
	defer delete(stageChecks, "exec")
	if err := ExecStage.Verify(EnterExec(&Module{Name: "good"})); err != nil {
		t.Errorf("a valid module: %v", err)
	}
	if err := ExecStage.Verify(EnterExec(&Module{Name: "bad"})); err == nil {
		t.Error("the check of the stage did not run")
	}
	m := &Module{Name: "good"}
	u := EnterExec(m)
	m.Stage = "flow"
	if err := ExecStage.Verify(u); err == nil {
		t.Error("a unit at the Exec stage whose module is at the Flow stage verifies")
	}
}
