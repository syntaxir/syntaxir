package archtest

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSourcesAreGofmted checks that every tracked Go file is formatted as gofmt prints it, with
// go/format and no simplification, so that the check agrees with `gofmt -l`. A file that differs
// is listed; `gofmt -w` on it fixes the file. No tracked Go file is exempt: none is printed by a
// generator or kept unformatted on purpose.
func TestSourcesAreGofmted(t *testing.T) {
	ls := exec.Command("git", "-C", moduleRoot, "ls-files", "-z", "*.go")
	ls.Env = repoEnv()
	out, err := ls.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	checked := 0
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p == "" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(moduleRoot, p))
		if err != nil {
			continue // listed but deleted in the working tree
		}
		checked++
		if !gofmted(src) {
			t.Errorf("%s: not formatted as gofmt prints it (run gofmt -w on it)", p)
		}
	}
	if checked < 500 {
		t.Fatalf("checked only %d files; the listing of tracked files is wrong", checked)
	}
}

// TestSourcesAreGofmtedNegative checks that the test's predicate tells a formatted source from one
// that gofmt would change.
func TestSourcesAreGofmtedNegative(t *testing.T) {
	const formatted = "package p\n\nvar x = 1\n"
	const unformatted = "package p\n\nvar x=1\n"
	if !gofmted([]byte(formatted)) {
		t.Error("a formatted source is reported unformatted")
	}
	if gofmted([]byte(unformatted)) {
		t.Error("an unformatted source is reported formatted")
	}
}

// gofmted reports whether src is what go/format prints for it.
func gofmted(src []byte) bool {
	out, err := format.Source(src)
	return err == nil && bytes.Equal(out, src)
}
