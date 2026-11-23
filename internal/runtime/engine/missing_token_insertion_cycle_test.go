package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestMissingTokensInsertionOnlyLeftRecursion checks that a rule left-recursive only through an
// insertion is grown when insertion is enabled, and only then: parses without insertion run the
// program of the grammar's own flags, and each insertion of "(" before a seed grows it by one "x".
func TestMissingTokensInsertionOnlyLeftRecursion(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, statetest.InsertionCycle)
	if prog.Static().InsertionFlags == nil || prog.ByName["a"].Leader {
		t.Fatalf("flags: insertion %v, leader without insertion %v", prog.Static().InsertionFlags, prog.ByName["a"].Leader)
	}
	for input, ok := range map[string]bool{"y": true, "(yx": true, "((yxx": true, "yx": false, "": false} {
		n, err := prog.ParseWith("main", input, ParseOptions{})
		if ok != (err == nil && n != nil) {
			t.Fatalf("without insertion, %q: %v %v", input, n, err)
		}
	}
	if prog.ins != nil {
		t.Fatal("a parse without insertion built the insertion program")
	}
	for _, controlled := range []bool{false, true} {
		if controlled && !state.BudgetControlsEnabled {
			continue
		}
		for _, recognize := range []bool{false, true} {
			o := ParseOptions{Expectations: true, MissingTokens: 1000, Recognize: recognize}
			if controlled {
				o.Context = context.Background()
				o.Limits.Compute = 1 << 24
			}
			if n, err := prog.ParseWith("main", "y", o); err != nil || !recognize && len(statetest.MissingNodes(t, n)) != 0 {
				t.Fatalf("controlled %v, recognize %v: y: %v %v", controlled, recognize, n, err)
			}
			for xs := 1; xs <= 4; xs++ {
				input := "y" + strings.Repeat("x", xs)
				// The insertions are all at the start, where their diagnostics are one syntax error.
				n, _ := requireInsertions(t, prog, input, o, 1)
				if !recognize && len(statetest.MissingNodes(t, n)) != xs {
					t.Fatalf("controlled %v: %q: missing nodes %d", controlled, input, len(statetest.MissingNodes(t, n)))
				}
			}
		}
	}
	if prog.ins == nil || !prog.ins.ByName["a"].Leader || prog.ins.ByName["a"].Memo {
		t.Fatal("insertion parses did not run the program with the insertion flags")
	}
}

// TestMissingTokensInsertionOnlyLeftRecursionLimits checks that the limit of missing tokens only
// caps an insertion-only left recursion: every parse with a positive limit either gives the result
// of an unbounded one or fails with the limit error, since abandoned attempts, the growth rounds'
// included, count toward the limit.
func TestMissingTokensInsertionOnlyLeftRecursionLimits(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, statetest.InsertionCycle)
	render := func(n *value.Node, err error) string {
		data, jerr := json.Marshal(n)
		if jerr != nil {
			t.Fatal(jerr)
		}
		var recovered value.SyntaxErrors
		if errors.As(err, &recovered) {
			err = nil
		}
		return string(data) + "|" + strings.Repeat("e", len(recovered)) + "|" + func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}()
	}
	for _, input := range []string{"y", "yx", "yxx", "yxxx", "(yx", "x", ""} {
		want := render(prog.ParseWith("main", input, ParseOptions{Expectations: true, MissingTokens: 1000}))
		capped := false
		for limit := 1; limit <= 50; limit++ {
			n, err := prog.ParseWith("main", input, ParseOptions{Expectations: true, MissingTokens: limit})
			var exceeded *runtime.MissingTokenLimitError
			if errors.As(err, &exceeded) {
				if capped {
					t.Fatalf("%q: limit %d exceeded after a smaller limit sufficed", input, limit)
				}
				continue
			}
			capped = true
			if got := render(n, err); got != want {
				t.Fatalf("%q at limit %d: %s, want %s", input, limit, got, want)
			}
		}
		if !capped {
			t.Fatalf("%q: every limit up to 50 was exceeded", input)
		}
	}
}

// TestMissingTokensInsertionOnlyLeftRecursionBackends checks that the bytecode VMs, the compiled grammar
// loaded on them and Documents parse a grammar whose left recursion exists only through an
// insertion as the closure engine does, with insertion off and on.
func TestMissingTokensInsertionOnlyLeftRecursionBackends(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, statetest.InsertionCycle)
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"y", "(yx", "x"} {
		checkBackends(t, prog, "main", in)
		for _, limit := range []int{0, 1, 2} {
			want := codegentest.ResultJSON(prog.ParseWith("main", in, ParseOptions{Backend: Closure, Expectations: true, MissingTokens: limit}))
			for _, b := range vmBackends {
				for _, p := range []*Program{prog, loaded} {
					if got := codegentest.ResultJSON(p.ParseWith("main", in, ParseOptions{Backend: b, Expectations: true, MissingTokens: limit})); got != want {
						t.Errorf("%q limit %d %v (loaded %t): %s, want %s", in, limit, b, p == loaded, got, want)
					}
				}
			}
		}
	}
	// A Document grows the cycle as the parse from scratch does, on every backend.
	for _, b := range append([]Backend{Closure}, vmBackends...) {
		for _, limit := range []int{0, 1, 2} {
			o := ParseOptions{Backend: b, Expectations: true, MissingTokens: limit}
			d, err := prog.NewDocumentWith("main", "y", o)
			if err != nil {
				t.Fatal(err)
			}
			for _, in := range []string{"y", "(yx", "x"} {
				if err := d.Edit(0, len([]rune(d.Text())), in); err != nil {
					t.Fatal(err)
				}
				n, err := d.ParseWith(o)
				if got, want := codegentest.ResultJSON(n, err), codegentest.ResultJSON(prog.ParseWith("main", in, o)); got != want {
					t.Errorf("document %q limit %d %v: %s, want %s", in, limit, b, got, want)
				}
			}
		}
	}
}
