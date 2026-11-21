package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoUnaliasedNameCollisions checks the package name criterion of the release package
// layout record: no file imports two packages of the same name without an alias. Packages
// may repeat a name across paths; the accepted repeats are listed in the record. The package
// name of a module import is the package clause of its directory, and the name of any other
// import is the last element of its path, which is the name the Go tools use for those packages.
// Fixtures under testdata are not checked.
func TestNoUnaliasedNameCollisions(t *testing.T) {
	ls := exec.Command("git", "-C", moduleRoot, "ls-files", "-z", "*.go")
	ls.Env = repoEnv()
	out, err := ls.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	checked := 0
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p == "" || strings.Contains(p, "testdata/") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(moduleRoot, p), nil, parser.ImportsOnly)
		if err != nil {
			continue // listed but deleted in the working tree
		}
		checked++
		names := map[string][]string{}
		for _, im := range f.Imports {
			if im.Name != nil {
				continue // an alias, or a blank or dot import
			}
			ip := strings.Trim(im.Path.Value, `"`)
			name := path.Base(ip)
			if rest, ok := strings.CutPrefix(ip, modulePath+"/"); ok {
				name = packageClauseName(t, filepath.Join(moduleRoot, filepath.FromSlash(rest)), name)
			}
			names[name] = append(names[name], ip)
		}
		for name, paths := range names {
			if len(paths) > 1 {
				t.Errorf("%s imports %d packages named %s without an alias: %s", p, len(paths), name, strings.Join(paths, ", "))
			}
		}
	}
	if checked < 500 {
		t.Fatalf("checked only %d files; the listing of tracked files is wrong", checked)
	}
}

// packageClauseName returns the package name declared by the non-test Go files of dir, or
// fallback when dir has none.
func packageClauseName(t *testing.T, dir, fallback string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		return f.Name.Name
	}
	return fallback
}
