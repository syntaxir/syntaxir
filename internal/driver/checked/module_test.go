package checked

import (
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestModuleBuiltOncePerSource checks that a source compiles its bytecode module once, on the
// first call of Module, and that BuiltModule observes it without building it.
func TestModuleBuiltOncePerSource(t *testing.T) {
	t.Parallel()
	s := Of(compiletest.Program(t, `rule main = "a"`, check.Options{}), pass.Settings{})
	if m := s.BuiltModule(); m != nil {
		t.Fatalf("BuiltModule() = %p before Module, want nil", m)
	}
	m, err := s.Module()
	if err != nil || m == nil {
		t.Fatalf("Module() = %v, %v", m, err)
	}
	if again, _ := s.Module(); again != m {
		t.Errorf("second Module() = %p, want the first module %p", again, m)
	}
	if got := s.BuiltModule(); got != m {
		t.Errorf("BuiltModule() = %p, want %p", got, m)
	}
}

// TestModuleErrorSharedByPrograms checks that an internal error of the bytecode compilation is
// reported by every program that runs the source, not only by the one whose call compiled it: a
// later program must not get a nil module. The error is recorded directly, since no grammar makes
// the compilation fail.
func TestModuleErrorSharedByPrograms(t *testing.T) {
	t.Parallel()
	s := Of(compiletest.Program(t, `rule main = "a"`, check.Options{}), pass.Settings{})
	want := errors.New("internal error: injected")
	s.moduleOnce.Do(func() { s.moduleErr = want })
	for name, p := range map[string]*engine.Program{"first": engine.New(s), "sharing": engine.New(s)} {
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
