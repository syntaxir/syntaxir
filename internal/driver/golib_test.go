package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The route of the Go library runs specialize, go-cold-path and print-libgo, every unit verified, and
// prints the files that go generate wrote: those checked in at their paths from the root of the
// module.
func TestGoLibraryPrintsTheCheckedInFiles(t *testing.T) {
	settings, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	out, err := GoLibrary(context.Background(), Options{Settings: settings, Debug: common.Debug{VerifyEach: true},
		Observers: []pipe.Observer{stepNames{&steps}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(steps, ","); got != "specialize ,go-cold-path ,print-libgo " {
		t.Errorf("the route ran %s", got)
	}
	files, ok := out.(*result.Files)
	if !ok || files.Lang != result.Go || len(files.Files) == 0 {
		t.Fatalf("the route printed %#v", out)
	}
	seen := map[string]bool{}
	for _, f := range files.Files {
		if seen[f.Path] {
			t.Errorf("the route prints %s twice", f.Path)
		}
		seen[f.Path] = true
		disk, err := os.ReadFile(filepath.Join("..", "..", f.Path))
		if err != nil {
			t.Fatal(err)
		}
		if string(disk) != string(f.Content) {
			t.Errorf("%s is not the file that the route prints; run go generate ./internal/backend/codegen/golang/libprint", f.Path)
		}
	}
}

// The route of the Go library has no slot, so an edit that names one fails before any step, naming
// the slot.
func TestGoLibraryRejectsSlotEdits(t *testing.T) {
	for _, c := range []struct {
		name string
		edit pipe.Edit
	}{
		{"core-passes", pipe.Fill(CoreSlot, "", pipe.Seq(corepass.IR))},
		{"flow-passes", pipe.Fill(FlowSlot, "", pipe.Seq(code.FlowStage))},
		{"exec-passes", pipe.Fill(ExecSlot, "", pipe.Seq(code.ExecStage))},
		{"target-passes", pipe.Fill(TargetSlot, "", pipe.Seq(execdata.LoweredIR))},
	} {
		var steps []string
		_, err := GoLibrary(context.Background(), Options{Edits: []pipe.Edit{c.edit}, Observers: []pipe.Observer{stepNames{&steps}}})
		if err == nil || !strings.Contains(err.Error(), "no slot "+c.name) {
			t.Errorf("%s: %v", c.name, err)
		}
		if len(steps) > 0 {
			t.Errorf("%s: the route ran %v", c.name, steps)
		}
	}
}

// A program text given to the route of the Go library, and its text given to Compile, fail before
// any step, naming the source kind of the text and of the pipeline.
func TestGoLibraryAndCompileRejectEachOthersTexts(t *testing.T) {
	p, err := checkSource(t, "@start\nrule A = \"a\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Describe(p, Options{}, goOut(common.GenOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	lib, err := DescribeGoLibrary(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	observe := []pipe.Observer{stepNames{&steps}}
	_, err = GoLibrary(context.Background(), Options{Pipeline: program.String(), Observers: observe})
	if want := "the text takes program to output, and the pipeline library to output"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a program text on the library route: %v, want %q", err, want)
	}
	_, err = Compile(context.Background(), p, Options{Pipeline: lib.String(), Observers: observe}, goOut(common.GenOptions{}))
	if want := "the text takes library to output, and the pipeline program to output"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a library text given to Compile: %v, want %q", err, want)
	}
	if len(steps) > 0 {
		t.Errorf("the rejected texts ran %v", steps)
	}
}
