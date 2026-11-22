package vmlower_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestModuleDisassembly checks the disassembly of a module as the compiler makes it: the rule table
// and the Flow body of a rule.
func TestModuleDisassembly(t *testing.T) {
	t.Parallel()
	got := compileModule(t, `type P = { K node[string]; N int }
rule main = k:key "=" v:[0-9]+ #atomic (";" / _) => P{K: $k, N: len($v) + 1}
rule key = [a-z]+ @error(message="expected a key") #atomic`).Disassemble()
	rules := `rules:
  0 main scope=[k v] action=0 memo,transient
  1 key scope=[] value=elems=false shared=false tuple=false k=1 mode=1 memo,transient
actions:
main.action:
  action 0 `
	main := `flow main regs=13:
     0  POS        r11 =
     1  MOVE       r1 = r11
     2  CALLJUMP   r3,r2 = -> 39 key level=0
     3  SETCAPTURE r3 slot=0
     4  MATCHLITJUMP _,r2 = -> 39 "=" "\"=\""
     5  POS        r11 =
     6  MOVE       r4 = r11
     7  POS        r11 =
     8  MOVE       r5 = r11
     9  CONST      r11 = 0
    10  MOVE       r8 = r11
    11  CONST      r0 = -1
    12  STEP       r0 = r0
    13  MATCHCLASS r6,r7,r2 = -> 19 "[0-9]" "[0-9]"
    14  ADVANCE    r7
    15  CONST      r11 = 1
    16  ADD        r12 = r8 r11
    17  MOVE       r8 = r12
    18  JUMP       -> 12
    19  CONST      r11 = 1
    20  LT         r12 = r8 r11
    21  JUMPFALSE  r12 -> 23
    22  JUMP       -> 39
    23  TEXT       r12 = r5
    24  NEWTERMINAL r11 = r5 r12
    25  MOVE       r9 = r11
    26  SETCAPTURE r9 slot=1
    27  MARK       r11 =
    28  MOVE       r10 = r11
    29  GUARDLITJUMP -> 33 ';' depth=0 "\";\""
    30  MATCHLITJUMP _,r2 = -> 32 ";" "\";\""
    31  JUMP       -> 36
    32  RESET      r10
    33  JUMP       -> 36
    34  RESET      r10
    35  JUMP       -> 39
    36  CONST      r11 = nil
    37  CONST      r12 = true
    38  RETURN     r11 r12
    39  CONST      r11 = nil
    40  CONST      r12 = false
    41  RETURN     r11 r12
flow key regs=13:
`
	// The action code is the engine's translation of the action's Core IR, which the action tests pin.
	if !strings.HasPrefix(got, rules) || !strings.Contains(got, main) {
		t.Errorf("got\n%s", got)
	}
}

func TestDiagnosticLabelDisassembly(t *testing.T) {
	t.Parallel()
	module := compileModule(t, `rule main = "a" @error(message="explicit") / "b" @diagnostic(name="letter")`)
	text := module.Disassemble()
	t.Logf("%s", text)
	if !strings.Contains(text, `"explicit" message`+"\n") {
		t.Errorf("explicit error label changed:\n%s", text)
	}
	if !strings.Contains(text, `MERGERENAMED r9 r10 "letter"`+"\n") {
		t.Errorf("diagnostic label mode missing:\n%s", text)
	}
}

// TestLoosestLevelCall checks that a call of the loosest level of an Operators Rule is the plain
// call of the rule: a call with binding level 0, so the same memo entry. The cost
// view treats the two as the same expression.
func TestLoosestLevelCall(t *testing.T) {
	t.Parallel()
	text := compileModule(t, `rule main = level(e, sum) "x" / e "y" / level(e, mul) "z"
rule e = #operators {
  operand "n"
  level sum { infix left "+" }
  level mul { infix left "*" }
}`).Disassemble()
	var calls []string
	for _, line := range strings.Split(text, "\n") {
		if f := strings.Fields(line); len(f) > 3 && (f[1] == "CALL" || f[1] == "CALLJUMP") {
			calls = append(calls, strings.Join(f[len(f)-2:], " ")) // the rule and the binding level
		}
	}
	want := []string{"e level=0", "e level=0", "e level=1"}
	if strings.Join(calls, "; ") != strings.Join(want, "; ") {
		t.Errorf("calls %q, want %q\n%s", calls, want, text)
	}
}

// TestMissingTokensModuleGuard checks that a grammar that uses #missing has no bytecode form.
func TestMissingTokensModuleGuard(t *testing.T) {
	t.Parallel()
	prog := compiletest.Program(t, `rule main = ";" #missing`, check.Options{SourceInfo: true})
	if _, _, err := vmlower.Compile(vmInput(t, prog)); err == nil || !strings.Contains(err.Error(), "bytecode module construction") {
		t.Fatal("silent bytecode lowering", err)
	}
}

// compileModule compiles src with source information, and the options opts if any, to a module.
func compileModule(t *testing.T, src string, opts ...check.Options) *bytecode.Module {
	t.Helper()
	var o check.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	o.SourceInfo = true
	m, _, err := vmlower.Compile(vmInput(t, compiletest.Program(t, src, o)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// vmInput returns the input of the bytecode lowering of prog (checked.Source.VMInput).
func vmInput(t testing.TB, prog *frontend.Result) vmlower.Input {
	t.Helper()
	in, err := checked.Of(prog).VMInput()
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// varModuleSource declares variables of each type, reads them in predicates and rebinds them.
const varModuleSource = `var n int = 3
var s string = "x"
var b bool
rule main = {n := n + 1} {s := s + "y"} {n > 2 && s != "" && !b} item "a"
rule item = {n > 0} "b"?
`
