//go:build !sxir_budget_controls_reference

package engine

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
)

// leastBudget returns the smallest budget under which the parse succeeds.
func leastBudget(t *testing.T, prog *Program, input string, o ParseOptions) uint64 {
	t.Helper()
	fits := func(b uint64) bool {
		o.Limits = runtime.Limits{Compute: b}
		_, err := prog.ParseWith("main", input, o)
		return err == nil
	}
	hi := uint64(1)
	for !fits(hi) {
		if hi > 1<<30 {
			t.Fatalf("input %q does not parse", input)
		}
		hi *= 2
	}
	lo := hi / 2 // fails, or 0
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if fits(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// One more element of each repetition, scan, rule call and Operators Rule counts exactly the compute
// units of its events (spec/runtime/parsing.md#compute-units): the input it advances over, its calls,
// the values its actions build and the bytes of the strings that len measures, so a lost or an extra
// count changes the step. The iterations, the
// lines of an operator level that fail and the lookups of its operators count nothing. Every backend counts the same.
func TestBudgetCoarseChargesPerIteration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src     string
		input         func(k int) string
		parse, recogn uint64
	}{
		{"repeat", `rule main = ("a" "b")* $$`, func(k int) string { return strings.Repeat("ab", k) },
			2, 2},
		{"scan", `rule main = [a-z]* $$`, func(k int) string { return strings.Repeat("a", k) },
			1, 1},
		{"call", "rule main = item* $$\nrule item = \"a\"", func(k int) string { return strings.Repeat("a", k) },
			2, 2},
		{"capture call", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", func(k int) string { return strings.Repeat("a", k) },
			3, 3},
		{"projected", "type L = { N int }\nrule main = xs:(-\",\" r:x)* $$ => L{N: len(map($xs, (e) => e.$r))}\nrule x = [a-z] #atomic",
			func(k int) string { return strings.Repeat(",a", k) }, 5, 3},
		{"prefix", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n    }\n    level {\n        prefix \"-\"\n    }\n}",
			func(k int) string { return strings.Repeat("-", k) + "a" }, 1, 1},
		{"infix", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n    }\n}",
			func(k int) string { return "a" + strings.Repeat("+a", k) }, 3, 3},
		// Three lines in one level: the longest match tries all of them at every operator position, and
		// the lines that fail count nothing.
		{"infix, three lines", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n        infix left \"-\"\n        infix left \"*\"\n    }\n}",
			func(k int) string { return "a" + strings.Repeat("+a", k) }, 3, 3},
	}
	for _, c := range cases {
		prog := mustCompile(t, c.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				o := ParseOptions{Expectations: true, Backend: backend, Recognize: recognize}
				want := c.parse
				if recognize {
					want = c.recogn
				}
				for k := 8; k <= 10; k++ {
					if step := leastBudget(t, prog, c.input(k+1), o) - leastBudget(t, prog, c.input(k), o); step != want {
						t.Errorf("%s %v recognize=%t: element %d costs %d, want %d", c.name, backend, recognize, k+1, step, want)
					}
				}
			}
		}
	}
}
