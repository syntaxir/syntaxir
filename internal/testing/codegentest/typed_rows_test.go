package codegentest

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestTypedRowsNameTheSpecRows holds GoTypedRows and TSTypedRows to the tables of typed values of
// the specification: each row of a table is named by exactly one row of its TypedRows, by a prefix
// of its first column, and each row of a TypedRows names a row of its table. A row added to a
// table so fails until the classification of typed values (RowsOf) maps a shape onto it.
func TestTypedRowsNameTheSpecRows(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(ModuleRoot(), "spec", "runtime", "generated-parsers.md"))
	if err != nil {
		t.Fatal(err)
	}
	section := string(data)
	start, end := strings.Index(section, "\n### Typed Values\n"), strings.Index(section, "\n### Recognition\n")
	if start < 0 || end < start {
		t.Fatal("no section Typed Values before Recognition in generated-parsers.md")
	}
	tables := specTables(section[start:end])
	if len(tables) != 2 {
		t.Fatalf("got %d tables of typed values, want the Go and the TypeScript table", len(tables))
	}
	for i, rows := range []TypedRows{GoTypedRows, TSTypedRows} {
		named := map[string]int{}
		for _, first := range tables[i] {
			var by []string
			for _, r := range rows.Rows {
				if strings.HasPrefix(first, r) {
					by = append(by, r)
					named[r]++
				}
			}
			if len(by) != 1 {
				t.Errorf("%s table: row %q is named by %d rows (%v), want one", rows.Name, first, len(by), by)
			}
		}
		for _, r := range rows.Rows {
			if named[r] == 0 {
				t.Errorf("%s table: %q names no row", rows.Name, r)
			}
		}
	}
}

// cell splits a row of a Markdown table at the bars that are not escaped.
var cell = regexp.MustCompile(`\s*(?:[^|\\]|\\.)*`)

// specTables returns the first columns of the rows of each Markdown table in text, without the
// header and the delimiter row, with escaped bars unescaped.
func specTables(text string) [][]string {
	var tables [][]string
	var rows []string
	inTable := false
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") {
			if inTable {
				tables = append(tables, rows[2:])
				rows, inTable = nil, false
			}
			continue
		}
		inTable = true
		first := cell.FindString(line[1:])
		rows = append(rows, strings.ReplaceAll(strings.TrimSpace(first), `\|`, "|"))
	}
	if inTable {
		tables = append(tables, rows[2:])
	}
	return slices.Clip(tables)
}
