package checked

import (
	"testing"

	"zeroparse.dev/zp/internal/backend/closure"
	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/frontend/elaborate"
	"zeroparse.dev/zp/internal/testing/compiletest"
)

// The Core IR that a program compiled at run time builds for its closures is handed to the Core
// passes, which change it in place: closure.Core returns the module it was given, not a copy, so the
// backends pay for no copy of a module that nobody keeps.
func TestClosureCoreTakesItsModule(t *testing.T) {
	p := compiletest.Program(t, `rule main = ("ab" / "cd" / [0-9]+)* $$`, compile.Options{})
	m, err := elaborate.CoreControlled(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := closure.Core(m)
	if err != nil {
		t.Fatal(err)
	}
	if out != m {
		t.Error("closure.Core returned a copy of its module")
	}
}
