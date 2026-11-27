package print_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTypesCoverEveryMappingRow checks that the corpora that compare the typed Go values
// with the engine's, and the direct route with the converted one, convert types of every row of the
// Go table of typed values (codegentest.GoTypedRows): the corpus of TestGeneratedTypes, and for the
// rows of grammars with #missing the cases of TestGeneratedTypedParsersInsertMissingTokens.
func TestGeneratedTypesCoverEveryMappingRow(t *testing.T) {
	t.Parallel()
	cases := append(codegentest.GeneratedTypesCases(t), codegentest.TypedInsertionCorpus()...)
	codegentest.GoTypedRows.RequireCovered(t, cases)
	_, first := codegentest.GoTypedRows.Uncovered(t, cases)
	for row, i := range first {
		t.Logf("%s: %s", row, cases[i].Name)
	}
}

// TestTypedRowsWitnessValues checks that a grammar witnesses a row of the tables of typed values only
// through the values of its inputs (codegentest.TypedRows.Witnesses): one whose inputs all fail
// witnesses none of the rows of its types, and one whose values hold no missing value witnesses the
// other rows but not that of missing.
func TestTypedRowsWitnessValues(t *testing.T) {
	t.Parallel()
	const src = `type P = { A node[string]; H missing }
@start
rule main P = a:"x" h:hole $$ => P{A: $a, H: $h}
rule hole missing = m:"]" #missing => $m.(missing)`
	missing := "`missing` (from `#missing`)"
	for _, c := range []struct {
		inputs []string
		want   bool
	}{{[]string{"y", "x]"}, false}, {[]string{"x"}, true}} {
		cs := codegentest.Case{Name: "a missing field", Src: src, Inputs: c.inputs}
		rows := codegentest.GoTypedRows.Witnesses(t, cs)
		if !c.want && len(rows) != 0 {
			t.Errorf("inputs %q, which all fail, witness %q", c.inputs, rows)
		}
		if c.want != slices.Contains(rows, missing) {
			t.Errorf("inputs %q witness %q, want the missing row %v", c.inputs, rows, c.want)
		}
		if all := codegentest.GoTypedRows.RowsOf(t, cs); !slices.Contains(all, missing) {
			t.Fatalf("the grammar maps by %q, without the missing row", all)
		}
	}
	// A value without a missing value: the field of the missing type is never set.
	cs := codegentest.Case{Name: "a struct without missing values", Src: `type P = { A node[string]; H *missing }
@start
rule main P = a:"x" $$ => P{A: $a}
rule hole missing = m:"]" #missing => $m.(missing)`, Inputs: []string{"x"}}
	if all := codegentest.GoTypedRows.RowsOf(t, cs); !slices.Contains(all, missing) {
		t.Fatalf("the grammar without missing values maps by %q, without the missing row", all)
	}
	if rows := codegentest.GoTypedRows.Witnesses(t, cs); slices.Contains(rows, missing) || !slices.Contains(rows, "Struct type `T`") {
		t.Errorf("a value without missing values witnesses %q, want the struct row and not the missing row", rows)
	}
}
