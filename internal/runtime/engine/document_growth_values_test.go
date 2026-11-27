package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A Document tells growth step results apart by their whole values (spec/runtime/parsing.md,
// Evaluations of Rules): results with the same end whose trees differ only below their roots, in
// children, positions or text, keep identities of their own, so the Document returns the tree of a
// parse from scratch on every engine backend.
func TestDocumentTellsGrowthResultsApart(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, codegentest.GrowthValuesGrammar)
	type edit = struct {
		Start, End int
		Text       string
	}
	cases := []struct {
		text  string
		edits []edit
	}{
		{codegentest.GrowthValuesText, codegentest.GrowthValuesEdits},
		{"aba", []edit{{0, 1, "ab"}}},
		{"aba", []edit{{3, 3, ""}, {2, 2, ""}, {3, 3, "bb"}, {5, 5, "a"}, {1, 2, ""}}},
		{"aabaac", []edit{{5, 6, ""}, {2, 3, "cb"}, {5, 5, "bb"}}},
	}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		o := ParseOptions{Backend: b}
		for _, c := range cases {
			doc, err := prog.NewDocumentWith("main", c.text, o)
			if err != nil {
				t.Fatal(err)
			}
			doc.Parse()
			for i, ed := range c.edits {
				if err := doc.Edit(ed.Start, ed.End, ed.Text); err != nil {
					t.Fatal(err)
				}
				n, err := doc.Parse()
				fn, ferr := prog.ParseWith("main", doc.Text(), o)
				if got, want := dump(t, n, err), dump(t, fn, ferr); got != want {
					t.Errorf("%v, %q, edit %d to %q:\n got  %s\n want %s", b, c.text, i, doc.Text(), got, want)
				}
			}
		}
	}
}
