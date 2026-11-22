package state

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestActionEvalClearsRegisters checks that an action program gives its registers back cleared: the
// registers outlive the evaluation in the parser (and in a pooled parser, the parse), so a value
// left in one would stay reachable after its action expression returned.
func TestActionEvalClearsRegisters(t *testing.T) {
	// The action expression $op: the Operators Rule local op, returned.
	prog := &ActionProgram{Code: []ActionInstr{{Op: TLocal, Dst: 0, A: 0}, {Op: TRet, A: 0}}, NA: 1, Names: []string{"op"}}
	p := &Parser{}
	op := nodes.Ptr(nodes.Data{Kind: valueimpl.SeqKind, Start: 0, End: 1})
	got, err := prog.Eval(&EvalCtx{P: p, Locals: &Local{Name: "op", Val: op}})
	if err != nil {
		t.Fatal(err)
	}
	if got != any(op) {
		t.Fatalf("the action expression returned %v, want $op", got)
	}
	if len(p.actionExprs.a) != 0 {
		t.Fatalf("%d value registers stay taken after the action expression returned", len(p.actionExprs.a))
	}
	for i, v := range p.actionExprs.a[:cap(p.actionExprs.a)] {
		if v != nil {
			t.Errorf("value register %d still holds %v after the action expression returned", i, v)
		}
	}
}
