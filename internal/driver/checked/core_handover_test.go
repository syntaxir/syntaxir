package checked

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// The Core IR that a program compiled at run time builds for its closures is handed to the Core
// passes, which change it in place: closure.Core returns the module it was given, not a copy, so the
// backends pay for no copy of a module that nobody keeps.
func TestClosureCoreTakesItsModule(t *testing.T) {
	p := compiletest.Program(t, `rule main = ("ab" / "cd" / [0-9]+)* $$`, check.Options{})
	m, err := elaborate.CoreControlled(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	out, err := closure.Core(m)
	if err != nil {
		t.Fatal(err)
	}
	if out != m {
		t.Error("closure.Core returned a copy of its module")
	}
}
