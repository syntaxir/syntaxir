package vmlower_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/vmlowertest"
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
     0  POS        r1 =
     1  CALLJUMP   r3,r2 = -> 21 key level=0
     2  SETCAPTURE r3 slot=0
     3  MATCHLITJUMP _,r2 = -> 21 "=" "\"=\""
     4  POS        r4 =
     5  POS        r5 =
     6  CONST      r8 = 0
     7  SCANCLASSCOUNT r6,r7,r2,r8 = r8 "[0-9]" "[0-9]"
     8  CONST      r11 = 1
     9  LT         r12 = r8 r11
    10  JUMPTRUE   r12 -> 21
    11  TEXT       r11 = r5
    12  NEWTERMINAL r9 = r5 r11
    13  SETCAPTURE r9 slot=1
    14  MARK       r10 =
    15  GUARDLITJUMP -> 19 ';' depth=0 "\";\""
    16  MATCHLITJUMP _,r2 = -> 18 ";" "\";\""
    17  JUMP       -> 19
    18  RESET      r10
    19  CONST      r11 = nil
    20  RETURNOK   r11
    21  RETURNFAIL 
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

// TestMissingTokensModule checks that a grammar that uses #missing has a bytecode module.
func TestMissingTokensModule(t *testing.T) {
	t.Parallel()
	prog := compiletest.Program(t, `rule main = ";" #missing`, check.Options{SourceInfo: true})
	if m, err := checked.Of(prog, pass.Settings{}).Module(); m == nil || err != nil {
		t.Fatal(m, err)
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
	m, _, err := vmlowertest.Lower(vmInput(t, compiletest.Program(t, src, o)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// vmInput returns the input of the bytecode lowering of prog at the default settings, as the VM
// pipeline of the driver builds it (vmlowertest.Input).
func vmInput(t testing.TB, prog *frontend.Result) vmlower.Input {
	t.Helper()
	return vmlowertest.Input(t, prog, pass.Settings{})
}

// varModuleSource declares variables of each type, reads them in predicates and rebinds them.
const varModuleSource = `var n int = 3
var s string = "x"
var b bool
rule main = {n := n + 1} {s := s + "y"} {n > 2 && s != "" && !b} item "a"
rule item = {n > 0} "b"?
`
