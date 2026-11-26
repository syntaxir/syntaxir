package check

import "testing"

// TestOperatorsFoldWideningTypesLinesWithNode checks the type of $lhs and $rhs of an Operators Rule
// with a whole-rule action whose folded type keeps growing, which the lines decide with $lhs and
// $rhs of type node[_]. The lines of n make nodes, so $lhs and $rhs widen to node[_] and the folded
// value is the join of the lines so typed. The postfix ? of u gives a value that fits node[_] only because its type is not known, so $lhs
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
`)
	for rule, want := range map[string]string{
		"e": "unknown",
		"n": `node[string] | node[(node[_], node[string]), {"lhs": 0, "op": 1}] | node[_]`,
		"u": "unknown",
	} {
		if got, _ := inferredType(p, rule); got != want {
			t.Errorf("%s type = %s; want %s", rule, got, want)
		}
	}
}
