package check

import (
	"fmt"
	"testing"
)

// TestOperatorsFoldWideningTypesLinesWithNode checks the type of $lhs and $rhs of an Operators Rule
// with a whole-rule action whose folded type keeps growing, which the lines decide with $lhs and
// $rhs of type node[_]. The lines of n make nodes, so $lhs and $rhs widen to node[_] and the folded
// value is the join of the lines so typed. The postfix ! of c makes a node with a capture of the
// type that is not known, which needs no cast to node[_], so $lhs of c widens to node[_] as well.
// The postfix ? of u gives a value that fits node[_] only because its type is not known, so $lhs
// and $rhs of u are of the type that is not known, and its postfix %, which indexes $lhs, compiles;
// with $lhs of type node[_] it would not. The postfix ? of e reads the value of its operand, which
// is no node, so the rounds of e already end at the type that is not known.
func TestOperatorsFoldWideningTypesLinesWithNode(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, unknownList+`rule e = #operators {
    operand "a"
    level { postfix "!" }
    level { postfix "?" => $lhs.Value }
} => $value
rule n = #operators {
    operand "a"
    level { postfix "!" }
    level { prefix "-" => $rhs }
} => $value
rule u = #operators {
    operand "a"
    level { postfix "!" }
    level { postfix "?" x:g => $x }
    level { postfix "%" => ($lhs[0], $lhs)[1] }
} => $value
rule c = #operators {
    operand "a"
    level { postfix "!" x:g }
} => $value
`)
	for rule, want := range map[string]string{
		"e": "unknown",
		"n": `node[string] | node[(node[_], node[string]), {"lhs": 0, "op": 1}] | node[_]`,
		"u": "unknown",
		"c": `node[string] | node[(node[_], node[(node[string], g), {"x": 1}]), {"lhs": 0, "op": 1}]`,
	} {
		if got, _ := inferredType(p, rule); got != want {
			t.Errorf("%s type = %s; want %s", rule, got, want)
		}
	}
}

// TestOperatorsFoldWideningTypesLinesAtNode checks that the lines that decide the widening are typed
// with $lhs and $rhs of type node[_] itself, not of a type that holds no value. The operand t12 is a
// sequence nested thirteen deep, so the rounds of e end before its type is stable. The postfix ?
// indexes $lhs, which a node[_] does not allow, so typed with $lhs of type node[_] the line gives no
// value of that type, and $lhs and $rhs of e are of the type that is not known. Typed with $lhs of
// no type, the line would fit node[_], e would widen to it, and the last typing would reject the
// index.
func TestOperatorsFoldWideningTypesLinesAtNode(t *testing.T) {
	t.Parallel()
	src := "rule t0 = \"a\" \"a\"\n"
	for i := 1; i <= 12; i++ {
		src += fmt.Sprintf("rule t%d = t%d \"a\"\n", i, i-1)
	}
	p := mustCompile(t, src+`rule e = #operators {
    operand t12
    level { postfix "!" }
    level { postfix "?" => $lhs[0] }
} => $value
`)
	if got, _ := inferredType(p, "e"); got != "unknown" {
		t.Errorf("e type = %s; want unknown", got)
	}
}
