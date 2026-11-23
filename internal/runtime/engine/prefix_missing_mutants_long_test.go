//go:build sxir_long

package engine_test

import "testing"

// TestPrefixInsertionMutantsFail is the negative control of the tests of prefix parsing with
// missing-token insertion: each mutant breaks the prefix parse in a copy of its source, and the
// test named for it must fail when it builds with the copy in place of the file. The file is not
// printed, but runPrintedMutants mutates any source file of the module alike.
func TestPrefixInsertionMutantsFail(t *testing.T) {
	const here = "."
	runPrintedMutants(t, "internal/runtime/engine/program_parse_prefix.go", []printedMutant{
		{"a prefix parse inserts no tokens",
			[]printedEdit{{"\tif o.MissingTokens > 0 && active.HasMissing {\n", "\tif false {\n"}},
			"^TestPrefixInsertionMatchesParse$", []string{here}},
		{"a prefix parse that inserts reports no errors",
			[]printedEdit{{"\tif len(p.Recovered) > 0 {\n", "\tif false {\n"}},
			"^TestPrefixInsertionAtTheEndOfThePrefix$", []string{here}},
	})
}
