package compiler_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
)

// Generate gives each target the files that GenerateGo or GenerateTypeScript gives it alone, under
// the options of the call, and names the backend in the dumps of each.
func TestGenerateSeveralTargets(t *testing.T) {
	src := grammar.FromString("two.sxir", "@start\nrule main = \"a\" B\nrule B = \"b\"\n")
	goC := compiler.GoConfig{Module: "example.com/m", Lib: compiler.LibImport, Types: true}
	tsC := compiler.TSConfig{Types: true, Recognize: true}
	opts := []compiler.Option{compiler.WithSourceInfo(), compiler.WithOptimize(compiler.Optimize{Profile: "size"})}
	var dumps bytes.Buffer
	out, err := compiler.Generate(src, []compiler.Target{goC, tsC},
		append(opts, compiler.WithDebug(compiler.Debug{PrintAfter: []string{"elaborate"}, Output: &dumps}))...)
	if err != nil {
		t.Fatal(err)
	}
	goFiles, err := compiler.GenerateGo(src, goC, opts...)
	if err != nil {
		t.Fatal(err)
	}
	tsFiles, err := compiler.GenerateTypeScript(src, tsC, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Target != compiler.Target(goC) || out[1].Target != compiler.Target(tsC) {
		t.Fatalf("Generate returns the targets %v", out)
	}
	if !reflect.DeepEqual(out[0].Files, goFiles) || !reflect.DeepEqual(out[1].Files, tsFiles) {
		t.Errorf("the files of a target differ from those it gets alone")
	}
	for _, label := range []string{"go", "ts"} {
		if !strings.Contains(dumps.String(), " ["+label+"/") {
			t.Errorf("no dump names the backend %s:\n%.600s", label, dumps.String())
		}
	}
}

// Generate takes a target or more, returns the check errors of the grammar once and returns no
// files on an error of any target.
func TestGenerateErrors(t *testing.T) {
	src := grammar.FromString("two.sxir", "@start\nrule main = \"a\"\n")
	if _, err := compiler.Generate(src, nil); err == nil || !strings.Contains(err.Error(), "at least one target") {
		t.Errorf("no target: %v", err)
	}
	if _, err := compiler.Generate(src, []compiler.Target{compiler.GoConfig{Module: "example.com/m"}, nil}); err == nil || !strings.Contains(err.Error(), "target 1 is nil") {
		t.Errorf("a nil target: %v", err)
	}
	bad := grammar.FromString("bad.sxir", "@start\nrule main = missing\n")
	var check grammar.CheckErrors
	if _, err := compiler.Generate(bad, []compiler.Target{compiler.TSConfig{}, compiler.TSConfig{Types: true}}); !errors.As(err, &check) {
		t.Errorf("a grammar that does not check: %v", err)
	}
	out, err := compiler.Generate(src, []compiler.Target{compiler.GoConfig{Module: "example.com/m"}, compiler.TSConfig{LibModule: "./lib.ts"}})
	if err == nil || out != nil || !strings.Contains(err.Error(), "target ts") {
		t.Errorf("a failing target: %v, %d results", err, len(out))
	}
}
