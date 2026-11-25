package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A parser generated without limit controls holds no code that counts compute units, spends budget or reads
// a control, does not export the control options, and fails a parse that passes them instead of ignoring
// them.
func TestGeneratedWithoutBudgetControls(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	const src = `rule main = item+ !.
rule item = word / num / " "
rule word = [a-zA-Z_ä-ö]+
rule num = [0-9]+ ("." [0-9]+)?
`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	gen := func(budgetControls bool, document ...bool) string {
		files, err := codegentest.Generate(t, g, common.GenOptions{Package: "wc", Module: "wc", Start: "main", Lib: common.LibImport, Types: true, Recognize: true, NoLimitControls: !budgetControls, Document: len(document) > 0})
		if err != nil {
			t.Fatal(err)
		}
		return string(codegentest.MergeGenerated(files))
	}
	with, without := gen(true), gen(false)
	// With limit controls a batch parse counts logical compute units and checks them.
	for _, want := range []string{"p.Fuel", "p.PollUnits()", "func WithContext"} {
		if !strings.Contains(with, want) {
			t.Errorf("with limit controls: missing %q", want)
		}
		if strings.Contains(without, want) {
			t.Errorf("without limit controls: found %q", want)
		}
	}
	for _, bad := range []string{"p.Control", "BudgetClass", "p.Spend()"} {
		if strings.Contains(with, bad) {
			t.Errorf("with limit controls: found %q", bad)
		}
		if strings.Contains(without, bad) {
			t.Errorf("without limit controls: found %q", bad)
		}
	}
	// Both forms take the depth limit through WithLimits.
	if !strings.Contains(with, "func WithLimits") || !strings.Contains(without, "func WithLimits") {
		t.Error("a form of the parser has no WithLimits")
	}
	// The helpers with an Ordinary form belong to the forms outside Documents: the form without limit
	// controls, and the form with them, which counts units beside the ordinary helpers. A Document
	// records the input it examines, which the Ordinary helpers do not.
	if !strings.Contains(with, "Ordinary(") || !strings.Contains(without, "Ordinary(") {
		t.Error("a form outside Documents calls no Ordinary helper: the check below is void")
	}
	if strings.Contains(gen(true, true), "Ordinary(") {
		t.Error("Document with limit controls: a call of an Ordinary helper")
	}
	if len(without) >= len(with) {
		t.Errorf("parser without limit controls is %d bytes, with %d: want smaller", len(without), len(with))
	}
	// With a Document the parser has the form that serves a Document, which has neither budget charges
	// nor control reads: with limit controls it counts logical compute units like a batch parse.
	documentWith, documentWithout := gen(true, true), gen(false, true)
	for _, bad := range []string{"p.Control", ".Spend()", ".SpendRead(", "BudgetClass"} {
		if strings.Contains(documentWithout, bad) {
			t.Errorf("a Document parser without limit controls has %q", bad)
		}
		if strings.Contains(documentWith, bad) {
			t.Errorf("a Document parser with limit controls has %q", bad)
		}
	}
	for _, want := range []string{"p.Fuel", "p.PollUnits()"} {
		if !strings.Contains(documentWith, want) {
			t.Errorf("a Document parser with limit controls lacks %q", want)
		}
		if strings.Contains(documentWithout, want) {
			t.Errorf("a Document parser without limit controls has %q", want)
		}
	}
	if !strings.Contains(documentWithout, "p.Document == nil") {
		t.Error("a Document parser without limit controls has no in-place read guard")
	}
	t.Logf("sizes: with controls %d, without %d; with Document: with controls %d, without %d", len(with), len(without), len(documentWith), len(documentWithout))
	if len(documentWithout) >= len(documentWith) {
		t.Errorf("Document parser without limit controls is %d bytes, with %d", len(documentWithout), len(documentWith))
	}
	tests := `package wc
import("errors";"strings";"testing";"context";sxirruntime "syntaxir.dev/sxir/pkg/runtime")
func TestOptions(t *testing.T) {
 if _,err:=Parse("abc 12.5 x"); err!=nil {t.Fatal(err)}
 if err:=Recognize("abc 12.5 x"); err!=nil {t.Fatal(err)}
 if _,err:=Parse("abc ("); err==nil {t.Fatal("no error for bad input")}
 if _,err:=ParseValue("abc 12.5 x"); err!=nil {t.Fatal(err)}
 // A zero Limits and a nil context set no control, so they are accepted.
 if _,err:=Parse("abc",sxirruntime.WithLimits(sxirruntime.Limits{}),sxirruntime.WithContext(nil)); err!=nil {t.Fatal(err)}
 for name,opt:=range map[string]sxirruntime.ParseOption{"limits":sxirruntime.WithLimits(sxirruntime.Limits{Compute:1}),"memory":sxirruntime.WithLimits(sxirruntime.Limits{Memory:1}),"context":sxirruntime.WithContext(context.Background())} {
  for entry,call:=range map[string]func() error{
   "Parse":func() error {_,err:=Parse("abc",opt);return err},
   "Recognize":func() error {return Recognize("abc",opt)},
   "ParseValue":func() error {_,err:=ParseValue("abc",opt);return err},
   "ParseRule":func() error {_,err:=ParseRule("word","abc",opt);return err},
  } {
   err:=call()
   if err==nil || !strings.Contains(err.Error(),"without limit controls") {t.Errorf("%s %s: got %v",entry,name,err)}
  }
 }
 // A finite Input needs no control: it is checked against the length of the input. A later
 // WithLimits keeps the fields it leaves zero.
 for entry,call:=range map[string]func(...sxirruntime.ParseOption) error{
  "Parse":func(o ...sxirruntime.ParseOption) error {_,err:=Parse("abc 12.5 x",o...);return err},
  "Recognize":func(o ...sxirruntime.ParseOption) error {return Recognize("abc 12.5 x",o...)},
  "ParseValue":func(o ...sxirruntime.ParseOption) error {_,err:=ParseValue("abc 12.5 x",o...);return err},
  "ParseRule":func(o ...sxirruntime.ParseOption) error {_,err:=ParseRule("word","abcdefghij",o...);return err},
 } {
  if err:=call(WithLimits(Limits{Input:10}));err!=nil {t.Errorf("%s: an input as long as the limit: %v",entry,err)}
  for _,opts:=range [][]sxirruntime.ParseOption{{WithLimits(Limits{Input:9})},{WithLimits(Limits{Input:9}),WithLimits(Limits{Depth:50})}} {
   var le *LimitError
   if err:=call(opts...);!errors.As(err,&le) || le.Kind!=LimitInput || le.Limit!=9 || le.Requested!=10 {t.Errorf("%s: a longer input: %v",entry,err)}
  }
 }
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": []byte(without), "wc_test.go": []byte(tests), "go.mod": []byte(codegentest.GoMod("wc"))} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
