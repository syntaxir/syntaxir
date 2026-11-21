package engine

import (
	"errors"
	"testing"
)

// TestModuleErrorSharedByPrograms checks that an internal error of the bytecode compilation is
// reported by every program sharing the static program, not only by the one whose call compiled it:
// a later program must not get a nil module. The error is recorded directly, since no grammar makes
// the compilation fail.
func TestModuleErrorSharedByPrograms(t *testing.T) {
	t.Parallel()
	a := mustCompile(t, `rule main = "a"`)
	b := checkedProgram(a.Static())
	s := a.Static()
	want := errors.New("internal error: injected")
	s.BytecodeOnce.Do(func() { s.BytecodeErr = want })
	for name, p := range map[string]*Program{"first": a, "sharing": b} {
		func() {
			defer func() {
				if got := recover(); got != want {
					t.Errorf("%s: Module() panicked with %v, want %v", name, got, want)
				}
			}()
			m := p.Module()
			t.Errorf("%s: Module() returned %v without panicking", name, m)
		}()
	}
}
