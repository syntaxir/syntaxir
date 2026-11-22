package libprint

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// root is the root of the module, relative to the directory of the package.
const root = "../../../../.."

// newLibrary returns a library that the Go library's route of the pipeline prints from: the
// compositions with Requests, specialized and with their cold paths split, every unit verified.
func newLibrary() (*library.Library, error) {
	p := pipe.Then(pipe.Of(codepass.Specialization), pipe.Of(gopass.ColdPath))
	return pipe.Execute(context.Background(), &pipe.Run{Settings: pass.Settings{}, VerifyEach: true}, p,
		&library.Library{Module: compose.Build(), Requests: Requests()})
}

// testLibrary is the library of newLibrary that the tests share.
var testLibrary = sync.OnceValues(newLibrary)

// printOutput prints the output of the path, a path of outputs, from testLibrary.
func printOutput(t *testing.T, path string) string {
	t.Helper()
	l, err := testLibrary()
	if err != nil {
		t.Fatal(err)
	}
	return printFrom(t, l, path)
}

// printFrom prints the output of the path, a path of outputs, from the library l.
func printFrom(t *testing.T, l *library.Library, path string) string {
	t.Helper()
	index := map[[2]string]int{}
	for i, r := range l.Requests {
		index[[2]string{r.File, r.Name}] = i
	}
	for i := range outputs {
		if o := &outputs[i]; o.path == path {
			src, err := o.print(file{l: l, o: o, index: index})
			if err != nil {
				t.Fatal(err)
			}
			return src
		}
	}
	t.Fatalf("no output prints %s", path)
	return ""
}

// checkCurrent checks that the file of the path, relative to the root of the module, is the one that
// the printer prints now.
func checkCurrent(t *testing.T, path string) {
	t.Helper()
	want := printOutput(t, path)
	got, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; run go generate ./internal/backend/codegen/golang/libprint", path)
	}
}

// Printing the Go library's action file and then the engine's from one unit gives the engine's file
// printed alone from another unit, and leaves the unit as it was: the printers of two files of one
// composition copy what they change.
func TestPrintingLeavesTheUnit(t *testing.T) {
	one, err := newLibrary()
	if err != nil {
		t.Fatal(err)
	}
	alone, err := newLibrary()
	if err != nil {
		t.Fatal(err)
	}
	before := libraryString(one)
	printFrom(t, one, goActionsFile)
	if got, want := printFrom(t, one, engineActionsFile), printFrom(t, alone, engineActionsFile); got != want {
		t.Errorf("the engine's action file printed after the Go library's differs from the one printed alone")
	}
	if after := libraryString(one); after != before {
		t.Errorf("printing changed the unit")
	}
}

// libraryString returns the functions of the module of l and the specialized and made functions of
// l, with their documentation, as text.
func libraryString(l *library.Library) string {
	var b strings.Builder
	fs := slices.Clone(l.Module.Funcs)
	fs = append(fs, l.Funcs...)
	for _, m := range l.Made {
		fs = append(fs, m.Func)
	}
	for _, f := range fs {
		b.WriteString(f.Doc + "\n" + code.FuncString(f))
	}
	return b.String()
}
