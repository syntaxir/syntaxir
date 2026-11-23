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
	demand  = &code.Do{Call: &code.Call{Callee: flow.OpInputDemand, Imm: prim.Demand{Char: true}}}
	advance = matchLit("a")
	peek    = &code.Do{Call: &code.Call{Callee: flow.OpPeek}}
	restore = &code.Do{Call: &code.Call{Callee: flow.OpRestore}}
)

// matchLit is the statement that matches the literal text, which moves the position past it.
func matchLit(text string) *code.Do {
	return &code.Do{Call: &code.Call{Callee: flow.OpMatchLit, Imm: prim.Lit{Text: text}}}
}

// demandLit is the statement that demands the input of the literal text.
func demandLit(text string) *code.Do {
	return &code.Do{Call: &code.Call{Callee: flow.OpInputDemand, Imm: prim.Demand{Lit: prim.Lit{Text: text}}}}
}

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
		// An exit to the loop's own label leaves the loop uncovered, as a break does.
		{"an exit to a loop leaves an uncovered state", []code.Stmt{demand, loop("L", advance, iff(blk("", &code.Exit{Label: "L"}), nil), demand), peek}, []string{flow.OpPeek}},
		// A break from an inner loop to the outer label leaves the outer loop uncovered.
		{"a break to an outer loop", []code.Stmt{demand, loop("O", loop("I", advance, iff(blk("", &code.Break{Label: "O"}), nil), demand)), peek}, []string{flow.OpPeek}},
		// A restore of the position uncovers the reads after it, as a move does.
		{"a set of the position", []code.Stmt{demand, &code.Set{Dst: []code.Place{{Name: prim.Pos, Reg: true}}, Values: []code.Expr{test()}}, peek}, []string{flow.OpPeek}},
		// A restore of a backtracking point moves the position back, so a read of the next alternative needs its own
		// demand: the one before the restore covers nothing after it.
		{"a restore uncovers the reads after it", []code.Stmt{demand, restore, peek}, []string{flow.OpPeek}},
		{"a demand after a restore covers its read", []code.Stmt{demand, restore, demand, peek}, nil},
		// A demand of one character covers a read of one character, but not a literal of two.
		{"a character demand does not cover a longer literal", []code.Stmt{demand, matchLit("ab")}, []string{flow.OpMatchLit}},
		// A demand of a literal covers a read of the literal, and of any shorter literal.
		{"a literal demand covers the literal", []code.Stmt{demandLit("ab"), matchLit("ab")}, nil},
		{"a literal demand covers a shorter literal", []code.Stmt{demandLit("ab"), matchLit("a")}, nil},
		// A literal demand of one character does not cover a longer literal.
		{"a literal demand does not cover a longer literal", []code.Stmt{demandLit("a"), matchLit("ab")}, []string{flow.OpMatchLit}},
		{"a literal demand covers a multi-byte literal", []code.Stmt{demandLit("é"), matchLit("é")}, nil},
		// A character demand joined with a literal demand covers no more bytes than the literal: the character
		// may take one byte, so the literal "ab" and a character do not cover the three bytes of "aé".
		{"a character demand adds no bytes to a literal demand", []code.Stmt{demandLit("ab"), demand, matchLit("aé")}, []string{flow.OpMatchLit}},
		{"a literal and a character demand cover both reads", []code.Stmt{demandLit("ab"), demand, peek, matchLit("ab")}, nil},
		// A literal demand shorter than utf8.UTFMax bytes does not cover a complete character, and one of at least
		// utf8.UTFMax bytes does.
		{"a short literal demand does not cover a character", []code.Stmt{demandLit("ab"), peek}, []string{flow.OpPeek}},
		{"a long literal demand covers a character", []code.Stmt{demandLit("abcd"), peek}, nil},
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
