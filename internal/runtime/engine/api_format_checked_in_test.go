package engine_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// unformattedFixtures are checked-in grammars that are unformatted on purpose.
// There are none; add a file here with the reason when a test needs one.
var unformattedFixtures = map[string]string{}

// Every checked-in grammar is what `sxir fmt` writes. Run `sxir fmt -w` on a
// file reported here.
func TestCheckedInGrammarsAreFormatted(t *testing.T) {
	chdirModuleRoot(t)
	// Only tracked files: untracked scratch grammars are not checked in.
	out, err := exec.Command("git", "ls-files", "*.sxir").Output()
	if err != nil {
		t.Skip("git ls-files:", err)
	}
	paths := strings.Fields(string(out))
	if len(paths) == 0 {
		t.Fatal("no grammars found")
	}
	for _, path := range paths {
		if _, ok := unformattedFixtures[path]; ok {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g, err := enginetest.ParseGrammar(string(src))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if fmt.Format(g) != string(src) {
			t.Errorf("%s is not formatted: run sxir fmt -w %s", path, path)
		}
	}
}
