package checked

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestRuntimeTablesMatchTheCompiler checks the values that the runtime side holds a copy of, because
// it imports no package of the compiler side, against the compiler's own: the fixed expectations,
// the spellings of the operator kinds and associativities of an Operators Rule, the edit lag of a
// memo entry, the member kinds of action expressions and the ASCII folding of literals.
func TestRuntimeTablesMatchTheCompiler(t *testing.T) {
	if !slices.Equal(program.FixedDescs, ir.FixedDescs) || int(program.NumFixedDescs) != len(ir.FixedDescs) {
		t.Errorf("fixed expectations: runtime %q, Core IR %q", program.FixedDescs, ir.FixedDescs)
	}
	for _, c := range [][2]string{
		{program.OpPrefix, ast.Prefix}, {program.OpPostfix, ast.Postfix}, {program.OpInfix, ast.Infix},
		{program.AssocLeft, ast.AssocLeft}, {program.AssocRight, ast.AssocRight}, {program.AssocNone, ast.AssocNone},
	} {
		if c[0] != c[1] {
			t.Errorf("operator spelling: runtime %q, grammar %q", c[0], c[1])
		}
	}
	if state.PendingEditLimit != exec.PendingEditLimit {
		t.Errorf("pending edit limit: runtime %d, Exec IR %d", state.PendingEditLimit, exec.PendingEditLimit)
	}
	if state.MemberCapture+1 != len(action.MemberKinds) {
		t.Errorf("member kinds: runtime %d, Action dialect %d", state.MemberCapture+1, len(action.MemberKinds))
	}
	for r := rune(0); r < 0x300; r++ {
		if state.FoldASCIIRune(r) != compile.FoldASCIIRune(r) {
			t.Errorf("folding %q: runtime %q, compiler %q", r, state.FoldASCIIRune(r), compile.FoldASCIIRune(r))
		}
	}
}
