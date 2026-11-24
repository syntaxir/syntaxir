package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// typedUnitsModule generates a package g<i> for each case with opts into a test module, and returns
// the directory of the module and the go command.
func typedUnitsModule(t *testing.T, cases []codegentest.Case, opts common.GenOptions) (dir, goBin string) {
	t.Helper()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("codegentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		codegentest.GeneratePackage(t, dir, fmt.Sprintf("g%d", i), g, opts)
	}
	return dir, goBin
}

// runTypedUnits writes main.go of the module dir, runs it, and fails the test with each line the
// program prints: the program prints only the parses whose compute units differ.
func runTypedUnits(t *testing.T, dir, goBin, main string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if len(out) != 0 {
		t.Errorf("the compute units differ:\n%s", out)
	}
}

// typedStreamInsertionCases are #stream grammars with #missing whose elements are typed: insertions in
// an element, in a recovered element and past the limit, and in a memoized rule that the elements share.
var typedStreamInsertionCases = []codegentest.Case{
	{Name: "inserted", Src: `type S = { N node[string]; Semi node[string] | missing }
rule main = item* #stream
rule item S = n:[a-z]+ #atomic s:(";" #missing) => S{N: $n, Semi: $s}`,
		Inputs: []string{"ab;cd;", "ab cd;e", "a b c d e f g h i j", ""}},
	{Name: "inserted in a recovery", Src: `type S = { N node[string]; Semi node[string] | missing }
rule main = (item #recover(skip=[^;]+ ";"))* #stream
rule item S = n:[a-z]+ #atomic s:(";" #missing) => S{N: $n, Semi: $s}`,
		Inputs: []string{"ab;1x;cd;", "ab 1x;cd", "1x;ab", "a b;"}},
	{Name: "inserted in a shared rule", Src: `rule main = item* #stream
rule item = a "x" / a "y"
rule a = "(" [a-z]* ")" #missing`,
		Inputs: []string{"(b)x(c)y", "(bx(cy", "(", "(b)x(c", ")"}},
}

// TestGeneratedTypedStreamInsertionUnits checks the claim of the specification that the typed stream
// parses count the compute units of the engine's stream parse when they insert missing tokens:
// ParseStreamValue and IterateValue, which read the input a byte at a time and in one piece, count
// the units that ParseStream of the same parser counts (which TestGeneratedParsersInsertMissingTokens
// compares with the engine), at the limits of missing tokens that insert nothing, some and all.
func TestGeneratedTypedStreamInsertionUnits(t *testing.T) {
	t.Parallel()
	dir, goBin := typedUnitsModule(t, typedStreamInsertionCases, common.GenOptions{Start: "main", Stream: true, Types: true})
	var imports, rows strings.Builder
	for i, c := range typedStreamInsertionCases {
		fmt.Fprintf(&imports, "\tg%d \"codegentest/g%d\"\n", i, i)
		var in strings.Builder
		for _, s := range c.Inputs {
			fmt.Fprintf(&in, "%q, ", s)
		}
		fmt.Fprintf(&rows, `	{%q, []string{%s}, func(s string, bytes bool, limit int, chunked bool) (node, typed, iter uint64) {
		u := g%[3]d.CodePoints
		if bytes {
			u = g%[3]d.Bytes
		}
		r := func() io.Reader {
			if chunked {
				return iotest.OneByteReader(strings.NewReader(s))
			}
			return strings.NewReader(s)
		}
		g%[3]d.ParseStream(r(), nil, g%[3]d.WithUnit(u), sxirruntime.WithMissingTokens(limit), sxirruntime.WithComputeReport(&node))
		g%[3]d.ParseStreamValue(r(), nil, g%[3]d.WithUnit(u), sxirruntime.WithMissingTokens(limit), sxirruntime.WithComputeReport(&typed))
		it, err := g%[3]d.IterateValue(r(), g%[3]d.WithUnit(u), sxirruntime.WithMissingTokens(limit), sxirruntime.WithComputeReport(&iter))
		if err == nil {
			for {
				if _, ok, _ := it.Next(); !ok {
					break
				}
			}
			it.Close()
		}
		return
	}},
`, c.Name, in.String(), i)
	}
	runTypedUnits(t, dir, goBin, `package main

import (
	"fmt"
	"io"
	"strings"
	"testing/iotest"

`+imports.String()+`	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

var cases = []struct {
	name   string
	inputs []string
	run    func(s string, bytes bool, limit int, chunked bool) (node, typed, iter uint64)
}{
`+rows.String()+`}

func main() {
	for _, c := range cases {
		for _, in := range c.inputs {
			for _, bytes := range []bool{false, true} {
				for _, limit := range []int{0, 2, 8} {
					for _, chunked := range []bool{false, true} {
						n, v, it := c.run(in, bytes, limit, chunked)
						if n != v || n != it {
							fmt.Printf("%s %q bytes=%v limit=%d chunked=%v: ParseStream %d, ParseStreamValue %d, IterateValue %d\n", c.name, in, bytes, limit, chunked, n, v, it)
						}
					}
				}
			}
		}
	}
}
`)
}
