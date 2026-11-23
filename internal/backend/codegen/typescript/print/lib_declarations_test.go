package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestTSLibDeclarations checks that the library alone compiles to declarations with the strictest
// checks, as a project that emits declarations compiles it: the classes that the layers of the parser
// make (trackedLayer, observedLayer) are anonymous in the declarations, which TypeScript refuses when
// a class they extend has a private or protected member.
func TestTSLibDeclarations(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs tsc")
	}
	tsc := tooltest.RequireTSC(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.ts"), libFiles(t)[0].Content, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tsc, "--declaration", "--emitDeclarationOnly", "--outDir", "out", "--target", "es2020", "--lib", "es2020", "--strict",
		"--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch",
		"--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess",
		"--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "lib.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("tsc: %v\n%s", err, out)
	}
}
