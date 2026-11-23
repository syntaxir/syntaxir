package flow_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
)

// The statements of the coverage tests. demand demands the input, advance reads it and moves the position, peek
// reads it, and test is a condition that reads nothing.
var (
	demand  = &code.Do{Call: &code.Call{Callee: flow.OpInputDemand}}
	advance = &code.Do{Call: &code.Call{Callee: flow.OpMatchLit}}
	peek    = &code.Do{Call: &code.Call{Callee: flow.OpPeek}}
)

func test() code.Expr { return &code.Call{Callee: "flow.test"} }

func blk(label string, stmts ...code.Stmt) *code.Block {
	return &code.Block{Label: label, Stmts: stmts}
}

func iff(then, els *code.Block) *code.If { return &code.If{Cond: test(), Then: then, Else: els} }

func loop(label string, stmts ...code.Stmt) *code.Loop {
	return &code.Loop{Label: label, Body: blk("", stmts...)}
}

// TestUncoveredReadsCoverage checks the coverage of each structure, the jumps that the counterexamples of the
// checker's first version missed (break, continue and exit leave their coverage to their targets), and the
// reads that a position move or a restore of the position makes uncovered.
func TestUncoveredReadsCoverage(t *testing.T) {
	for _, c := range []struct {
		name string
		body []code.Stmt
		want []string
	}{
		{"demand covers a read", []code.Stmt{demand, peek}, nil},
		{"a move uncovers a read", []code.Stmt{demand, advance, peek}, []string{flow.OpPeek}},
		{"a break on a covered path", []code.Stmt{demand, loop("L", peek, iff(blk("", &code.Break{Label: "L"}), nil), demand), peek}, nil},
		// The loop leaves after the advance, uncovered, so the read after it is not covered.
		{"a break leaves an uncovered state", []code.Stmt{demand, loop("L", advance, iff(blk("", &code.Break{Label: "L"}), nil), demand), peek}, []string{flow.OpPeek}},
		// The continue goes to the next iteration with the advance's uncovered position, so the head is uncovered.
		{"a continue passes its state to the head", []code.Stmt{demand, loop("L", peek, iff(blk("", advance, &code.Continue{Label: "L"}), nil), demand)},
			[]string{flow.OpPeek, flow.OpMatchLit}},
		// The exit leaves the labeled block with the advance's uncovered position.
		{"an exit leaves an uncovered state", []code.Stmt{demand, blk("B", advance, iff(blk("", &code.Exit{Label: "B"}), nil), demand), peek}, []string{flow.OpPeek}},
		// A break from an inner loop to the outer label leaves the outer loop uncovered.
		{"a break to an outer loop", []code.Stmt{demand, loop("O", loop("I", advance, iff(blk("", &code.Break{Label: "O"}), nil), demand)), peek}, []string{flow.OpPeek}},
		// A restore of the position uncovers the reads after it, as a move does.
		{"a set of the position", []code.Stmt{demand, &code.Set{Dst: []code.Place{{Name: prim.Pos, Reg: true}}, Values: []code.Expr{test()}}, peek}, []string{flow.OpPeek}},
		// The arguments of a call are evaluated before its read: the move of the argument uncovers the call's read.
		{"arguments before the call", []code.Stmt{demand, &code.Do{Call: &code.Call{Callee: flow.OpPeek, Args: []code.Expr{&code.Call{Callee: flow.OpMatchLit}}}}}, []string{flow.OpPeek}},
		// A return ends the path: the join with it keeps the coverage of the other path.
		{"a return is vacuous", []code.Stmt{demand, iff(blk("", &code.Return{}), nil), peek}, nil},
		// The cleanup of a protect meets the states of the body's returns, which are uncovered here.
		{"a protect meets its returns", []code.Stmt{demand, &code.Protect{Body: blk("", iff(blk("", &code.Return{}), nil), advance), Finally: blk("", peek)}}, []string{flow.OpPeek}},
		// A demand before each read of a loop covers it in every iteration.
		{"a demand in each iteration", []code.Stmt{loop("L", demand, peek, advance, iff(blk("", &code.Break{Label: "L"}), nil))}, nil},
		// A read in a later iteration needs its own demand.
		// The head of the later iterations is uncovered, so the peek and the advance after it both read uncovered.
		{"a read in a later iteration", []code.Stmt{demand, loop("L", peek, advance, iff(blk("", &code.Break{Label: "L"}), nil))}, []string{flow.OpPeek, flow.OpMatchLit}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &code.Func{Name: "f", Body: blk("", c.body...)}
			got := flow.UncoveredReads(f)
			if !slices.Equal(got, c.want) {
				t.Errorf("uncovered reads %v, want %v", got, c.want)
			}
		})
	}
}

// TestVerifyDemandsChecksSuspendedFunctions checks that VerifyDemands rejects a read without a demand in a
// suspended function, and accepts the same read in a function that is not suspended, which has no demands.
func TestVerifyDemandsChecksSuspendedFunctions(t *testing.T) {
	body := func() *code.Block { return blk("", advance) }
	bad := &code.Func{Name: "f", Body: body(), Attrs: &flow.Attrs{Suspend: true}}
	if err := flow.VerifyDemands(bad); err == nil {
		t.Error("a suspended function reads with no demand, and VerifyDemands accepted it")
	}
	ok := &code.Func{Name: "g", Body: blk("", demand, advance), Attrs: &flow.Attrs{Suspend: true}}
	if err := flow.VerifyDemands(ok); err != nil {
		t.Errorf("a demanded read is rejected: %v", err)
	}
	plain := &code.Func{Name: "h", Body: body(), Attrs: &flow.Attrs{}}
	if err := flow.VerifyDemands(plain); err != nil {
		t.Errorf("a function that is not suspended is checked: %v", err)
	}
}
