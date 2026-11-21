//go:build sxir_long

package complexity_test

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/analysis/complexity"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// An equivEdit replaces the first old by new in the function fn of rewrite.go.
type equivEdit struct{ fn, old, new string }

// equivMutants break each law of the view in rewrite.go: at every place where the law rewrites an
// expression, the mutant writes an expression that matches otherwise, so a check that reaches the
// rewrite sees the difference. They break what the rewrites give, not the conditions under which they
// apply, which TestNormalView holds to the counterexamples of each law.
var equivMutants = []struct {
	law   complexity.Law
	edits []equivEdit
}{
	{complexity.N1, []equivEdit{
		// S1 splices the inner sequence without its first item; C2 flattens the inner choice without
		// its first alternative.
		{"seq", "items = append(items, inner.Items...)", "items = append(items, inner.Items[1:]...)"},
		{"choice", "for _, a := range inner.Alts {", "for _, a := range inner.Alts[1:] {"},
	}},
	{complexity.N2, []equivEdit{
		// C3 drops the alternative that always succeeds with those after it.
		{"choice", "b.count(N2)\n", "b.count(N2)\n\t\t\t\tkept = kept[:len(kept)-1]\n"},
	}},
	{complexity.N3, []equivEdit{
		// C1 and C5 drop the earlier alternative with the one it covers.
		{"choice", "b.count(N3)\n", "b.count(N3)\n\t\t\tkept = kept[:len(kept)-1]\n"},
	}},
	{complexity.N4, []equivEdit{
		// C4 replaces a bottom alternative by a top; S2 drops the bottom with what follows it.
		{"choice", "b.count(N4)\n", "b.count(N4)\n\t\t\tkept = append(kept, &core.Succeed{})\n"},
		{"seq", "items, changed = items[:i+1], true", "items, changed = items[:i], true"},
	}},
	{complexity.N5, []equivEdit{
		// K2 and K1 give the lookahead of the other sign.
		{"lookahead", "Neg: l.Neg, Site: l.Site}", "Neg: !l.Neg, Site: l.Site}"},
		{"lookahead", "b.count(N5)\n\t\t\treturn in\n", "b.count(N5)\n\t\t\treturn &core.Look{Body: in.Body, Site: l.Site}\n"},
		{"lookahead", "return &core.Look{Body: in.Body, Site: l.Site}\n\t\t}", "return &core.Look{Body: in.Body, Neg: true, Site: l.Site}\n\t\t}"},
	}},
	{complexity.N6, []equivEdit{
		// T2 turns the cuts it drops into bottoms; V1 drops the expression of the recovery with it.
		{"looseCutsDroppedIn", "return b.built(&core.Succeed{}, x)", "return b.built(&core.FailNode{}, x)"},
		{"looseCutsDroppedIn", "changed = true\n\t\t\t\tcontinue", "changed = true\n\t\t\t\titems = append(items, &core.FailNode{})\n\t\t\t\tcontinue"},
		{"dropCuts", "return b.seqOf(s, s.Items[:len(s.Items)-1], tuple)", "return b.seqOf(s, append(slices.Clone(s.Items[:len(s.Items)-1]), &core.FailNode{}), tuple)"},
		{"recover", "return b.dropped(body, r)", "return &core.FailNode{}"},
	}},
	{complexity.N7, []equivEdit{
		// The label is dropped with its expression.
		{"rewrite", "return b.dropped(body, x)", "return &core.Succeed{}"},
	}},
	{complexity.N8, []equivEdit{
		// The merged class leaves out the characters of the first alternative.
		{"mergeChars", "b.class(ranges)", "b.class(ranges[1:])"},
	}},
	{complexity.N9, []equivEdit{
		// X? X* becomes X?, R3 and R4 keep the first repetition with at most one iteration, and R5
		// keeps the minimum of the star.
		{"adjacent", "b.count(N9)\n\t\t\treturn ry", "b.count(N9)\n\t\t\treturn x"},
		{"adjacent", "b.count(N9)\n\t\t\treturn rx", "b.count(N9)\n\t\t\tc := *rx\n\t\t\tc.Max = 1\n\t\t\treturn b.built(&c, rx)"},
		{"adjacent", "c.Min = 1", "c.Min = 0"},
	}},
	{complexity.N10, []equivEdit{
		// R6 gives a plus in place of a star, and an option in place of a star of an option.
		{"nestedRepeat", "c.Min = 0", "c.Min = 1"},
		{"nestedRepeat", "c.Body = in.Body", "c.Body, c.Max = in.Body, 1"},
	}},
	{complexity.N11, []equivEdit{
		// R7 unrolls without the separator, and the absorption keeps the first repetition.
		{"unroll", "[]*core.Alt{{Body: x}, {Body: y}}", "[]*core.Alt{{Body: x}}"},
		{"unroll", "b.count(N11)\n\t\treturn ry", "b.count(N11)\n\t\treturn rx"},
	}},
	{complexity.N12, []equivEdit{
		// K4 keeps X in place of !X X; C6 factors the run without its last alternative.
		{"adjacent", "b.count(N12)\n\t\treturn &core.FailNode{}", "b.count(N12)\n\t\treturn y"},
		{"factor", "for _, a := range alts[i:j] {", "for _, a := range alts[i : j-1] {"},
	}},
	{complexity.N13, []equivEdit{
		// An inlined call never matches.
		{"rewrite", "b.count(N13)\n\t\t\t\treturn body", "b.count(N13)\n\t\t\t\treturn &core.FailNode{}"},
	}},
}

// equivDead are subjects in which a law rewrites only what no parse reaches, so that its mutant
// matches as the grammar does: in the shape with a cut in a skip, the alternative !"q" "q" that K4
// rewrites follows a recovery whose skip always matches.
var equivDead = map[complexity.Law][]string{complexity.N12: {"shape_cut_in_a_skip"}}

// TestViewEquivalenceMutants is the proof that TestViewEquivalence checks every law where it
// applies: with a source overlay of the mutant of each law (equivMutants), the subtest of the view of
// that law alone reports a difference between the view and the grammar on every subject whose view
// the law rewrites, but those in equivDead, and the views of the other laws are not checked.
func TestViewEquivalenceMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, "internal/analysis/complexity", "internal/testing/complexitytest", "testdata/grammars", "examples")
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	if got, want := len(equivMutants), len(equivLaws); got != want {
		t.Fatalf("%d mutants for %d laws", got, want)
	}
	// The subjects whose views each law rewrites, which the subtest of the law must fail on.
	applies := map[complexity.Law][]string{}
	for _, s := range equivSubjects(t) {
		g, err := parser.Parse(s.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range equivViews()[2:] {
			_, view, err := complexity.NormalView(g, v.opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			l := v.opts.Laws[0]
			if view.Applied[l] > 0 {
				applies[l] = append(applies[l], s.name)
			}
		}
	}
	path, src := mutanttest.Source(t, "internal/analysis/complexity/rewrite.go")
	var batch []mutanttest.Mutant
	for l, dead := range equivDead {
		for _, s := range dead {
			if !slices.Contains(applies[l], s) {
				t.Fatalf("%s does not rewrite the subject %s that it is said to rewrite where no parse reaches", l, s)
			}
		}
		applies[l] = slices.DeleteFunc(applies[l], func(s string) bool { return slices.Contains(dead, s) })
	}
	for _, m := range equivMutants {
		if len(applies[m.law]) == 0 {
			t.Fatalf("no subject applies %s", m.law)
		}
		text := string(src)
		for _, e := range m.edits {
			var ok bool
			if text, ok = codegentest.MutateFunc(text, e.fn, e.old, e.new); !ok {
				t.Fatalf("%s: rewrite.go has no %s with %q", m.law, e.fn, e.old)
			}
		}
		names := make([]string, len(applies[m.law]))
		for i, n := range applies[m.law] {
			names[i] = regexp.QuoteMeta(n)
		}
		batch = append(batch, mutanttest.Mutant{Replace: map[string]string{path: text}, Pkgs: []string{"./internal/analysis/complexity"},
			Run: fmt.Sprintf("^TestViewEquivalence$/^(%s)$/^%s$", strings.Join(names, "|"), m.law)})
	}
	b := mutanttest.NewBatch(t, codegentest.ModuleRoot(), append(os.Environ(), "GOFLAGS="), nil, batch)
	for i, m := range equivMutants {
		t.Run(string(m.law), func(t *testing.T) {
			t.Parallel()
			out, err := b.Test(i)
			if err == nil {
				t.Fatalf("TestViewEquivalence passes with the mutant of %s:\n%s", m.law, out)
			}
			failed := failedWithDifference(string(out))
			for _, s := range applies[m.law] {
				if !slices.Contains(failed, subtestName(s)+"/"+string(m.law)) {
					t.Errorf("the mutant of %s: the subject %s reports no difference", m.law, s)
				}
			}
			if t.Failed() {
				t.Logf("output:\n%s", out)
			}
		})
	}
}

// failedWithDifference returns the subtests of TestViewEquivalence, as subject/view, that failed
// reporting a difference between a view and its grammar, in the output of go test.
func failedWithDifference(out string) []string {
	var failed []string
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		name, ok := strings.CutPrefix(strings.TrimSpace(l), "--- FAIL: TestViewEquivalence/")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(name, " ")
		for _, d := range lines[i+1:] {
			if !strings.HasPrefix(d, "        ") {
				break
			}
			if strings.Contains(d, equivDifference) {
				failed = append(failed, name)
				break
			}
		}
	}
	return failed
}
