package frontend

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/testing/factstest"
)

// TestStructuralTypes requires the checked types of a structural module to be all or nothing
// (spec/core-ir/modules.md, Structural modules): for every corpus grammar that the compiler
// accepts, the structural modules of CheckStructural and of Result.WithModule carry a type on every rule
// and on every action expression but a lambda; for a grammar whose types do not check, the
// structural module of CheckStructural carries none.
func TestStructuralTypes(t *testing.T) {
	t.Parallel()
	accepted := 0
	for _, path := range factstest.Corpus {
		g, err := parseMarked(factstest.Read(t, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		prog, st, err := CheckStructural(g, check.Options{}, nil, desugar.InlineNames{}, checkfacts.Source{}, nil)
		if err != nil {
			if st != nil && st.Module != nil {
				if err := untyped(st.Module); err != nil {
					t.Errorf("%s: rejected: %v", path, err)
				}
			}
			continue
		}
		accepted++
		if err := typed(st.Module); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		res, err := prog.WithModule(nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := typed(res.Structural.Module); err != nil {
			t.Errorf("%s: WithModule: %v", path, err)
		}
	}
	if accepted == 0 {
		t.Fatal("the compiler accepts no corpus grammar")
	}
	// Grammars whose structural modules are built but whose types do not check.
	for _, src := range []string{
		"@start\nrule main = x:\"a\" => x + 1\n",
		"@start\nrule main = x:\"a\" => len(1)\n",
	} {
		g, err := parseMarked(src)
		if err != nil {
			t.Fatal(err)
		}
		_, st, err := CheckStructural(g, check.Options{}, nil, desugar.InlineNames{}, checkfacts.Source{}, nil)
		if err == nil {
			t.Fatalf("%q: the compiler accepts it", src)
		}
		if st == nil || st.Module == nil {
			t.Fatalf("%q: no structural module: %v", src, err)
		}
		if err := untyped(st.Module); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

// TestStructuralTypesMutant requires TestStructuralTypes to see a structural module of a corpus
// grammar on which the record of the types skips one action expression.
func TestStructuralTypesMutant(t *testing.T) {
	t.Parallel()
	g, err := parseMarked(factstest.Read(t, factstest.Corpus[0]))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := compileGrammar(g, check.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defs, _ := desugar.UniqueRules(prog.Grammar, nil)
	m, err := elaborate.Structural(elaborate.StructuralInput(prog.Grammar, defs, prog.StartRules), true)
	if err != nil {
		t.Fatal(err)
	}
	skipped := false
	recordTypes(m, prog.Checker().ValueRecord(), func(x core.ActionExpr, typ core.Type) {
		if _, lambda := x.(*core.Lambda); !skipped && !lambda {
			skipped = true
			return
		}
		core.SetType(x, typ)
	})
	if !skipped {
		t.Fatal("the grammar has no action expression to skip")
	}
	if typed(m) == nil {
		t.Error("a module with an action expression left without its type passes as typed")
	}
	if untyped(m) == nil {
		t.Error("a module with the types of its other parts passes as carrying none")
	}
}

// typed reports a rule of m without a type, or an action expression of m but a lambda without one.
func typed(m *core.Module) error {
	return eachType(m, func(what string, has bool) error {
		if !has {
			return fmt.Errorf("%s has no type", what)
		}
		return nil
	})
}

// untyped reports a rule or an action expression of m with a type.
func untyped(m *core.Module) error {
	return eachType(m, func(what string, has bool) error {
		if has {
			return fmt.Errorf("%s has a type", what)
		}
		return nil
	})
}

// eachType calls f for each rule of m and each action expression of m but a lambda, with whether
// it carries a type, and returns the first error of f.
func eachType(m *core.Module, f func(what string, has bool) error) error {
	var err error
	for _, r := range m.Rules {
		if err == nil {
			err = f("rule "+r.Name, r.Type != nil)
		}
		core.EachActionExpr(r, func(t core.ActionExpr) {
			core.WalkActionExpr(t, func(x core.ActionExpr) {
				if _, lambda := x.(*core.Lambda); err == nil && !lambda {
					err = f(fmt.Sprintf("rule %s: action expression %s", r.Name, core.ActionExprString(x)), core.TypeOf(x) != nil)
				}
			})
		})
	}
	return err
}
