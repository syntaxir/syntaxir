package main

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readTree returns the files below dir by their slash paths relative to dir, or nil if dir does not
// exist.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// genTree runs sxir gen with args into a new directory and returns the files it wrote.
func genTree(t *testing.T, args ...string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if _, err := runCLI(t, "", append([]string{"gen", "-o", dir}, args...)...); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return readTree(t, dir)
}

// sxir gen -lang go,ts writes DIR/go and DIR/ts equal to two single-language runs; a qualified flag
// applies to its language only and wins over the unqualified one, and -go.o and -ts.o give a
// language its own directory.
func TestGenSeveralLanguages(t *testing.T) {
	g := writeFile(t, "kv.sxir", kv)
	common := []string{"-g", g, "-lib", "import", "-recognize", "-source-info"}
	dir := t.TempDir()
	if _, err := runCLI(t, "", append([]string{"gen", "-lang", "go,ts", "-o", dir, "-module", "example.com/kv", "-lib-module", "./lib.ts", "-go.types", "-ts.max-depth", "7", "-max-depth", "3"}, common...)...); err != nil {
		t.Fatal(err)
	}
	goAlone := genTree(t, append([]string{"-module", "example.com/kv", "-types", "-max-depth", "3"}, common...)...)
	tsAlone := genTree(t, append([]string{"-lang", "ts", "-lib-module", "./lib.ts", "-max-depth", "7"}, common...)...)
	if got := readTree(t, filepath.Join(dir, "go")); !maps.Equal(got, goAlone) {
		t.Errorf("DIR/go differs from the Go run alone: %v and %v", slicesOf(got), slicesOf(goAlone))
	}
	if got := readTree(t, filepath.Join(dir, "ts")); !maps.Equal(got, tsAlone) {
		t.Errorf("DIR/ts differs from the TypeScript run alone: %v and %v", slicesOf(got), slicesOf(tsAlone))
	}
	if _, ok := goAlone["types/types.go"]; !ok {
		t.Errorf("-go.types gave the Go target no types")
	}
	own := filepath.Join(t.TempDir(), "own")
	parent := t.TempDir()
	if _, err := runCLI(t, "", append([]string{"gen", "-lang", "ts,go", "-o", parent, "-ts.o", own, "-module", "example.com/kv", "-lib-module", "./lib.ts", "-go.types", "-ts.max-depth", "7", "-max-depth", "3"}, common...)...); err != nil {
		t.Fatal(err)
	}
	if got := readTree(t, own); !maps.Equal(got, tsAlone) {
		t.Errorf("-ts.o holds other files than the TypeScript run alone: %v", slicesOf(got))
	}
	if got := readTree(t, filepath.Join(parent, "ts")); got != nil {
		t.Errorf("-ts.o left files in DIR/ts: %v", slicesOf(got))
	}
	if got := readTree(t, filepath.Join(parent, "go")); !maps.Equal(got, goAlone) {
		t.Errorf("DIR/go differs from the Go run alone beside -ts.o")
	}
}

// -lib only with several languages writes the library of each.
func TestGenSeveralLanguagesLibOnly(t *testing.T) {
	dir := t.TempDir()
	if _, err := runCLI(t, "", "gen", "-lang", "go,ts", "-lib", "only", "-o", dir); err != nil {
		t.Fatal(err)
	}
	if got := readTree(t, filepath.Join(dir, "go")); !maps.Equal(got, genTree(t, "-lib", "only")) {
		t.Errorf("DIR/go is not the Go library alone")
	}
	if got := readTree(t, filepath.Join(dir, "ts")); !maps.Equal(got, genTree(t, "-lang", "ts", "-lib", "only")) {
		t.Errorf("DIR/ts is not the TypeScript library alone")
	}
}

// sxir gen -stream generates the stream of each listed language: Go ParseStream and TypeScript
// createStream, as each language generated alone.
func TestGenStreamSeveralLanguages(t *testing.T) {
	g := writeFile(t, "items.sxir", "@start\nrule main = item* #stream $$\nrule item = [a-z]+ \",\"\n")
	dir := t.TempDir()
	if _, err := runCLI(t, "", "gen", "-lang", "go,ts", "-o", dir, "-module", "example.com/items", "-g", g, "-stream"); err != nil {
		t.Fatal(err)
	}
	goAlone := genTree(t, "-module", "example.com/items", "-g", g, "-stream")
	tsAlone := genTree(t, "-lang", "ts", "-g", g, "-stream")
	if got := readTree(t, filepath.Join(dir, "go")); !maps.Equal(got, goAlone) {
		t.Errorf("DIR/go differs from the Go run alone: %v and %v", slicesOf(got), slicesOf(goAlone))
	}
	if got := readTree(t, filepath.Join(dir, "ts")); !maps.Equal(got, tsAlone) {
		t.Errorf("DIR/ts differs from the TypeScript run alone: %v and %v", slicesOf(got), slicesOf(tsAlone))
	}
	if !strings.Contains(tsAlone["parser.ts"], "export function createStream(") {
		t.Error("the TypeScript parser has no createStream")
	}
	if _, err := runCLI(t, "", "gen", "-lang", "ts", "-o", t.TempDir(), "-g", writeFile(t, "kv.sxir", kv), "-stream"); err == nil || !strings.Contains(err.Error(), "needs a #stream repetition") {
		t.Errorf("a TypeScript stream without #stream: %v", err)
	}
}

// The errors of several languages: a flag that a listed language does not support names its
// qualified form, a qualified flag needs its language listed, a language is listed once, and a
// target that cannot write makes the command write nothing for any target.
func TestGenSeveralLanguagesErrors(t *testing.T) {
	g := writeFile(t, "kv.sxir", kv)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-lang", "go,ts", "-module", "example.com/kv", "-typed-only"}, "typed-only output needs typed output (-types)"},
		{[]string{"-lang", "go", "-module", "example.com/kv", "-ts.types"}, "-ts.types is a flag of the ts target, and -lang does not list ts"},
		{[]string{"-lang", "go,go", "-module", "example.com/kv"}, "-lang lists go twice"},
		{[]string{"-lang", "go,ts", "-lib", "import", "-module", "example.com/kv"}, "target ts: -lib-module"},
		{[]string{"-lang", "go,ts", "-ts.lib", "only"}, "target go: -module"},
		{[]string{"-lang", "go,ts", "-ts.lib", "only", "-module", "example.com/kv"}, "target ts: -g cannot be combined with -lib only"},
		{[]string{"-lang", "go,ts", "-go.lib", "only", "-ts.lib", "only", "-go.types"}, "target go: -types cannot be combined with -lib only"},
	} {
		args := []string{"gen", "-o", t.TempDir()}
		if !strings.Contains(strings.Join(c.args, " "), "-go.lib only") {
			args = append(args, "-g", g)
		}
		_, err := runCLI(t, "", append(args, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want an error with %q", c.args, err, c.want)
		}
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ts", "parser.ts"), []byte("// mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runCLI(t, "", "gen", "-lang", "go,ts", "-g", g, "-o", dir, "-module", "example.com/kv")
	if err == nil || !strings.Contains(err.Error(), "was not generated by sxir") {
		t.Errorf("a TypeScript directory that refuses the files: %v", err)
	}
	if got := readTree(t, filepath.Join(dir, "go")); got != nil {
		t.Errorf("the Go target wrote %v although the TypeScript target failed", slicesOf(got))
	}
}

// slicesOf returns the paths of files, for messages.
func slicesOf(files map[string]string) []string {
	var paths []string
	for p := range maps.Keys(files) {
		paths = append(paths, p)
	}
	return paths
}
