package state

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestLoadActionRejectsEachCheck checks the operand and pool checks of LoadAction one by one: each case
// is a valid action expression, which loads (the negative control), and the same action expression with one value moved just
// out of range, which LoadAction rejects with the error of that check.
func TestLoadActionRejectsEachCheck(t *testing.T) {
	strs := []string{"S"}
	types := ActionTypes{
		NodeType: func(string) *nodes.NodeTypeKinds { return nil },
		Struct: func(name string) *valueimpl.Kind {
			if name == "S" {
				return valueimpl.SeqKind
			}
			return nil
		},
	}
	const vars = 1
	ret := bytecode.ActionInstr{Op: uint8(TRet), A: 0}
	// action expression returns an action expression of one value register, one register of every other file, and the code
	// in followed by a return.
	actionExpr := func(in ...bytecode.ActionInstr) bytecode.Action {
		return bytecode.Action{Regs: [4]int{1, 1, 1, 1}, Code: append(in, ret)}
	}
	for _, tc := range []struct {
		name string
		edit func(x *bytecode.Action, bad bool)
		want string
	}{
		{"sequence register", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(TMoveS)}
			if bad {
				in.Dst = 1
			}
			*x = actionExpr(in)
		}, "invalid operand"},
		{"field cover register", func(x *bytecode.Action, bad bool) {
			// The integer register that FIELD reads and writes, the one operand no other case pins.
			in := bytecode.ActionInstr{Op: uint8(TField)}
			if bad {
				in.Dst = 1
			}
			*x = actionExpr(in)
			x.Names = []int{0}
		}, "invalid operand"},
		{"variable", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(TVar), A: vars - 1}
			if bad {
				in.A = vars
			}
			*x = actionExpr(in)
		}, "invalid operand"},
		{"unary operator", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(TUnary), B: int32(len(action.UnaryOps) - 1)}
			if bad {
				in.B++
			}
			*x = actionExpr(in)
		}, "invalid operand"},
		{"binary operator", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(TBinary), C: int32(len(action.BinaryOps) - 1)}
			if bad {
				in.C++
			}
			*x = actionExpr(in)
		}, "invalid operand"},
		{"logical operator", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(TLogic), B: int32(len(action.LogicalOps) - 1)}
			if bad {
				in.B++
			}
			*x = actionExpr(in)
		}, "invalid operand"},
		{"flag", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(THull), B: 1}
			if bad {
				in.B = 2
			}
			*x = actionExpr(in)
		}, "invalid operand"},
		{"boolean constant", func(x *bytecode.Action, bad bool) {
			*x = actionExpr(bytecode.ActionInstr{Op: uint8(TConst)})
			x.Consts = []bytecode.ActionConst{{Kind: bytecode.ConstBool, Int: 1}}
			if bad {
				x.Consts[0].Int = 2
			}
		}, "invalid boolean constant"},
		{"slot", func(x *bytecode.Action, bad bool) {
			in := bytecode.ActionInstr{Op: uint8(TCap)}
			if bad {
				in.A = -1
			}
			*x = actionExpr(in)
		}, "invalid slot"},
		{"register count", func(x *bytecode.Action, bad bool) {
			*x = actionExpr()
			x.Regs[0] = 1 << 20
			if bad {
				x.Regs[0]++
			}
		}, "invalid register count"},
		{"negative register count", func(x *bytecode.Action, bad bool) {
			*x = actionExpr()
			x.Regs[3] = 0
			if bad {
				x.Regs[3] = -1
			}
		}, "invalid register count"},
		{"struct field count", func(x *bytecode.Action, bad bool) {
			*x = actionExpr()
			x.Structs = []bytecode.ActionStruct{{Type: 0, Fields: 0}}
			if bad {
				x.Structs[0].Fields = -1
			}
		}, "invalid field count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var good, bad bytecode.Action
			tc.edit(&good, false)
			tc.edit(&bad, true)
			if _, err := LoadAction(&good, strs, vars, types); err != nil {
				t.Fatalf("the valid action expression: %v", err)
			}
			if _, err := LoadAction(&bad, strs, vars, types); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
