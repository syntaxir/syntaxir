package common_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// editedSource is the compiler side of a program whose bytecode module and closure module the
// driver builds with the edits of the pipeline, such as an Action pass of a test.
type editedSource struct {
	*checked.Source
	edits  []pipe.Edit
	module func() (*bytecode.Module, error)
	core   func() (any, error)
}

func newEditedSource(src *checked.Source, edits ...pipe.Edit) *editedSource {
	s := &editedSource{Source: src, edits: edits}
	compile := func(b result.Backend) (result.Output, error) {
		outs, err := driver.Compile(context.Background(), src.Program(), driver.Options{Edits: s.edits}, driver.Target{Backend: b})
		if err != nil {
			return nil, err
		}
		return outs[0], nil
	}
	s.module = sync.OnceValues(func() (*bytecode.Module, error) {
		out, err := compile(result.VM)
		if err != nil {
			return nil, err
		}
		return out.(*vmlower.Bytecode).Module, nil
	})
	s.core = sync.OnceValues(func() (any, error) { return compile(result.Closure) })
	return s
}

func (s *editedSource) Module() (*bytecode.Module, error) { return s.module() }
func (s *editedSource) Core(context.Context) (any, error) { return s.core() }

// testActionUnchanged is an Action pass of the tests that changes nothing.
var testActionUnchanged = pipe.New(pipe.Def[actionpass.Unit, actionpass.Unit]{
	Decl: pipe.Decl{Name: "test-action-unchanged", Test: true, Obligation: "The function is unchanged."},
	In:   code.ActionStage, Out: code.ActionStage,
	Run: func(_ *pipe.Ctx[actionpass.Unit], u actionpass.Unit) (actionpass.Unit, pass.PreservedSet, error) {
		return u, pass.PreserveAll(), nil
	},
})

// testActionSwapsEquality is a broken Action pass of the tests: it swaps == and != in every call of
// the composition binary of the function of a site.
var testActionSwapsEquality = pipe.New(pipe.Def[actionpass.Unit, actionpass.Unit]{
	Decl: pipe.Decl{Name: "test-action-swaps-equality", Test: true, Obligation: "none: it breaks the function"},
	In:   code.ActionStage, Out: code.ActionStage,
	Run: func(_ *pipe.Ctx[actionpass.Unit], u actionpass.Unit) (actionpass.Unit, pass.PreservedSet, error) {
		eq, ne := int64(slices.Index(action.BinaryOps, "==")), int64(slices.Index(action.BinaryOps, "!="))
		code.Walk(u.Module().Funcs[0].Body, nil, func(e code.Expr) {
			c, ok := e.(*code.Call)
			if !ok || c.Callee != "binary" {
				return
			}
			switch op := c.Args[0].(*code.Const); op.Value {
			case eq:
				c.Args[0] = code.Int(ne)
			case ne:
				c.Args[0] = code.Int(eq)
			}
		})
		return u, pass.PreserveNone(), nil
	},
})

// TestActionReferenceDetectsBrokenActionPass is the negative control of the comparisons of the engine's
// backends with the reference evaluator (TestActionReferenceEvaluator): an Action pass that swaps ==
// and != in the functions of the Action module makes the closure backend, the bytecode VM and its
// iterative form give other values than the reference evaluator, and a pass that changes nothing
// leaves them all equal.
func TestActionReferenceDetectsBrokenActionPass(t *testing.T) {
	t.Parallel()
	backends := []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative}
	cases, _ := actionSeedCases(t, 1, 1, true)
	for _, c := range []struct {
		name   string
		broken bool
		pass   *pipe.Pass[actionpass.Unit, actionpass.Unit]
	}{
		{"control: unchanged", false, testActionUnchanged},
		{"== and != swapped", true, testActionSwapsEquality},
	} {
		mismatches := map[engine.Backend]int{}
		compared := 0
		for _, k := range cases {
			if k.MissingTokens {
				continue // the parse that inserts tokens runs another program of the grammar
			}
			g, _ := parseTest(k.Src)
			prog, err := entry.Compile(g, entry.Options{})
			if err != nil {
				t.Fatal(err)
			}
			edited := engine.New(newEditedSource(entry.Source(prog), pipe.Fill(driver.ActionSlot, "", pipe.Seq(code.ActionStage, c.pass))))
			actionExpr := referenceAction(t, k)
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				w := utf8.RuneCountInString(k.Lit)
				if unit == input.Bytes {
					w = len(k.Lit)
				}
				env := &ref.ActionEnv{Captures: map[string]ref.V{"x": ref.Node(ref.StringNode, k.Lit, 0, w, nil, nil)}, Bytes: unit == input.Bytes,
					Start: 0, End: w, Line: 1, Col: 1, Zeros: referenceZeros(t, entry.Checked(prog), "R", "Inner", "Leaf")}
				in := k.Lit
				if k.Kind == codegentest.PredicateAction {
					in += "b"
				}
				for _, b := range backends {
					mismatches[b] += len(referenceMismatches(k, edited, actionExpr, env, unit, in, false, b))
				}
				compared++
			}
		}
		t.Logf("%s: %d comparisons, mismatches %v", c.name, compared, mismatches)
		if compared == 0 {
			t.Fatalf("%s: no case was compared", c.name)
		}
		for _, b := range backends {
			switch {
			case c.broken && mismatches[b] == 0:
				t.Errorf("%s: backend %d gives the reference values", c.name, b)
			case !c.broken && mismatches[b] != 0:
				t.Errorf("%s: backend %d has %d mismatches", c.name, b, mismatches[b])
			}
		}
	}
}
