package state

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// actionOpNames names each instruction of action code as the specification's table does.
var actionOpNames = map[string]ActionOp{
	"UNITS": TUnits, "UNITSOF": TUnitsOf, "UNITSSINCE": TUnitsSince,
	"NOP": TNop, "CONST": TConst, "INT": TIntK, "MOVEA": TMoveA, "MOVEN": TMoveN, "MOVES": TMoveS,
	"MOVET": TMoveT, "CAP": TCap, "LOCAL": TLocal, "VAR": TVar, "UNBOX": TUnbox, "FROMB": TFromB,
	"FROMT": TFromT, "NOT": TNot, "LT": TLt, "ADD": TAdd, "EQN": TEqN, "ISNIL": TIsNil,
	"MEMBER": TMember, "UNARY": TUnary, "BINARY": TBinary, "LOGIC": TLogic, "TEXT": TText,
	"TEXTEQ": TTextEq, "LENGTH": TLength, "CONVERT": TConvert, "CONFORMS": TConforms,
	"CASTERR": TCastErr, "KIDS": TKids, "PUSH": TPush, "PUSHALL": TPushAll, "ABOVE": TAbove,
	"HULL": THull, "ITEMS": TItems, "ITEMAT": TItemAt, "SEQLEN": TSeqLen, "STEP": TStep,
	"KEEP": TKeep, "FILTER": TFilter, "COPY": TCopy, "STRUCT": TStruct, "SETFIELD": TSetField,
	"COVER": TCover, "MADE": TMade, "JUMP": TJump, "JUMPF": TJumpF, "RET": TRet, "RETERR": TRetErr,
	"FIELD": TField, "LOOP": TLoop, "ENDLOOP": TEndLoop,
}

// TestSpecActionOpcodes checks that the opcodes of action code are those of the table in
// spec/bytecode/README.md: a module stores the opcodes as they are, so renumbering an instruction
// changes the file format.
func TestSpecActionOpcodes(t *testing.T) {
	data, err := os.ReadFile("../../../spec/bytecode/README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(data), "\n## Action Code\n")
	if !ok {
		t.Fatal("spec/bytecode/README.md has no Action Code section")
	}
	table, _, _ = strings.Cut(table, "\n## ")
	row := regexp.MustCompile("(?m)^\\| (\\d+)(?:–(\\d+))? \\| ((?:`[A-Z]+`(?:, )?)+) \\|")
	name := regexp.MustCompile("`([A-Z]+)`")
	seen := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(table, -1) {
		first, _ := strconv.Atoi(m[1])
		last := first
		if m[2] != "" {
			last, _ = strconv.Atoi(m[2])
		}
		names := name.FindAllStringSubmatch(m[3], -1)
		if len(names) != last-first+1 {
			t.Errorf("the row of codes %s names %d instructions", m[0], len(names))
			continue
		}
		for i, n := range names {
			op, ok := actionOpNames[n[1]]
			switch {
			case !ok:
				t.Errorf("the specification names the instruction %s, which the runtime does not have", n[1])
			case int(op) != first+i:
				t.Errorf("%s is code %d in the specification and %d in the runtime", n[1], first+i, op)
			}
			seen[n[1]] = true
		}
	}
	for n, op := range actionOpNames {
		if !seen[n] {
			t.Errorf("the specification's table has no row for %s", n)
		}
		if _, ok := actionOperands[op]; !ok {
			t.Errorf("%s has no operand kinds", n)
		}
	}
	if len(actionOpNames) != len(actionOperands) {
		t.Errorf("the runtime has %d instructions, the table names %d", len(actionOperands), len(actionOpNames))
	}
}
