package codegentest

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWithCasesNamesFileInTSCOutput pins that a tsc failure names the case of each file it reports,
// whether tsc prints the path of a file as it was given (absolute) or relative to the directory it
// runs in, which is how tsc prints the files of RunTSC.
func TestWithCasesNamesFileInTSCOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "root")
	rel := filepath.Join("shard1", "g0.ts")
	abs := filepath.Join(dir, rel)
	other := filepath.Join(dir, "shard1", "g10.ts")
	labels := map[string]string{abs: "caseA", other: "caseB"}
	batch := []string{abs, other}

	relative := []byte(rel + "(1,7): error TS2322: bad type.\n" + filepath.Join("shard1", "g10.ts") + "(2,3): error TS2304: missing.\n")
	got := withCases(relative, dir, batch, labels)
	if !strings.Contains(got, rel+" (caseA)(1,7)") {
		t.Errorf("relative tsc output: caseA not named; got:\n%s", got)
	}
	if !strings.Contains(got, filepath.Join("shard1", "g10.ts")+" (caseB)(2,3)") {
		t.Errorf("relative tsc output: caseB not named; got:\n%s", got)
	}

	absolute := []byte(abs + "(1,7): error TS2322: bad type.\n")
	got = withCases(absolute, dir, batch, labels)
	if !strings.Contains(got, abs+" (caseA)") {
		t.Errorf("absolute tsc output: case not named; got:\n%s", got)
	}
	if strings.Contains(got, abs+" (caseA) (caseA)") {
		t.Errorf("absolute path named twice; got:\n%s", got)
	}
}
