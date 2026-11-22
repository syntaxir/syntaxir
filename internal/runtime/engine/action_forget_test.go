package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestPredicateForgetsWhatItsActionExpressionMade checks that a predicate leaves nothing of its action
// expression in the parser on every backend: the nodes the expression created stop being tracked,
// and the values that a failed expression gathered leave the stack of values. Neither is an event of
// the compute units (spec/runtime/parsing.md#compute-units), so the parser's state is where a node or
// a value that stayed behind shows.
func TestPredicateForgetsWhatItsActionExpressionMade(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, input string }{
		// Each predicate creates two nodes and holds.
		{"created", "type T = { N int }\n@start\nrule main = ({len(list(T{N: 1}, T{N: 2})) > 0} \"a\")* $$\n", "aaa"},
		// The concat gathers the elements of its first list in place before the second fails.
		{"gathered", "@start\nrule main = x:\"a\" ({len(concat(list(1, 2), list(10 / (1 - len($x))))) > 0} \"!\" / \"\") $$\n", "a"},
	}
	for _, tc := range cases {
		prog := mustCompile(t, tc.src)
		for name, b := range map[string]Backend{"closure": Closure, "bytecode": Bytecode, "iterative": BytecodeIterative} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				o := ParseOptions{Backend: b}
				active, _, _, err := prog.admitParse("main", o, optionsFamily(o), false, tc.input)
				if err != nil {
					t.Fatal(err)
				}
				p := state.NewParser(&active.sharedProgram, tc.input, sxirinput.CodePoints, false)
				if _, err := active.Run(p, b, "main"); err != nil {
					t.Fatal(err)
				}
				if len(p.Created) != 0 || len(p.KidStack) != 0 {
					t.Fatalf("%d created nodes and %d values stayed behind", len(p.Created), len(p.KidStack))
				}
			})
		}
	}
}
