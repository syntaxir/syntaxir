package print_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// The tests of this package compile the grammars that they generate parsers for with the engine,
// as package engine does.
func init() { enginetest.UseEngine() }

func TestMain(m *testing.M) { enginetest.Main(m) }

var (
	mustCompile         = enginetest.MustCompile
	compileBare         = enginetest.CompileBare
	referenceProgram    = enginetest.Reference
	compileForGenerator = enginetest.CompileForGenerator
	parseTest           = enginetest.Parse
	mustParse           = enginetest.MustParse
	compileStarts       = enginetest.CompileStarts
	compileMessage      = enginetest.CompileMessage
	resultWith          = enginetest.Result
)

// libSource is the text of the library package, for tests that check its source.
var libSource = func() string {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "pkg", "runtime", "libgo", "*.go"))
	if err != nil {
		panic(err)
	}
	var b strings.Builder
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			panic(err)
		}
		b.Write(data)
	}
	return b.String()
}()

// startEntryWant is what the interpreter reports for each of codegentest.StartEntryNames, one line each.
func startEntryWant(t *testing.T) string {
	t.Helper()
	prog, err := compileStarts(t, codegentest.StartRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, name := range codegentest.StartEntryNames {
		if err := prog.CheckStart(name); err != nil {
			fmt.Fprintf(&b, "%s: %v\n", name, err)
		} else {
			fmt.Fprintf(&b, "%s: ok\n", name)
		}
	}
	return b.String()
}

// ignoreCaseLines is the expected output of a program that parses each input and prints "ok" with
// the consumed input, or "error".
func ignoreCaseLines(t *testing.T, src string, inputs []string) string {
	t.Helper()
	prog := mustCompile(t, src)
	var b strings.Builder
	for _, in := range inputs {
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, Backend: engine.Closure})
		if err != nil {
			b.WriteString("error\n")
			continue
		}
		r := []rune(in)
		fmt.Fprintf(&b, "ok %s\n", string(r[n.Start():n.End()]))
	}
	return b.String()
}

// firstCharEnd returns the position after the character at pos in the unit u: what a call that
// looks at that character only has examined, which is pos+1 at the end of the input.
func firstCharEnd(input string, pos int, u sxirinput.PositionUnit) int {
	if u == sxirinput.Bytes && pos < len(input) {
		_, size := utf8.DecodeRuneInString(input[pos:])
		return pos + size
	}
	return pos + 1
}

// generateGo generates the Go parser of g with opts, the library of the tests, the edits and observers
// of x and the hooks h of the printer, as package generate does (generate.GoWith).
func generateGo(g *ast.Grammar, opts common.GenOptions, x driver.Options, h *print.Hooks) ([]common.GeneratedFile, error) {
	files, _, err := generateGoChecked(g, opts, x, h)
	return files, err
}

// generateGoChecked is generateGo that also returns the checked program that the generation compiled
// g to, which the steps of the pipeline elaborated the Core IR of the tables from.
func generateGoChecked(g *ast.Grammar, opts common.GenOptions, x driver.Options, h *print.Hooks) ([]common.GeneratedFile, *frontend.Result, error) {
	var prog *frontend.Result
	compile := func(g *ast.Grammar, o check.Options) (*frontend.Result, func(driver.Role) (*frontend.Result, error), error) {
		p, derive, err := codegentest.Compile(g, o)
		prog = p
		return p, derive, err
	}
	files, err := generate.GoWith(compile, g, codegentest.WithLib(opts), x, h)
	return files, prog, err
}
