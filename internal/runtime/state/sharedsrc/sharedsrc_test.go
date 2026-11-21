package sharedsrc

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// root is the root of the module, from the directory of this package.
var root = filepath.Join("..", "..", "..", "..")

// TestCopiesCurrent checks that every shared file has its copy in the engine, as Copy makes it from
// the library's source now, and that the engine holds no copy of a file that is not shared.
func TestCopiesCurrent(t *testing.T) {
	for _, name := range Files {
		src, err := os.ReadFile(filepath.Join(root, LibDir, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := Copy(name, src)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, StateDir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("the copy of %s is out of date; run go generate ./%s/sharedsrc", name, StateDir)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, StateDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || slices.Contains(Files, n) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, StateDir, n))
		if err != nil {
			t.Fatal(err)
		}
		if IsCopy(data) {
			t.Errorf("%s/%s is a copy of a file that is no longer shared; run go generate ./%s/sharedsrc", StateDir, n, StateDir)
		}
	}
}

// TestCopyChangesOnlyThePackageClause checks the transform: the copy is the source with the header
// before it and package state in place of package libgo, and a source that does not start with its
// package clause is refused.
func TestCopyChangesOnlyThePackageClause(t *testing.T) {
	got, err := Copy("x.go", []byte("package libgo\n\n// package libgo stays in a comment.\nfunc f() {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Header("x.go") + "package state\n\n// package libgo stays in a comment.\nfunc f() {}\n"
	if string(got) != want {
		t.Errorf("Copy = %q, want %q", got, want)
	}
	if _, err := Copy("x.go", []byte("//go:build x\n\npackage libgo\n")); err == nil {
		t.Error("Copy accepts a source that does not start with its package clause")
	}
}
