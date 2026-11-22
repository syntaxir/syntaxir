package state

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestLoadActionLoops checks that LoadAction accepts counted loops and rejects every other way back:
// a backward jump, a loop end that does not match its loop, a jump into a loop's body, a body that
// writes its loop's index or count, and loops that do not nest. Each rejected case differs from an
// accepted one in the one instruction that breaks the rule.
func TestLoadActionLoops(t *testing.T) {
	in := func(op ActionOp, dst, a, b, c int32) bytecode.ActionInstr {
		return bytecode.ActionInstr{Op: uint8(op), Dst: dst, A: a, B: b, C: c}
	}
	loop := func(exit, index, count int32) bytecode.ActionInstr { return in(TLoop, exit, 0, index, count) }
	end := func(body, index, count int32) bytecode.ActionInstr { return in(TEndLoop, body, 0, index, count) }
	set := func(r int32) bytecode.ActionInstr { return in(TIntK, r, 1, 0, 0) }
	ret := in(TRet, 0, 0, 0, 0)
	for _, tc := range []struct {
		name string
		code []bytecode.ActionInstr
		want string // "" when LoadAction accepts the code
	}{
		{"loop", []bytecode.ActionInstr{loop(3, 0, 1), set(2), end(1, 0, 1), ret}, ""},
		{"empty body", []bytecode.ActionInstr{loop(2, 0, 1), end(1, 0, 1), ret}, ""},
		{"nested loops", []bytecode.ActionInstr{loop(5, 0, 1), loop(4, 2, 3), set(4), end(2, 2, 3), end(1, 0, 1), ret}, ""},
		{"loops one after another", []bytecode.ActionInstr{loop(2, 0, 1), end(1, 0, 1), loop(4, 0, 1), end(3, 0, 1), ret}, ""},
		{"jump out of a body", []bytecode.ActionInstr{loop(3, 0, 1), in(TJump, 4, 0, 0, 0), end(1, 0, 1), ret, ret}, ""},
		{"jump to a body's end", []bytecode.ActionInstr{loop(3, 0, 1), in(TJump, 2, 0, 0, 0), end(1, 0, 1), ret}, ""},
		{"index set before the loop", []bytecode.ActionInstr{set(0), loop(4, 0, 1), set(2), end(2, 0, 1), ret}, ""},

		{"backward jump", []bytecode.ActionInstr{set(2), in(TJump, 0, 0, 0, 0), ret}, "back to"},
		{"jump to itself", []bytecode.ActionInstr{in(TJumpF, 0, 2, 0, 0), ret}, "back to"},
		{"loop end with another index", []bytecode.ActionInstr{loop(3, 0, 1), set(2), end(1, 2, 1), ret}, "no matching end"},
		{"loop end with another count", []bytecode.ActionInstr{loop(3, 0, 1), set(2), end(1, 0, 2), ret}, "no matching end"},
		{"loop end to another place", []bytecode.ActionInstr{loop(3, 0, 1), set(2), end(2, 0, 1), ret}, "no matching end"},
		{"index register as count", []bytecode.ActionInstr{loop(3, 0, 0), set(2), end(1, 0, 0), ret}, "no matching end"},
		{"loop exit not after its end", []bytecode.ActionInstr{loop(2, 0, 1), set(2), end(1, 0, 1), ret}, "no matching end"},
		{"loop end without a loop", []bytecode.ActionInstr{set(2), set(2), end(1, 0, 1), ret}, "without a loop"},
		{"jump into a body", []bytecode.ActionInstr{in(TJump, 2, 0, 0, 0), loop(4, 0, 1), set(2), end(2, 0, 1), ret}, "into the loop"},
		{"body writes the index", []bytecode.ActionInstr{loop(3, 0, 1), set(0), end(1, 0, 1), ret}, "writes the index or count"},
		{"body writes the count", []bytecode.ActionInstr{loop(3, 0, 1), set(1), end(1, 0, 1), ret}, "writes the index or count"},
		{"inner loop reuses the index", []bytecode.ActionInstr{loop(4, 0, 1), loop(3, 0, 2), end(2, 0, 2), end(1, 0, 1), ret}, "writes the index or count"},
		{"loops that overlap", []bytecode.ActionInstr{loop(4, 0, 1), loop(5, 2, 3), set(4), end(1, 0, 1), end(2, 2, 3), ret}, "do not nest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := bytecode.Action{Regs: [4]int{1, 5, 1, 1}, Code: tc.code}
			_, err := LoadAction(&x, nil, 0, ActionTypes{})
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("rejected: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
