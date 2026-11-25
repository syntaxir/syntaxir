package check

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/bench/synth"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/factstest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// depthShape is a synthetic grammar whose groups nest n deep, the series on which a walk of the
// operand of every enclosing operator grows with the square of the depth.
func depthShape(n int) synth.Shape {
	return synth.Shape{Rules: 16, Alts: 4, Depth: n, LeftRecursion: 4, Seed: 3}
}

// TestValueTypesKeptByBottomAnswers requires the value inference, which keeps the answers of
// bottom once the rules of type _|_ are final, to infer what an inference that walks every
// operand again infers: the same rule types, errors, node member accesses and recorded types, on
// the repository's grammars and on grammars of nested groups.
func TestValueTypesKeptByBottomAnswers(t *testing.T) {
	t.Parallel()
	sources := map[string]string{}
	for _, path := range factstest.Corpus {
		sources[path] = factstest.Read(t, path)
	}
	paths, err := filepath.Glob(filepath.Join(factstest.Root(t), "testdata/grammars/*/*.sxir"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no repository grammars: %v", err)
	}
	for _, path := range paths {
		rel, _ := filepath.Rel(factstest.Root(t), path)
		sources[rel] = factstest.Read(t, rel)
	}
	for _, n := range []int{6, 12, 24} {
		sources[fmt.Sprintf("depth %d", n)] = synth.Grammar(depthShape(n))
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g, err := parser.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			g = starttest.MarkAll(g)
			names := desugar.InlineNames{}
			m, err := Check(g, Options{}, nil, names, nil)
			if err != nil {
				t.Fatal(names.Rewrite(err))
			}
			p := m.Checker()
			infer := func(walk bool) (*ValueInference, map[string]Ty, string, map[*ast.FieldAccess]bool, *ValueRecord) {
				members, rec := map[*ast.FieldAccess]bool{}, newValueRecord()
				v := newValueInference(p, nil, members, rec)
				if walk {
					// A set of rules makes bottom walk every operand it is asked about.
					v.bottoms = p.bottomRules(nil)
				}
				out, errs := v.ruleValues()
				return v, out, fmt.Sprint(errs), members, rec
			}
			kept, keptOut, keptErrs, keptMembers, keptRec := infer(false)
			walked, walkedOut, walkedErrs, walkedMembers, walkedRec := infer(true)
			if walked.nevers != nil {
				t.Fatalf("the walking inference kept %d answers", len(walked.nevers))
			}
			if !reflect.DeepEqual(keptOut, walkedOut) {
				t.Errorf("rule types differ:\nkept   %v\nwalked %v", keptOut, walkedOut)
			}
			if keptErrs != walkedErrs {
				t.Errorf("errors differ:\nkept   %s\nwalked %s", keptErrs, walkedErrs)
			}
			if !reflect.DeepEqual(keptMembers, walkedMembers) {
				t.Errorf("node member accesses differ")
			}
			if !reflect.DeepEqual(keptRec, walkedRec) {
				t.Errorf("recorded types differ")
			}
			// Every kept answer is the answer of a walk.
			for e, b := range kept.nevers {
				if w := walked.bottom(e); w != b {
					t.Errorf("bottom of %T: kept %v, walked %v", e, b, w)
				}
			}
		})
	}
}

// TestValueTypesStepsLinearInDepth requires the steps of the check of grammars of nested groups
// (package cancel) to grow linearly with their text as the depth doubles: the increments of the
// steps from n to 2n and from 2n to 4n, scaled to an exact doubling of the text, are within the
// band of a linear slope (2^1.15), which a walk of the operand of every enclosing operator
// exceeds.
func TestValueTypesStepsLinearInDepth(t *testing.T) {
	t.Parallel()
	var steps, sizes [3]float64
	for i, n := range []int{6, 12, 24} {
		src := synth.Grammar(depthShape(n))
		g, err := parser.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		control := &cancel.Control{Ctx: context.Background()}
		names := desugar.InlineNames{}
		if _, err := Check(g, Options{}, nil, names, control); err != nil {
			t.Fatal(names.Rewrite(err))
		}
		steps[i], sizes[i] = float64(control.Steps), float64(len(src))
	}
	ratio := (steps[2] - steps[1]) / (steps[1] - steps[0]) * 2 * (sizes[1] - sizes[0]) / (sizes[2] - sizes[1])
	if limit := math.Pow(2, 1.15); !(ratio <= limit) {
		t.Errorf("steps %v at sizes %v: doubling ratio %.3f, more than the linear %.3f", steps, sizes, ratio, limit)
	}
}
