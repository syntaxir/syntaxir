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
	main := `flow main regs=12:
     0  POS        r10 =
     1  MOVE       r0 = r10
     2  CALLJUMP   r2,r1 = -> 38 key level=0
     3  SETCAPTURE r2 slot=0
     4  MATCHLITJUMP _,r1 = -> 38 "=" "\"=\""
     5  POS        r10 =
     6  MOVE       r3 = r10
     7  POS        r10 =
     8  MOVE       r4 = r10
     9  CONST      r10 = 0
    10  MOVE       r7 = r10
    11  STEP       
    12  MATCHCLASS r5,r6,r1 = -> 18 "[0-9]" "[0-9]"
    13  ADVANCE    r6
    14  CONST      r10 = 1
    15  ADD        r11 = r7 r10
    16  MOVE       r7 = r11
    17  JUMP       -> 11
    18  CONST      r10 = 1
    19  LT         r11 = r7 r10
    20  JUMPFALSE  r11 -> 22
    21  JUMP       -> 38
    22  TEXT       r11 = r4
    23  NEWTERMINAL r10 = r4 r11
    24  MOVE       r8 = r10
    25  SETCAPTURE r8 slot=1
    26  MARK       r10 =
    27  MOVE       r9 = r10
    28  GUARDLITJUMP -> 32 ';' depth=0 "\";\""
    29  MATCHLITJUMP _,r1 = -> 31 ";" "\";\""
    30  JUMP       -> 35
    31  RESET      r9
    32  JUMP       -> 35
    33  RESET      r9
    34  JUMP       -> 38
    35  CONST      r10 = nil
    36  CONST      r11 = true
    37  RETURN     r10 r11
    38  CONST      r10 = nil
    39  CONST      r11 = false
    40  RETURN     r10 r11
flow key regs=12:
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
