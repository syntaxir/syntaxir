package mutanttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const schemaLib = `package lib

import "strings"

// Sum adds its arguments.
func Sum(xs ...int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

type Box[T any] struct{ v T }

func (b *Box[T]) Get() T { return b.v }

func Max[T int | float64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

var calls int

func touch(n int) {
	calls += n
}

func Upper(s string) string { touch(1); return strings.ToUpper(s) }

func Unnamed(int) int { return 1 }

const Limit = 3
`

const schemaLibTest = `package lib

import "testing"

func TestSum(t *testing.T) {
	if Sum(1, 2, 3) != 6 {
		t.Fatal("Sum")
	}
}

func TestBox(t *testing.T) {
	b := &Box[string]{"x"}
	if b.Get() != "x" {
		t.Fatal("Get")
	}
}

func TestMax(t *testing.T) {
	if Max(1, 2) != 2 || Max(2.5, 1.5) != 2.5 {
		t.Fatal("Max")
	}
}

func TestUpper(t *testing.T) {
	calls = 0
	if Upper("a") != "A" || calls != 1 {
		t.Fatal("Upper")
	}
}

func TestUnnamed(t *testing.T) {
	if Unnamed(0) != 1 || Limit != 3 {
		t.Fatal("Unnamed")
	}
}
`

// TestBatch runs mutants of a scratch package through a Batch: the mutants of function bodies, of generic and
// variadic functions and of methods run from one schema build, the others (a change of a constant, of a function
// whose parameter has no name) one by one; every mutant that breaks a test is reported with its failure, and a
// mutant that changes nothing passes, either way.
func TestBatch(t *testing.T) {
	dir := scratchDir(t)
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/lib\n\ngo 1.24\n")
	write("lib.go", schemaLib)
	write("lib_test.go", schemaLibTest)
	path := filepath.Join(dir, "lib.go")
	mutate := func(old, new string) map[string]string {
		if strings.Count(schemaLib, old) != 1 {
			t.Fatalf("lib.go has no single %q", old)
		}
		return map[string]string{path: strings.Replace(schemaLib, old, new, 1)}
	}
	env := append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	cases := []struct {
		name   string
		m      Mutant
		schema bool
		killed string // the test that must fail, or "" for a mutant that must survive
	}{
		{"a variadic sum skips an element", Mutant{mutate("s += x", "s += x * 0"), []string{"."}, "^TestSum$"}, true, "TestSum"},
		{"a generic method returns the zero", Mutant{mutate("return b.v", "var z T; return z"), []string{"."}, "^TestBox$"}, true, "TestBox"},
		{"a generic function takes the least", Mutant{mutate("if a > b {", "if a < b {"), []string{"."}, "^TestMax$"}, true, "TestMax"},
		{"a callee counts twice", Mutant{mutate("calls += n", "calls += 2 * n"), []string{"."}, "^TestUpper$"}, true, "TestUpper"},
		{"an equivalent sum", Mutant{mutate("s := 0", "s := 0 * 1"), []string{"."}, "^TestSum$"}, true, ""},
		{"a constant changes", Mutant{mutate("const Limit = 3", "const Limit = 4"), []string{"."}, "^TestUnnamed$"}, false, "TestUnnamed"},
		{"a function with an unnamed parameter", Mutant{mutate("return 1 }", "return 2 }"), []string{"."}, "^TestUnnamed$"}, false, "TestUnnamed"},
	}
	var ms []Mutant
	for _, c := range cases {
		ms = append(ms, c.m)
	}
	b := NewBatch(t, dir, env, nil, ms)
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if b.Schema(i) != c.schema {
				t.Errorf("Schema = %v, want %v", b.Schema(i), c.schema)
			}
			out, err := b.Test(i)
			switch {
			case c.killed == "" && err != nil:
				t.Errorf("the mutant that changes nothing fails: %v\n%s", err, out)
			case c.killed != "" && (err == nil || !strings.Contains(string(out), "--- FAIL: "+c.killed)):
				t.Errorf("the mutant is not reported killed by %s: %v\n%s", c.killed, err, out)
			}
		})
	}
}

// TestBatchFallsBackWhenTheSchemaDoesNotBuild gives a Batch a mutant that does not compile: the schema build fails,
// so every mutant runs one by one, the broken one fails to build and the other is still killed.
func TestBatchFallsBackWhenTheSchemaDoesNotBuild(t *testing.T) {
	dir := scratchDir(t)
	for name, text := range map[string]string{"go.mod": "module example.com/lib\n\ngo 1.24\n", "lib.go": schemaLib, "lib_test.go": schemaLibTest} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "lib.go")
	env := append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	b := NewBatch(t, dir, env, nil, []Mutant{
		{map[string]string{path: strings.Replace(schemaLib, "s += x", "s += y", 1)}, []string{"."}, "^TestSum$"},
		{map[string]string{path: strings.Replace(schemaLib, "if a > b {", "if a < b {", 1)}, []string{"."}, "^TestMax$"},
	})
	if b.Schema(0) || b.Schema(1) {
		t.Fatalf("a schema that does not build runs mutants: %v %v", b.Schema(0), b.Schema(1))
	}
	if out, err := b.Test(0); err == nil || strings.Contains(string(out), "--- FAIL") || !strings.Contains(string(out), "undefined: y") {
		t.Errorf("the broken mutant: %v\n%s", err, out)
	}
	if out, err := b.Test(1); err == nil || !strings.Contains(string(out), "--- FAIL: TestMax") {
		t.Errorf("the mutant beside the broken one: %v\n%s", err, out)
	}
}

// scratchDir returns a temporary directory by the path that the go command sees, without symbolic links, so that
// the overlay names the files that it builds.
func scratchDir(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
